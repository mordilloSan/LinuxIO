package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/debugserver"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Run validates invocation mode, builds the authenticated bridge session, and
// hands the inherited client connection to the bridge runtime. It returns the
// process exit code; only main should call os.Exit.
func Run(args []string) int {
	return run(args, os.Stdin)
}

func run(args []string, stdin *os.File) int {
	if handled, exitCode := handleBridgeArgs(args); handled {
		return exitCode
	}

	direct, err := isDirectBridgeInvocation(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if direct {
		fmt.Fprintln(os.Stderr, "linuxio-bridge must be spawned by the auth daemon")
		return 2
	}

	if err := runBridgeProcess(); err != nil {
		logging.ReportFatal(err)
		return 1
	}
	return 0
}

func runBridgeProcess() error {
	if configureErr := logging.Configure("linuxio-bridge", false); configureErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", configureErr)
	}
	debugserver.Start("127.0.0.1:6061")

	sess, readyAck, err := initializeBridgeSession()
	if err != nil {
		// No ack possible: a bootstrap that fails to parse cannot be trusted
		// to have set the ReadyAck flag, so the launcher learns of this exit
		// via EOF on the status fd.
		return fmt.Errorf("failed to initialize bridge session: %w", err)
	}
	status := newStartupStatus(readyAck)
	// Catch-all: any exit before the startup handoff reports a typed failure
	// instead of leaving the launcher to infer death from EOF. After ready()
	// claims the handoff this is a no-op.
	defer status.fail("bridge exited before becoming ready")
	slog.Info("bridge boot",
		"effective_uid", os.Geteuid(),
		"user", sess.User.Username,
		"session_ref", session.DiagnosticRef(sess.SessionID),
		"privileged", sess.Privileged,
		"uid", sess.User.UID,
		"gid", sess.User.GID,
	)
	logBridgeResourceLimits()

	syscall.Umask(0o077)

	clientConn, err := openClientConnection()
	if err != nil {
		status.fail("bridge cannot open inherited client connection: " + err.Error())
		return fmt.Errorf("failed to open inherited client connection: %w", err)
	}
	slog.Debug("bridge connected to inherited client fd", "fd", clientConnFD)

	userConfig, err := userconfig.OpenUserStore(sess.User.Username, sess.User.UID, sess.User.GID)
	if err != nil {
		status.fail("bridge config store failed: " + err.Error())
		return fmt.Errorf("failed to open config store: %w", err)
	}
	slog.Debug("config store ready", "user", sess.User.Username, "storage_mode", userConfig.StorageMode(), "path", userConfig.Path(), "ui_path", userConfig.UIPath())

	rt := runtime.New(sess, userConfig)
	runBridge(clientConn, rt, status.ready)
	slog.Info("bridge stopped")
	return nil
}
