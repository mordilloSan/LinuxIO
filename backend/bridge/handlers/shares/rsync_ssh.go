package shares

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// TOS's encrypted mode logs in over SSH and runs `rsync --daemon` as that
// account, which reads rsyncd.conf from the account's home directory. LinuxIO
// writes that file and keeps a symlink to it so the saved module can be found.
const (
	rsyncSSHModule        = "backup"
	rsyncSSHHeader        = "# Managed by LinuxIO. Configure this module in Shares > rsync > SSH.\n"
	rsyncSSHAccountPrefix = "# account = "
)

var (
	rsyncSSHLink    = "/etc/linuxio/rsyncd-ssh.conf"
	rsyncLookupUser = user.Lookup
)

func handleGetRsyncSSH(ctx context.Context, req apischema.RsyncSSHRequest) (apischema.RsyncSSHStatus, error) {
	if req.Port < 1 || req.Port > 65535 {
		return apischema.RsyncSSHStatus{}, errors.New("SSH port must be between 1 and 65535")
	}
	err := probeRsyncSSH(ctx, req.Port)
	if ctx.Err() != nil {
		return apischema.RsyncSSHStatus{}, ctx.Err()
	}
	status := apischema.RsyncSSHStatus{Available: err == nil, Port: req.Port}
	if err != nil {
		status.Error = utils.OptionalString(fmt.Sprintf("SSH did not respond on local port %d. Check the SSH service and its listening address in Services. This check does not test access from the NAS.", req.Port))
	}
	err = filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		var readErr error
		status.Config, readErr = readRsyncSSHConfig()
		return readErr
	}, filelock.WithTimeout(10*time.Second))
	return status, err
}

func probeRsyncSSH(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("connect to local SSH: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	return readSSHIdentification(conn)
}

func readSSHIdentification(reader io.Reader) error {
	// SSH permits informational lines before its identification. Bound both
	// their count and length so an unrelated listener cannot exhaust memory.
	buffer := bufio.NewReaderSize(reader, 256)
	for range 50 {
		line, err := buffer.ReadSlice('\n')
		if err != nil {
			return fmt.Errorf("read SSH identification: %w", err)
		}
		if strings.HasPrefix(string(line), "SSH-2.0-") || strings.HasPrefix(string(line), "SSH-1.99-") {
			return nil
		}
		if strings.HasPrefix(string(line), "SSH-") || strings.HasPrefix(string(line), "@RSYNCD:") {
			return errors.New("listener does not support SSH 2")
		}
	}
	return errors.New("SSH identification was not received")
}

func rsyncSSHConfigText(username, path string) string {
	// rsync rejects unknown parameters, so the account is recorded in a comment.
	return rsyncSSHHeader + rsyncSSHAccountPrefix + username + fmt.Sprintf(`
# rsync reads this file when TOS runs "rsync --daemon" over an SSH login as this account.

[%s]
path = %s
comment = Read-only backup for TNAS over SSH
read only = yes
use chroot = no
munge symlinks = no
list = yes
`, rsyncSSHModule, path)
}

// readRsyncSSHConfig follows the LinuxIO symlink to the account's rsyncd.conf.
// A missing link or file means no SSH module is configured.
func readRsyncSSHConfig() (*apischema.RsyncSSHConfig, error) {
	target, err := os.Readlink(rsyncSSHLink)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read rsync SSH link: %w", err)
	}
	data, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read rsync SSH configuration: %w", err)
	}
	lines := strings.SplitN(string(data), "\n", 3)
	username, ok := strings.CutPrefix(lines[min(1, len(lines)-1)], rsyncSSHAccountPrefix)
	if lines[0]+"\n" != rsyncSSHHeader || !ok || !rsyncName.MatchString(username) {
		return nil, fmt.Errorf("%s is not managed by LinuxIO", target)
	}
	file, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, data)
	if err != nil {
		return nil, fmt.Errorf("parse rsync SSH configuration: %w", err)
	}
	sections := file.Sections()
	if len(sections) != 2 {
		return nil, errors.New("LinuxIO rsync SSH configuration must contain one backup module")
	}
	return &apischema.RsyncSSHConfig{Username: username, Path: sections[1].Key("path").String(), Module: sections[1].Name()}, nil
}

