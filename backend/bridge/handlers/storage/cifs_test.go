package storage

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cifsTestEnv struct {
	tmp      string
	lastArgs []string
	runErr   error
}

// setupCIFSTest redirects fstab + the credentials dir at temp locations and
// stubs the mount runner + availability check so tests need no real mount.cifs.
func setupCIFSTest(t *testing.T) *cifsTestEnv {
	t.Helper()
	tmp := t.TempDir()
	env := &cifsTestEnv{tmp: tmp}

	origFstab, origCreds := fstabPath, cifsCredentialsDir
	origAvail, origRunner := cifsClientAvailable, cifsMountRunner
	t.Cleanup(func() {
		fstabPath, cifsCredentialsDir = origFstab, origCreds
		cifsClientAvailable, cifsMountRunner = origAvail, origRunner
	})

	fstabPath = filepath.Join(tmp, "fstab")
	if err := os.WriteFile(fstabPath, []byte("# test fstab\n"), 0o644); err != nil {
		t.Fatalf("seed fstab: %v", err)
	}
	cifsCredentialsDir = filepath.Join(tmp, "cifs-credentials")
	cifsClientAvailable = func() (bool, error) { return true, nil }
	cifsMountRunner = func(_ context.Context, _ time.Duration, name string, args ...string) ([]byte, error) {
		env.lastArgs = append([]string{name}, args...)
		return nil, env.runErr
	}
	return env
}

func readFstab(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(fstabPath)
	if err != nil {
		t.Fatalf("read fstab: %v", err)
	}
	return string(data)
}

func TestMountCIFSAuthedWritesFstabAndCredsFile(t *testing.T) {
	env := setupCIFSTest(t)

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const pw = "hunter2-secret"
	mp := filepath.Join(env.tmp, "mnt", "media")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp,
		username: "miguel", password: pw, domain: "WORKGROUP", options: []string{"rw"},
	}); err != nil {
		t.Fatalf("MountCIFS() error = %v", err)
	}

	// mount is invoked with just the mountpoint (reads fstab); no password in argv.
	for _, a := range env.lastArgs {
		if strings.Contains(a, pw) {
			t.Fatalf("password leaked into argv: %v", env.lastArgs)
		}
	}

	fstab := readFstab(t)
	if strings.Contains(fstab, pw) {
		t.Fatalf("password leaked into fstab:\n%s", fstab)
	}
	if !strings.Contains(fstab, "credentials=") || !strings.Contains(fstab, mp) {
		t.Fatalf("fstab missing credentials entry:\n%s", fstab)
	}
	if !strings.Contains(fstab, "_netdev") || !strings.Contains(fstab, "nofail") {
		t.Fatalf("fstab missing boot options:\n%s", fstab)
	}

	credPath := credentialsFilePath(mp)
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat creds file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("creds file mode = %v, want 0600", info.Mode().Perm())
	}
	dinfo, err := os.Stat(cifsCredentialsDir)
	if err != nil {
		t.Fatalf("stat creds dir: %v", err)
	}
	if dinfo.Mode().Perm() != 0o700 {
		t.Fatalf("creds dir mode = %v, want 0700", dinfo.Mode().Perm())
	}
	data, _ := os.ReadFile(credPath)
	if !strings.Contains(string(data), "password="+pw) {
		t.Fatalf("creds file missing password")
	}

	if strings.Contains(logBuf.String(), pw) {
		t.Fatalf("logs leaked the password:\n%s", logBuf.String())
	}
}

func TestMountCIFSGuestNoCredsFile(t *testing.T) {
	env := setupCIFSTest(t)
	mp := filepath.Join(env.tmp, "mnt", "public")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "public", mountpoint: mp,
	}); err != nil {
		t.Fatalf("MountCIFS() guest error = %v", err)
	}

	fstab := readFstab(t)
	if !strings.Contains(fstab, "guest") {
		t.Fatalf("guest mount missing guest option:\n%s", fstab)
	}
	if strings.Contains(fstab, "credentials=") {
		t.Fatalf("guest mount must not write credentials=:\n%s", fstab)
	}
	if _, err := os.Stat(credentialsFilePath(mp)); !os.IsNotExist(err) {
		t.Fatalf("guest mount wrote a credentials file")
	}
}

func TestMountCIFSUnavailableLeavesNoOrphanDir(t *testing.T) {
	env := setupCIFSTest(t)
	cifsClientAvailable = func() (bool, error) {
		return false, errors.New("mount.cifs not found (install cifs-utils)")
	}
	mp := filepath.Join(env.tmp, "mnt", "x")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp, username: "u", password: "p",
	}); err == nil {
		t.Fatal("MountCIFS() error = nil, want unavailable error")
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("mountpoint dir must not be created when client unavailable")
	}
	if strings.Contains(readFstab(t), mp) {
		t.Fatalf("fstab must not be modified when client unavailable")
	}
}

