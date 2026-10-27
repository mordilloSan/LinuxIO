// Package jobslauncher is the root launcher behind jobs-launcher.sock. A
// non-root bridge cannot start a system unit, so the launcher, socket-activated
// with Accept=yes, learns the caller's uid from the kernel and starts that
// uid's job host, nothing else. It reads nothing from the client.
package jobslauncher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	// LauncherSocketPath is the world-connectable socket systemd listens on.
	LauncherSocketPath = "/run/linuxio/jobs-launcher.sock"

	disallowedUsersPath = "/etc/linuxio/disallowed-users"
	defaultWaitTimeout  = 5 * time.Second
	defaultPoll         = 50 * time.Millisecond
)

// refusal is a failed launch: reason is the constant text the client sees,
// cause (optional) is only for the log.
type refusal struct {
	reason string
	cause  error
}

func (r *refusal) Error() string {
	if r.cause == nil {
		return r.reason
	}
	return r.reason + ": " + r.cause.Error()
}

func (r *refusal) Unwrap() error { return r.cause }

// Deps are Serve's collaborators; a zero field takes its production default.
type Deps struct {
	Peer        func(*net.UnixConn) (peercred.Cred, error)
	LookupUser  func(uid uint32) (string, error)
	Disallowed  func(username string) (bool, error)
	StartUnit   func(ctx context.Context, unit string) error
	SocketPath  func(uid uint32) string
	WaitTimeout time.Duration
	Poll        time.Duration
}

// UnitName is the systemd unit of a uid's job host.
func UnitName(uid uint32) string {
	return "linuxio-jobs@" + strconv.FormatUint(uint64(uid), 10) + ".service"
}

// HostSocketPath is where a uid's job host listens.
func HostSocketPath(uid uint32) string {
	return "/run/linuxio/jobs/" + strconv.FormatUint(uint64(uid), 10) + "/host.sock"
}

func (d Deps) withDefaults() Deps {
	if d.Peer == nil {
		d.Peer = peercred.Read
	}
	if d.LookupUser == nil {
		d.LookupUser = lookupUsername
	}
	if d.Disallowed == nil {
		d.Disallowed = func(username string) (bool, error) { return disallowedIn(disallowedUsersPath, username) }
	}
	if d.StartUnit == nil {
		d.StartUnit = systemd.StartUnit
	}
	if d.SocketPath == nil {
		d.SocketPath = HostSocketPath
	}
	if d.WaitTimeout <= 0 {
		d.WaitTimeout = defaultWaitTimeout
	}
	if d.Poll <= 0 {
		d.Poll = defaultPoll
	}
	return d
}

func lookupUsername(uid uint32) (string, error) {
	account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return "", err
	}
	return account.Username, nil
}

// disallowedIn reports whether username is listed in the pam_listfile style
// file at path: one name per line, # comments, blank lines. A missing file
// disallows nobody.
func disallowedIn(path, username string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == username {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// Serve handles one launcher connection: it starts the peer's own job host,
// waits for its socket, and replies one line, "ok" or "error: <reason>". It
// returns the error it replied with, naming the uid and the cause; the caller
// logs it.
func Serve(ctx context.Context, conn *net.UnixConn, d Deps) error {
	d = d.withDefaults()
	uid, unit, err := launch(ctx, conn, d)
	if err != nil {
		_, _ = fmt.Fprintf(conn, "error: %s\n", err.reason)
		return fmt.Errorf("uid %d: %w", uid, err)
	}
	slog.Debug("job host launched", "uid", uid, "unit", unit)
	_, _ = conn.Write([]byte("ok\n"))
	return nil
}

func launch(ctx context.Context, conn *net.UnixConn, d Deps) (uid uint32, unit string, _ *refusal) {
	cred, err := d.Peer(conn)
	if err != nil {
		return 0, "", &refusal{"unknown uid", err}
	}
	uid = cred.UID
	username, err := d.LookupUser(uid)
	if err != nil {
		return uid, "", &refusal{"unknown uid", err}
	}
	if blocked, err := d.Disallowed(username); err != nil || blocked {
		return uid, "", &refusal{"user is not allowed", err}
	}
	unit = UnitName(uid)
	if err := d.StartUnit(ctx, unit); err != nil {
		return uid, unit, &refusal{"start failed", err}
	}
	if !waitForSocket(ctx, d.SocketPath(uid), d.WaitTimeout, d.Poll) {
		return uid, unit, &refusal{"host did not start", nil}
	}
	return uid, unit, nil
}

func waitForSocket(ctx context.Context, path string, timeout, poll time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}
