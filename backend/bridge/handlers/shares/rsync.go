package shares

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const rsyncUnit = "linuxio-rsync.service"

var (
	rsyncConfigFile    = "/etc/linuxio/rsyncd.conf"
	rsyncSecretsFile   = "/etc/linuxio/rsyncd.secrets"
	rsyncUnitFile      = "/etc/systemd/system/" + rsyncUnit
	rsyncLockFile      = "/run/lock/linuxio-rsync.lock"
	rsyncLookPath      = exec.LookPath
	rsyncStopUnit      = systemd.StopUnit
	rsyncStartUnit     = systemd.StartUnit
	rsyncEnableUnit    = systemd.EnableUnit
	rsyncDisableUnit   = systemd.DisableUnit
	rsyncActiveState   = systemd.GetActiveState
	rsyncUnitFileState = systemd.GetUnitFileState
	rsyncWaitReady     = waitRsyncReady
	rsyncCheckPort     = checkRsyncPort
	rsyncName          = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
)

func handleGetRsync(ctx context.Context, _ apischema.NoRequest) (apischema.RsyncStatus, error) {
	var status apischema.RsyncStatus
	err := filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		var err error
		status, err = getRsyncStatus(ctx)
		return err
	}, filelock.WithTimeout(10*time.Second))
	return status, err
}

func readRsyncConfig() (*apischema.RsyncConfig, error) {
	data, err := os.ReadFile(rsyncConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read rsync configuration: %w", err)
	}
	file, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, data)
	if err != nil {
		return nil, fmt.Errorf("parse rsync configuration: %w", err)
	}
	sections := file.Sections()
	if len(sections) != 2 {
		return nil, errors.New("LinuxIO rsync configuration must contain one backup module")
	}
	module := sections[1]
	hosts := strings.Fields(module.Key("hosts allow").String())
	if len(hosts) == 0 {
		return nil, errors.New("LinuxIO rsync configuration has no allowed NAS address")
	}
	port, err := file.Section("").Key("port").Int()
	if err != nil {
		return nil, fmt.Errorf("read rsync port: %w", err)
	}
	return &apischema.RsyncConfig{
		Module: module.Name(), Path: module.Key("path").String(),
		Username:   module.Key("auth users").String(),
		NASAddress: hosts[0], Port: port,
	}, nil
}

func getRsyncStatus(ctx context.Context) (apischema.RsyncStatus, error) {
	config, err := readRsyncConfig()
	status := apischema.RsyncStatus{Config: config}
	if err != nil || config == nil {
		return status, err
	}
	active, err := rsyncActiveState(ctx, rsyncUnit)
	if err != nil {
		return status, fmt.Errorf("read rsync service status: %w", err)
	}
	state, err := rsyncUnitFileState(ctx, rsyncUnit)
	if err != nil {
		return status, fmt.Errorf("read rsync startup status: %w", err)
	}
	status.Active = active == "active"
	status.Enabled = state == "enabled"
	return status, nil
}

func validateRsyncConfig(config *apischema.RsyncConfig) error {
	if !rsyncName.MatchString(config.Module) || strings.EqualFold(config.Module, "global") {
		return errors.New("module must be 1–64 letters, digits, underscores or hyphens, and cannot be global")
	}
	if !rsyncName.MatchString(config.Username) {
		return errors.New("backup username must be 1–64 letters, digits, underscores or hyphens")
	}
	address, err := netip.ParseAddr(config.NASAddress)
	if err != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
		return errors.New("enter a single NAS IP address")
	}
	config.NASAddress = address.Unmap().String()
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("rsync port must be between 1 and 65535")
	}
	path, err := validateRsyncPath(config.Path)
	if err != nil {
		return err
	}
	config.Path = path
	return nil
}

// validateRsyncPath resolves a backup folder to a form rsyncd.conf accepts.
func validateRsyncPath(requested string) (string, error) {
	if !filepath.IsAbs(requested) || strings.TrimSpace(requested) != requested || strings.ContainsAny(requested, "%\\") || strings.ContainsFunc(requested, unicode.IsControl) {
		return "", errors.New("backup folder must be an absolute path without control characters, percent signs or backslashes")
	}
	path, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve backup folder: %w", err)
	}
	if strings.ContainsAny(path, "%\\") || strings.ContainsFunc(path, unicode.IsControl) || strings.TrimSpace(path) != path {
		return "", errors.New("resolved backup folder contains unsupported characters")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read backup folder: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("backup path must be an existing folder")
	}
	return path, nil
}

func rsyncConfigText(config apischema.RsyncConfig) string {
	return fmt.Sprintf(`# Managed by LinuxIO. Configure this service in Shares > rsync.
port = %d
use chroot = yes
read only = yes
list = yes
timeout = 600
max connections = 4

[%s]
path = %s
comment = Read-only backup for TNAS
uid = root
gid = root
numeric ids = yes
hosts allow = %s 127.0.0.1 ::1
hosts deny = *
auth users = %s
secrets file = %s
strict modes = yes
`, config.Port, config.Module, config.Path, config.NASAddress, config.Username, rsyncSecretsFile)
}

