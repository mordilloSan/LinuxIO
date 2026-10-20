package btrfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// stubSSH points ReplicationDir at a temp dir and replaces commands with the
// given shell bodies (keyed by command base name), recording each argv.
// Fake executables let findCommand resolve ssh tools that are not installed.
func stubSSH(t *testing.T, script map[string]string) (string, *[][]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "btrfs-replication")
	bin := t.TempDir()
	for _, name := range []string{"ssh-keygen", "ssh-keyscan"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldDir, oldSearch, oldExec := ReplicationDir, searchDirs, execCommand
	ReplicationDir = dir
	searchDirs = append(slices.Clone(oldSearch), bin)
	var calls [][]string
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		base := filepath.Base(name)
		calls = append(calls, append([]string{base}, args...))
		if body, ok := script[base]; ok {
			return exec.CommandContext(ctx, "sh", append([]string{"-c", body, base}, args...)...)
		}
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { ReplicationDir, searchDirs, execCommand = oldDir, oldSearch, oldExec })
	return dir, &calls
}

func TestEnsureSSHKeyGeneratesOnce(t *testing.T) {
	// The fake ssh-keygen writes "$9" (the -f argument) and its .pub.
	dir, calls := stubSSH(t, map[string]string{
		"ssh-keygen": `echo private > "$9"; echo ' ssh-ed25519 AAAAC3Nza linuxio-btrfs-replication ' > "$9.pub"`,
	})

	pub, err := EnsureSSHKey(context.Background())
	if err != nil {
		t.Fatalf("EnsureSSHKey: %v", err)
	}
	if pub != "ssh-ed25519 AAAAC3Nza linuxio-btrfs-replication" {
		t.Fatalf("public key = %q", pub)
	}
	key := filepath.Join(dir, "id_ed25519")
	want := [][]string{{"ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "linuxio-btrfs-replication", "-f", key}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls =\n%q\nwant\n%q", *calls, want)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir stat = %v, %v; want 0700", info, err)
	}

	if _, err = EnsureSSHKey(context.Background()); err != nil {
		t.Fatalf("second EnsureSSHKey: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("existing key regenerated: %q", *calls)
	}
}

func TestScanHostParsesKeys(t *testing.T) {
	_, calls := stubSSH(t, map[string]string{
		"ssh-keyscan": `printf '# backup.lan:2222 SSH-2.0-OpenSSH_9.6\n[backup.lan]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\n[backup.lan]:2222 ssh-rsa AAAAB3NzaC1yc2E=\n'`,
		"ssh-keygen":  `read host kind key; echo "256 SHA256:fp-$kind $host (X)"`,
	})

	fps, keys, err := ScanHost(context.Background(), "backup.lan", 2222)
	if err != nil {
		t.Fatalf("ScanHost: %v", err)
	}
	if want := []string{"SHA256:fp-ssh-ed25519", "SHA256:fp-ssh-rsa"}; !reflect.DeepEqual(fps, want) {
		t.Fatalf("fingerprints = %q, want %q", fps, want)
	}
	if want := []string{"[backup.lan]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", "[backup.lan]:2222 ssh-rsa AAAAB3NzaC1yc2E="}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %q, want %q", keys, want)
	}
	if want := []string{"ssh-keyscan", "-p", "2222", "-T", "10", "-t", "ed25519,rsa", "backup.lan"}; !reflect.DeepEqual((*calls)[0], want) {
		t.Fatalf("keyscan argv = %q, want %q", (*calls)[0], want)
	}
	if want := []string{"ssh-keygen", "-lf", "/dev/stdin"}; !reflect.DeepEqual((*calls)[1], want) {
		t.Fatalf("keygen argv = %q, want %q", (*calls)[1], want)
	}

	for _, host := range []string{"", "-oProxyCommand=x", "a b", "u@h"} {
		if _, _, err := ScanHost(context.Background(), host, 22); err == nil {
			t.Errorf("ScanHost(%q) accepted", host)
		}
	}
	if _, _, err := ScanHost(context.Background(), "backup.lan", 70000); err == nil {
		t.Error("ScanHost accepted port 70000")
	}
}

