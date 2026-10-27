package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobshost"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/socketactivation"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// jobsSocketName is the FileDescriptorName= of linuxio-jobs@.socket.
const jobsSocketName = "linuxio-jobs"

// jobsHostActivated reports whether systemd started this process as the job
// host, linuxio-jobs@<uid>.service, by passing it the job host socket. The
// host takes no arguments: this is how the binary tells it from a bridge,
// which linuxio-auth starts.
func jobsHostActivated() bool {
	return os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) && os.Getenv("LISTEN_FDNAMES") == jobsSocketName
}

// runJobsHost runs this uid's job host on the socket systemd passed it, until
// SIGTERM or idle exit.
func runJobsHost(stderr io.Writer) int {
	runtimeDir, stateDir := os.Getenv("RUNTIME_DIRECTORY"), os.Getenv("STATE_DIRECTORY")
	listeners, err := socketactivation.Listeners()
	if err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge: job host socket: %v\n", err)
		return 2
	}
	var ln *net.UnixListener
	if len(listeners) == 1 {
		ln, _ = listeners[0].(*net.UnixListener)
	}
	if ln == nil || runtimeDir == "" || stateDir == "" {
		socketactivation.CloseListeners(listeners)
		fmt.Fprintln(stderr, "linuxio-bridge: the job host needs its socket, RUNTIME_DIRECTORY and STATE_DIRECTORY from linuxio-jobs@.service")
		return 2
	}
	if err := serveJobsHost(ln, runtimeDir, stateDir); err != nil {
		drainQueued(ln)
		logging.ReportFatal(err)
		return 1
	}
	return 0
}

// drainQueued closes the connections systemd queued for a host that cannot
// start. Left queued, they restart the failing host at once, again and again,
// until systemd's start limit fails the socket for the rest of the boot;
// closed, the clients' backoff paces the next start.
func drainQueued(ln *net.UnixListener) {
	if err := ln.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func serveJobsHost(ln *net.UnixListener, runtimeDir, stateDir string) error {
	if err := logging.Configure("linuxio-jobs", false); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	syscall.Umask(0o077)

	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	account, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return fmt.Errorf("look up job host user: %w", err)
	}
	store, err := userconfig.OpenUserStore(account.Username, uid, gid)
	if err != nil {
		return fmt.Errorf("failed to open config store: %w", err)
	}
	rt := runtime.New(&session.Session{
		SessionID:  "jobs-host",
		User:       session.User{Username: account.Username, UID: uid, GID: gid},
		Privileged: uid == 0,
		Timing:     session.Timing{CreatedAt: time.Now()},
	}, store)
	router := handlers.RegisterAllHandlers(rt)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	cfg := jobshost.Config{
		UID:        uid,
		GID:        gid,
		Username:   account.Username,
		Listener:   ln,
		Store:      jobs.NewStore(stateDir, runtimeDir),
		Router:     router,
		Tasks:      router.TaskService(),
		IdleExit:   60 * time.Second,
		MaxRunning: 256,
		Notify:     systemd.Notify,
		Now:        time.Now,
	}
	if uid == 0 {
		cfg.Alert = alerts.Raise
	}
	if err := jobshost.Serve(ctx, cfg); err != nil {
		return err
	}
	slog.Info("job host stopped", "user", account.Username, "uid", uid)
	return nil
}
