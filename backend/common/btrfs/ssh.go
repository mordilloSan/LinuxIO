package btrfs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ReplicationDir holds the replication SSH key pair and known_hosts. Tests
// may override it.
var ReplicationDir = version.ConfigDir + "/btrfs-replication"

// BtrfsSSHKey is the public half of the replication key.
type BtrfsSSHKey struct {
	PublicKey string `json:"publicKey"`
}

// BtrfsSSHTrust is the result of scanning, and optionally trusting, a host.
type BtrfsSSHTrust struct {
	Fingerprints []string `json:"fingerprints"`
	Trusted      bool     `json:"trusted"`
}

var hostKeyTypes = []string{"ssh-ed25519", "ssh-rsa"}

func sshKeyPath() string     { return filepath.Join(ReplicationDir, "id_ed25519") }
func knownHostsPath() string { return filepath.Join(ReplicationDir, "known_hosts") }

// EnsureSSHKey creates the replication key pair when missing and returns the
// public key. The private key never leaves ReplicationDir.
func EnsureSSHKey(ctx context.Context) (string, error) {
	if err := os.MkdirAll(ReplicationDir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", ReplicationDir, err)
	}
	key := sshKeyPath()
	if _, statErr := os.Stat(key); errors.Is(statErr, fs.ErrNotExist) {
		if _, err := runCommand(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "linuxio-btrfs-replication", "-f", key); err != nil {
			return "", fmt.Errorf("generate replication ssh key: %w", err)
		}
	} else if statErr != nil {
		return "", fmt.Errorf("stat %s: %w", key, statErr)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return "", fmt.Errorf("read replication public key: %w", err)
	}
	return strings.TrimSpace(string(pub)), nil
}

// ScanHost fetches the host keys of host:port and returns their SHA256
// fingerprints alongside the known_hosts lines for TrustHost. Port 0 is 22.
func ScanHost(ctx context.Context, host string, port int) (fingerprints, keys []string, err error) {
	if port == 0 {
		port = defaultSSHPort
	}
	if err = validateSSHHost(host, port); err != nil {
		return nil, nil, err
	}
	out, err := runCommand(ctx, "ssh-keyscan", "-p", strconv.Itoa(port), "-T", "10", "-t", "ed25519,rsa", host)
	if err != nil {
		return nil, nil, fmt.Errorf("scan host keys of %s: %w", host, err)
	}
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := validateHostKeyLine(line); err != nil {
			return nil, nil, err
		}
		fp, err := runCommandInput(ctx, line+"\n", "ssh-keygen", "-lf", "/dev/stdin")
		if err != nil {
			return nil, nil, fmt.Errorf("fingerprint host key of %s: %w", host, err)
		}
		fields := strings.Fields(fp)
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "SHA256:") {
			return nil, nil, fmt.Errorf("unexpected fingerprint output %q", fp)
		}
		fingerprints = append(fingerprints, fields[1])
		keys = append(keys, line)
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("no ed25519 or rsa host keys found for %s", host)
	}
	return fingerprints, keys, nil
}

func validateHostKeyLine(line string) error {
	fields := strings.Fields(line)
	if len(fields) != 3 || !slices.Contains(hostKeyTypes, fields[1]) {
		return fmt.Errorf("invalid host key line %q", line)
	}
	if _, err := base64.StdEncoding.DecodeString(fields[2]); err != nil {
		return fmt.Errorf("invalid host key line %q: %w", line, err)
	}
	return nil
}

// TrustApprovedHost scans host:port again and trusts only the keys whose
// fingerprints the user approved. It writes nothing when any approved
// fingerprint is no longer offered, and returns the fingerprints trusted.
func TrustApprovedHost(ctx context.Context, host string, port int, approved []string) ([]string, error) {
	if len(approved) == 0 {
		return nil, errors.New("approved host key fingerprints are required")
	}
	fingerprints, keys, err := ScanHost(ctx, host, port)
	if err != nil {
		return nil, err
	}
	for _, fp := range approved {
		if !slices.Contains(fingerprints, fp) {
			return nil, errors.New("host keys changed since they were shown")
		}
	}
	var trustedFPs, trustedKeys []string
	for i, fp := range fingerprints {
		if slices.Contains(approved, fp) {
			trustedFPs = append(trustedFPs, fp)
			trustedKeys = append(trustedKeys, keys[i])
		}
	}
	if err := TrustHost(ctx, trustedKeys); err != nil {
		return nil, err
	}
	return trustedFPs, nil
}

// TrustHost writes the given known_hosts lines, replacing any existing lines
// for the same host field so a stale key does not stay trusted.
func TrustHost(ctx context.Context, keys []string) error {
	hosts := map[string]bool{}
	var lines []string
	for _, k := range keys {
		if err := validateHostKeyLine(k); err != nil {
			return err
		}
		fields := strings.Fields(k)
		hosts[fields[0]] = true
		if line := strings.Join(fields, " "); !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	if err := os.MkdirAll(ReplicationDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", ReplicationDir, err)
	}
	path := knownHostsPath()
	return filelock.RunExclusive(ctx, path+".lock", func() error {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var out strings.Builder
		for line := range strings.Lines(string(data)) {
			fields := strings.Fields(line)
			if len(fields) == 0 || hosts[fields[0]] {
				continue
			}
			out.WriteString(strings.TrimRight(line, "\n") + "\n")
		}
		for _, line := range lines {
			out.WriteString(line + "\n")
		}
		if err := atomicfile.Write(path, []byte(out.String()), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	}, filelock.WithTimeout(rulesLockWait))
}

// SSHArgs are the ssh arguments, without the ssh binary itself, that reach a
// job's target with the replication key and strict host key checking.
func SSHArgs(job BtrfsReplicationJob) []string {
	port := job.Target.Port
	if port == 0 {
		port = defaultSSHPort
	}
	return []string{
		"-i", sshKeyPath(),
		"-o", "UserKnownHostsFile=" + knownHostsPath(),
		"-o", "StrictHostKeyChecking=yes",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=30",
		"-p", strconv.Itoa(port),
		job.Target.User + "@" + job.Target.Host,
	}
}