func TestMountCIFSRollsBackOnMountFailure(t *testing.T) {
	env := setupCIFSTest(t)
	env.runErr = errors.New("mount error 13: permission denied")
	mp := filepath.Join(env.tmp, "mnt", "media")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp, username: "u", password: "p",
	}); err == nil {
		t.Fatal("MountCIFS() error = nil, want mount failure")
	}
	if strings.Contains(readFstab(t), mp) {
		t.Fatalf("fstab entry not rolled back after mount failure:\n%s", readFstab(t))
	}
	if _, err := os.Stat(credentialsFilePath(mp)); !os.IsNotExist(err) {
		t.Fatalf("credentials file not rolled back after mount failure")
	}
}

func TestUnmountCIFSRemovesFstabAndCreds(t *testing.T) {
	env := setupCIFSTest(t)
	mp := filepath.Join(env.tmp, "mnt", "media")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp, username: "u", password: "p",
	}); err != nil {
		t.Fatalf("MountCIFS() error = %v", err)
	}
	if _, err := os.Stat(credentialsFilePath(mp)); err != nil {
		t.Fatalf("credentials file not written: %v", err)
	}

	if _, err := UnmountCIFS(context.Background(), mp, true); err != nil {
		t.Fatalf("UnmountCIFS() error = %v", err)
	}
	if strings.Contains(readFstab(t), mp) {
		t.Fatalf("fstab entry not removed:\n%s", readFstab(t))
	}
	if _, err := os.Stat(credentialsFilePath(mp)); !os.IsNotExist(err) {
		t.Fatalf("credentials file not deleted on remove")
	}
}

func TestMountCIFSRejectsMountpointInUseByDifferentSource(t *testing.T) {
	env := setupCIFSTest(t)
	mp := filepath.Join(env.tmp, "mnt", "media")

	// An existing CIFS entry at this mountpoint pointing at a different server.
	if err := addToFstab("//other/share", mp, "cifs", []string{"guest", "_netdev", "nofail"}); err != nil {
		t.Fatalf("seed fstab: %v", err)
	}

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp, username: "u", password: "p",
	}); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("expected 'already in use' rejection, got %v", err)
	}
	// The pre-existing entry must be untouched.
	if !strings.Contains(readFstab(t), "//other/share") {
		t.Fatalf("pre-existing fstab entry was modified:\n%s", readFstab(t))
	}
}

func TestRemountCIFSRollsBackOnMountFailure(t *testing.T) {
	env := setupCIFSTest(t)
	mp := filepath.Join(env.tmp, "mnt", "media")

	if err := addToFstab("//nas/media", mp, "cifs", []string{"credentials=/x", "_netdev", "nofail", "rw"}); err != nil {
		t.Fatalf("seed fstab: %v", err)
	}

	env.runErr = errors.New("mount error 22: invalid argument")
	if _, err := RemountCIFS(context.Background(), mp, []string{"ro", "bad_option"}, true); err == nil {
		t.Fatal("RemountCIFS() error = nil, want mount failure")
	}

	// Options must be rolled back to the original (rw, not ro).
	fstab := readFstab(t)
	line := ""
	for l := range strings.SplitSeq(fstab, "\n") {
		if strings.Contains(l, mp) {
			line = l
		}
	}
	if !strings.Contains(line, "rw") || strings.Contains(line, "bad_option") {
		t.Fatalf("fstab options not rolled back after remount failure:\n%s", line)
	}
}

func TestWriteCredentialsTightensExistingDir(t *testing.T) {
	env := setupCIFSTest(t)
	if err := os.MkdirAll(cifsCredentialsDir, 0o777); err != nil {
		t.Fatalf("pre-create creds dir: %v", err)
	}
	mp := filepath.Join(env.tmp, "mnt", "media")

	if _, err := MountCIFS(context.Background(), cifsMountParams{
		server: "nas", share: "media", mountpoint: mp, username: "u", password: "p",
	}); err != nil {
		t.Fatalf("MountCIFS() error = %v", err)
	}
	info, err := os.Stat(cifsCredentialsDir)
	if err != nil {
		t.Fatalf("stat creds dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("existing creds dir not tightened: mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestValidateCIFSMountRequest(t *testing.T) {
	cases := []struct {
		server, share, mountpoint string
		ok                        bool
	}{
		{"nas", "media", "/mnt/media", true},
		{"nas.local", "Media_Share-1$", "/mnt/m", true},
		{"nas", "My Share", "/mnt/m", false}, // spaces would corrupt fstab
		{"bad host", "media", "/mnt/m", false},
		{"nas", "", "/mnt/m", false},
		{"nas", "media", "relative/path", false},
		{"nas", "media", "/etc", false},
	}
	for _, c := range cases {
		err := validateCIFSMountRequest(c.server, c.share, c.mountpoint)
		if (err == nil) != c.ok {
			t.Fatalf("validate(%q,%q,%q) err=%v, want ok=%v", c.server, c.share, c.mountpoint, err, c.ok)
		}
	}
}

func TestRejectSensitiveCustomOptions(t *testing.T) {
	if err := rejectSensitiveCustomOptions([]string{"rw", "vers=3.0", "uid=0"}); err != nil {
		t.Fatalf("unexpected rejection of safe options: %v", err)
	}
	for _, bad := range []string{"password=x", "credentials=/a", "username=u", "guest"} {
		if err := rejectSensitiveCustomOptions([]string{bad}); err == nil {
			t.Fatalf("expected rejection of %q", bad)
		}
	}
}
