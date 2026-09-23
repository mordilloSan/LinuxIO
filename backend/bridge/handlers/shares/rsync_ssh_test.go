package shares

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestSSHIdentification(t *testing.T) {
	for _, test := range []struct {
		name   string
		banner string
		valid  bool
	}{
		{"ssh2", "SSH-2.0-OpenSSH_9.6\r\n", true},
		{"compatible", "SSH-1.99-OpenSSH\r\n", true},
		{"notice", "Authorized users only\r\nSSH-2.0-OpenSSH\r\n", true},
		{"rsync daemon", "@RSYNCD: 32.0\n", false},
		{"ssh1", "SSH-1.5-obsolete\r\n", false},
		{"http", "HTTP/1.1 200 OK\r\n", false},
		{"oversized line", strings.Repeat("x", 257) + "\nSSH-2.0-test\n", false},
		{"too many lines", strings.Repeat("notice\n", 50) + "SSH-2.0-test\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := readSSHIdentification(strings.NewReader(test.banner))
			if (err == nil) != test.valid {
				t.Fatalf("identification error = %v, valid = %v", err, test.valid)
			}
		})
	}
}

func TestRsyncSSHCheckHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handleGetRsyncSSH(ctx, apischema.RsyncSSHRequest{Port: 22}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SSH check = %v", err)
	}
}

func TestRsyncSSHRejectsInvalidPorts(t *testing.T) {
	setupRsyncSSHTest(t)
	for _, port := range []int{-1, 0, 65536} {
		if _, err := handleGetRsyncSSH(context.Background(), apischema.RsyncSSHRequest{Port: port}); err == nil {
			t.Errorf("accepted invalid SSH port %d", port)
		}
	}
}

func TestRsyncSSHChecksSelectedPort(t *testing.T) {
	setupRsyncSSHTest(t)
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		if deadlineErr := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
			t.Error(deadlineErr)
			return
		}
		if _, writeErr := io.WriteString(conn, "SSH-2.0-test\r\n"); writeErr != nil {
			t.Error(writeErr)
		}
	}()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener did not return a TCP address")
	}
	request := apischema.RsyncSSHRequest{Port: address.Port}
	status, err := handleGetRsyncSSH(context.Background(), request)
	if err != nil || !status.Available || status.Port != request.Port {
		t.Fatalf("SSH check on selected port: status = %+v, error = %v", status, err)
	}
	<-done
	_ = listener.Close()
	status, err = handleGetRsyncSSH(context.Background(), request)
	if err != nil || status.Available || status.Error == nil || status.Port != request.Port {
		t.Fatalf("closed SSH port: status = %+v, error = %v", status, err)
	}
}

// setupRsyncSSHTest maps three accounts onto the test process so ownership
// assignment succeeds without root. It returns the home of "tnas-ssh".
func setupRsyncSSHTest(t *testing.T) string {
	t.Helper()
	setupRsyncTest(t)
	oldLink, oldLookup := rsyncSSHLink, rsyncLookupUser
	t.Cleanup(func() { rsyncSSHLink, rsyncLookupUser = oldLink, oldLookup })
	rsyncSSHLink = filepath.Join(t.TempDir(), "rsyncd-ssh.conf")
	uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	home := t.TempDir()
	accounts := map[string]*user.User{
		"tnas-ssh": {Username: "tnas-ssh", Uid: uid, Gid: gid, HomeDir: home},
		"other":    {Username: "other", Uid: uid, Gid: gid, HomeDir: t.TempDir()},
		"root":     {Username: "root", Uid: "0", Gid: "0", HomeDir: "/"},
	}
	rsyncLookupUser = func(name string) (*user.User, error) {
		if account, ok := accounts[name]; ok {
			return account, nil
		}
		return nil, user.UnknownUserError(name)
	}
	return home
}