func handleSaveRsync(ctx context.Context, req apischema.RsyncSaveRequest) (apischema.RsyncStatus, error) {
	if err := validateRsyncConfig(&req.RsyncConfig); err != nil {
		return apischema.RsyncStatus{}, err
	}
	binary, lookupErr := rsyncLookPath("rsync")
	if lookupErr != nil {
		return apischema.RsyncStatus{}, errors.New("install rsync from Settings > Capabilities first")
	}
	// systemd performs its own expansion, even without a shell.
	if !filepath.IsAbs(binary) || strings.ContainsAny(binary, "%$\"\\") || strings.ContainsFunc(binary, unicode.IsSpace) {
		return apischema.RsyncStatus{}, errors.New("rsync executable path contains unsupported characters")
	}
	var status apischema.RsyncStatus
	err := filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		if saveErr := saveRsync(ctx, req, binary); saveErr != nil {
			return saveErr
		}
		var statusErr error
		status, statusErr = getRsyncStatus(ctx)
		return statusErr
	}, filelock.WithTimeout(10*time.Second))
	return status, err
}

func rsyncSecret(req apischema.RsyncSaveRequest) (string, error) {
	secret := req.Password
	if secret == "" {
		data, err := os.ReadFile(rsyncSecretsFile)
		if err != nil {
			return "", errors.New("enter a backup password for the first setup")
		}
		username, password, ok := strings.Cut(strings.TrimSuffix(string(data), "\n"), ":")
		if !ok || username != req.Username {
			return "", errors.New("enter a new backup password when changing the username")
		}
		secret = password
	}
	if len(secret) < 12 || len(secret) > 128 || strings.ContainsFunc(secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", errors.New("backup password must be 12–128 characters without whitespace or control characters")
	}
	return secret, nil
}

func saveRsync(ctx context.Context, req apischema.RsyncSaveRequest, binary string) error {
	secret, secretErr := rsyncSecret(req)
	if secretErr != nil {
		return secretErr
	}
	// Stop existing transfers before changing access restrictions or credentials.
	if _, statErr := os.Stat(rsyncUnitFile); statErr == nil {
		if err := rsyncStopUnit(ctx, rsyncUnit); err != nil {
			return fmt.Errorf("stop rsync before saving: %w", err)
		}
		if err := waitRsyncStopped(ctx); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := rsyncCheckPort(ctx, req.Port); err != nil {
		return err
	}
	if err := writeRsyncFiles(req.RsyncConfig, secret, binary); err != nil {
		return err
	}
	if err := rsyncEnableUnit(ctx, rsyncUnit); err != nil {
		return fmt.Errorf("enable rsync service: %w", err)
	}
	if err := rsyncStartUnit(ctx, rsyncUnit); err != nil {
		return fmt.Errorf("start rsync service: %w", err)
	}
	if err := rsyncWaitReady(ctx, req.Port); err != nil {
		return fmt.Errorf("configuration saved, but rsync did not start; check whether port %d is already in use: %w", req.Port, err)
	}
	return nil
}

func checkRsyncPort(ctx context.Context, port int) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("rsync port %d is unavailable; stop the conflicting service or choose another port: %w", port, err)
	}
	return listener.Close()
}

func writeRsyncFiles(config apischema.RsyncConfig, secret, binary string) error {
	if err := utils.WriteFileAtomic(rsyncSecretsFile, []byte(config.Username+":"+secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("save rsync credentials: %w", err)
	}
	if err := utils.WriteFileAtomic(rsyncConfigFile, []byte(rsyncConfigText(config)), 0o600); err != nil {
		return fmt.Errorf("save rsync configuration: %w", err)
	}
	unit := fmt.Sprintf(`[Unit]
Description=LinuxIO read-only rsync backups
After=network.target

[Service]
Type=exec
ExecStart=%s --daemon --no-detach --config=%s
KillMode=control-group
UMask=0077

[Install]
WantedBy=multi-user.target
`, binary, rsyncConfigFile)
	if err := utils.WriteFileAtomic(rsyncUnitFile, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("save rsync service: %w", err)
	}
	return nil
}

func handleStopRsync(ctx context.Context, _ apischema.NoRequest) (apischema.RsyncStatus, error) {
	var status apischema.RsyncStatus
	err := filelock.RunExclusive(ctx, rsyncLockFile, func() error {
		if err := rsyncStopUnit(ctx, rsyncUnit); err != nil {
			return fmt.Errorf("stop rsync service: %w", err)
		}
		if err := waitRsyncStopped(ctx); err != nil {
			return err
		}
		if err := rsyncDisableUnit(ctx, rsyncUnit); err != nil {
			return fmt.Errorf("disable rsync startup: %w", err)
		}
		var err error
		status, err = getRsyncStatus(ctx)
		return err
	}, filelock.WithTimeout(10*time.Second))
	return status, err
}

func waitRsyncStopped(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		state, err := rsyncActiveState(ctx, rsyncUnit)
		if err != nil {
			return err
		}
		if state == "inactive" || state == "failed" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for rsync to stop: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitRsyncReady(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		state, err := rsyncActiveState(ctx, rsyncUnit)
		if err != nil {
			return err
		}
		if state == "failed" {
			return errors.New("rsync service failed")
		}
		if state == "active" {
			conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err == nil {
				_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				line, readErr := bufio.NewReaderSize(conn, 256).ReadSlice('\n')
				_ = conn.Close()
				if readErr == nil && strings.HasPrefix(string(line), "@RSYNCD:") {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for rsync listener: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