func TestScanHostNoKeys(t *testing.T) {
	stubSSH(t, map[string]string{"ssh-keyscan": `echo '# nothing'`})
	if _, _, err := ScanHost(context.Background(), "backup.lan", 22); err == nil {
		t.Fatal("ScanHost with no keys succeeded")
	}
}

func TestTrustHostIdempotent(t *testing.T) {
	dir, _ := stubSSH(t, nil)
	ed := "[backup.lan]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5"
	rsa := "backup.lan ssh-rsa AAAAB3NzaC1yc2E="

	if err := TrustHost(context.Background(), []string{ed}); err != nil {
		t.Fatalf("TrustHost: %v", err)
	}
	if err := TrustHost(context.Background(), []string{ed, rsa}); err != nil {
		t.Fatalf("TrustHost again: %v", err)
	}
	path := filepath.Join(dir, "known_hosts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), ed+"\n"+rsa+"\n"; got != want {
		t.Fatalf("known_hosts = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts stat = %v, %v; want 0600", info, err)
	}

	for _, line := range []string{"backup.lan ssh-dss AAAA", "backup.lan ssh-ed25519", "a b c d", "backup.lan ssh-ed25519 not*base64", strings.Repeat(" ", 3)} {
		if err := TrustHost(context.Background(), []string{line}); err == nil {
			t.Errorf("TrustHost(%q) accepted", line)
		}
	}
}

const (
	keyscanA = `printf 'backup.lan ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\nbackup.lan ssh-rsa AAAAB3NzaC1yc2E=\n'`
	keyscanB = `printf 'backup.lan ssh-ed25519 QkJCQkJCQkI=\n'`
	// The fake fingerprint is derived from the key, so a swapped key changes it.
	keygenFP = `read host kind key; echo "256 SHA256:$key $host (X)"`
)

func TestTrustApprovedHostTrustsOnlyApproved(t *testing.T) {
	dir, _ := stubSSH(t, map[string]string{"ssh-keyscan": keyscanA, "ssh-keygen": keygenFP})
	path := filepath.Join(dir, "known_hosts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := "backup.lan ssh-ed25519 U1RBTEU=\nother.lan ssh-rsa T1RIRVI=\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	shown, _, err := ScanHost(context.Background(), "backup.lan", 22)
	if err != nil {
		t.Fatal(err)
	}
	// Approve only the ed25519 key the user saw.
	trusted, err := TrustApprovedHost(context.Background(), "backup.lan", 22, shown[:1])
	if err != nil {
		t.Fatalf("TrustApprovedHost: %v", err)
	}
	if want := []string{"SHA256:AAAAC3NzaC1lZDI1NTE5"}; !reflect.DeepEqual(trusted, want) {
		t.Fatalf("trusted = %q, want %q", trusted, want)
	}
	data, _ := os.ReadFile(path)
	// The stale backup.lan key is replaced; other hosts and the unapproved rsa key are not touched or added.
	if got, want := string(data), "other.lan ssh-rsa T1RIRVI=\nbackup.lan ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\n"; got != want {
		t.Fatalf("known_hosts = %q, want %q", got, want)
	}
}

func TestTrustApprovedHostRejectsChangedKeys(t *testing.T) {
	script := map[string]string{"ssh-keyscan": keyscanA, "ssh-keygen": keygenFP}
	dir, _ := stubSSH(t, script)
	path := filepath.Join(dir, "known_hosts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	before := "backup.lan ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	shown, _, err := ScanHost(context.Background(), "backup.lan", 22)
	if err != nil {
		t.Fatal(err)
	}

	// An interceptor answers only the confirming scan.
	script["ssh-keyscan"] = keyscanB
	_, err = TrustApprovedHost(context.Background(), "backup.lan", 22, shown)
	if err == nil || err.Error() != "host keys changed since they were shown" {
		t.Fatalf("err = %v, want host keys changed", err)
	}
	if _, err = TrustApprovedHost(context.Background(), "backup.lan", 22, nil); err == nil {
		t.Fatal("TrustApprovedHost without approved fingerprints succeeded")
	}
	if data, _ := os.ReadFile(path); string(data) != before {
		t.Fatalf("known_hosts changed to %q", data)
	}
}