func TestRsyncSSHSaveAndRead(t *testing.T) {
	home := setupRsyncSSHTest(t)
	path := t.TempDir()
	config, err := handleSaveRsyncSSH(context.Background(), apischema.RsyncSSHSaveRequest{Username: "tnas-ssh", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	want := apischema.RsyncSSHConfig{Username: "tnas-ssh", Path: path, Module: "backup"}
	if config != want {
		t.Fatalf("saved config = %+v, want %+v", config, want)
	}
	assertRsyncSSHFile(t, filepath.Join(home, "rsyncd.conf"), path)
	status, err := handleGetRsyncSSH(context.Background(), apischema.RsyncSSHRequest{Port: 1})
	if err != nil || status.Config == nil || *status.Config != want {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

func assertRsyncSSHFile(t *testing.T, target, path string) {
	t.Helper()
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"[backup]", "path = " + path, "read only = yes", "use chroot = no", "munge symlinks = no", "list = yes"} {
		if !strings.Contains(string(data), setting+"\n") {
			t.Errorf("missing setting %q in %q", setting, data)
		}
	}
	for _, forbidden := range []string{"auth users", "secrets file", "uid =", "hosts allow"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("SSH module must not set %q", forbidden)
		}
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("rsyncd.conf must be readable by the account: %v, %v", info, err)
	}
	if link, linkErr := os.Readlink(rsyncSSHLink); linkErr != nil || link != target {
		t.Fatalf("link = %q, %v; want %q", link, linkErr, target)
	}
}

func TestRsyncSSHSwitchAccountAndRemove(t *testing.T) {
	home := setupRsyncSSHTest(t)
	path := t.TempDir()
	if _, err := handleSaveRsyncSSH(context.Background(), apischema.RsyncSSHSaveRequest{Username: "tnas-ssh", Path: path}); err != nil {
		t.Fatal(err)
	}
	// Switching accounts moves the module and removes the previous file.
	if _, err := handleSaveRsyncSSH(context.Background(), apischema.RsyncSSHSaveRequest{Username: "other", Path: path}); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "rsyncd.conf")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("previous account still has rsyncd.conf: %v", statErr)
	}
	config, err := readRsyncSSHConfigUnderTest(t)
	if err != nil || config.Username != "other" {
		t.Fatalf("switched config = %+v, %v", config, err)
	}

	if _, err = handleRemoveRsyncSSH(context.Background(), apischema.NoRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, linkErr := os.Lstat(rsyncSSHLink); !errors.Is(linkErr, os.ErrNotExist) {
		t.Fatalf("link still exists: %v", linkErr)
	}
	if removed, readErr := readRsyncSSHConfig(); readErr != nil || removed != nil {
		t.Fatalf("config after removal = %+v, %v", removed, readErr)
	}
	// Removing twice is harmless.
	if _, err = handleRemoveRsyncSSH(context.Background(), apischema.NoRequest{}); err != nil {
		t.Fatal(err)
	}
}

func readRsyncSSHConfigUnderTest(t *testing.T) (apischema.RsyncSSHConfig, error) {
	t.Helper()
	config, err := readRsyncSSHConfig()
	if err != nil || config == nil {
		return apischema.RsyncSSHConfig{}, errors.Join(err, errors.New("no configuration"))
	}
	return *config, nil
}

func TestRsyncSSHRejectsUnsuitableAccountsAndFolders(t *testing.T) {
	home := setupRsyncSSHTest(t)
	path := t.TempDir()
	for name, req := range map[string]apischema.RsyncSSHSaveRequest{
		"root":          {Username: "root", Path: path},
		"unknown":       {Username: "nobody-here", Path: path},
		"bad name":      {Username: "bad name", Path: path},
		"relative path": {Username: "tnas-ssh", Path: "data"},
		"missing path":  {Username: "tnas-ssh", Path: filepath.Join(path, "missing")},
	} {
		if _, err := handleSaveRsyncSSH(context.Background(), req); err == nil {
			t.Errorf("%s: accepted %+v", name, req)
		}
	}
	if _, err := os.Lstat(rsyncSSHLink); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected saves must not create the link: %v", err)
	}

	// A hand-written rsyncd.conf in the account's home is never overwritten or deleted.
	target := filepath.Join(home, "rsyncd.conf")
	if err := os.WriteFile(target, []byte("[mine]\npath = /srv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := handleSaveRsyncSSH(context.Background(), apischema.RsyncSSHSaveRequest{Username: "tnas-ssh", Path: path}); err == nil {
		t.Fatal("overwrote a foreign rsyncd.conf")
	}
	if err := os.Symlink(target, rsyncSSHLink); err != nil {
		t.Fatal(err)
	}
	if _, err := handleGetRsyncSSH(context.Background(), apischema.RsyncSSHRequest{Port: 1}); err == nil {
		t.Fatal("reported a foreign rsyncd.conf as LinuxIO's")
	}
	if _, err := handleRemoveRsyncSSH(context.Background(), apischema.NoRequest{}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "[mine]\npath = /srv\n" {
		t.Fatalf("foreign rsyncd.conf was altered: %q, %v", data, err)
	}
}
