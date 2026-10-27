package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
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
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// runJobsHost runs this uid's job host, linuxio-jobs@<uid>.service, until
// SIGTERM or idle exit.
func runJobsHost(stderr io.Writer) int {
	runtimeDir, stateDir := os.Getenv("RUNTIME_DIRECTORY"), os.Getenv("STATE_DIRECTORY")
	if runtimeDir == "" || stateDir == "" {
		fmt.Fprintln(stderr, "linuxio-bridge jobs host must be started by systemd with RUNTIME_DIRECTORY and STATE_DIRECTORY set")
		return 2
	}
	if err := serveJobsHost(runtimeDir, stateDir); err != nil {
		logging.ReportFatal(err)
		return 1
	}
	return 0
}

func serveJobsHost(runtimeDir, stateDir string) error {
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
		SocketPath: filepath.Join(runtimeDir, "host.sock"),
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