func handleSaveRsyncSSH(ctx context.Context, req apischema.RsyncSSHSaveRequest) (apischema.RsyncSSHConfig, error) {
	account, uid, gid, err := lookupRsyncSSHAccount(req.Username)
	if err != nil {
		return apischema.RsyncSSHConfig{}, err
	}
	path, err := validateRsyncPath(req.Path)
	if err != nil {
		return apischema.RsyncSSHConfig{}, err
	}
	var config *apischema.RsyncSSHConfig
	err = filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		if writeErr := writeRsyncSSHConfig(account, uid, gid, path); writeErr != nil {
			return writeErr
		}
		var readErr error
		config, readErr = readRsyncSSHConfig()
		return readErr
	}, filelock.WithTimeout(10*time.Second))
	if err != nil {
		return apischema.RsyncSSHConfig{}, err
	}
	if config == nil {
		return apischema.RsyncSSHConfig{}, errors.New("saved rsync SSH configuration could not be read back")
	}
	return *config, nil
}

func lookupRsyncSSHAccount(username string) (*user.User, int, int, error) {
	if !rsyncName.MatchString(username) {
		return nil, 0, 0, errors.New("the Linux username must be 1–64 letters, digits, underscores or hyphens")
	}
	account, err := rsyncLookupUser(username)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("account %q was not found; create it in Accounts first", username)
	}
	uid, uidErr := strconv.Atoi(account.Uid)
	gid, gidErr := strconv.Atoi(account.Gid)
	if uidErr != nil || gidErr != nil {
		return nil, 0, 0, fmt.Errorf("account %q has an invalid uid or gid", username)
	}
	if uid == 0 {
		return nil, 0, 0, errors.New("choose a regular account: rsync over SSH as root reads /etc/rsyncd.conf instead of the home configuration")
	}
	info, err := os.Stat(account.HomeDir)
	if err != nil || !info.IsDir() || !filepath.IsAbs(account.HomeDir) {
		return nil, 0, 0, fmt.Errorf("home directory %q of %q is not available", account.HomeDir, username)
	}
	return account, uid, gid, nil
}

func writeRsyncSSHConfig(account *user.User, uid, gid int, path string) error {
	target := filepath.Join(account.HomeDir, "rsyncd.conf")
	if data, err := os.ReadFile(target); err == nil && !strings.HasPrefix(string(data), rsyncSSHHeader) {
		return fmt.Errorf("%s already exists and is not managed by LinuxIO", target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing rsync SSH configuration: %w", err)
	}
	// Switching accounts leaves no module behind in the previous home.
	if previous, err := os.Readlink(rsyncSSHLink); err == nil && previous != target {
		if err := removeManagedRsyncSSHFile(previous); err != nil {
			return err
		}
	}
	if err := utils.WriteFileAtomic(target, []byte(rsyncSSHConfigText(account.Username, path)), 0o644, uid, gid); err != nil {
		return fmt.Errorf("save rsync SSH configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(rsyncSSHLink), 0o755); err != nil {
		return fmt.Errorf("create rsync SSH link directory: %w", err)
	}
	tmp := rsyncSSHLink + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("link rsync SSH configuration: %w", err)
	}
	if err := os.Rename(tmp, rsyncSSHLink); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("link rsync SSH configuration: %w", err)
	}
	return nil
}

// removeManagedRsyncSSHFile deletes target only when LinuxIO wrote it.
func removeManagedRsyncSSHFile(target string) error {
	data, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !strings.HasPrefix(string(data), rsyncSSHHeader)) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read rsync SSH configuration: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rsync SSH configuration: %w", err)
	}
	return nil
}

func handleRemoveRsyncSSH(ctx context.Context, _ apischema.NoRequest) (apischema.SuccessResponse, error) {
	err := filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		target, err := os.Readlink(rsyncSSHLink)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read rsync SSH link: %w", err)
		}
		if err := removeManagedRsyncSSHFile(target); err != nil {
			return err
		}
		if err := os.Remove(rsyncSSHLink); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove rsync SSH link: %w", err)
		}
		return nil
	}, filelock.WithTimeout(10*time.Second))
	return apischema.SuccessResponse{Success: err == nil}, err
}
