package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
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
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

func runBridgeProcess() error {
	if configureErr := logging.Configure("linuxio-bridge", false); configureErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", configureErr)
	}

	sess, err := initializeBridgeSession()
	if err != nil {
		logBridgeStartupError("failed to initialize bridge session", err)
		return err
	}
	slog.Info("bridge boot",
		"effective_uid", os.Geteuid(),
		"user", sess.User.Username,
		"session_id", sess.SessionID,
		"privileged", sess.Privileged,
		"uid", sess.User.UID,
		"gid", sess.User.GID,
	)
	logBridgeResourceLimits()

	syscall.Umask(0o077)
	slog.Info("bridge starting", "uid", os.Geteuid())

	clientConn, err := openClientConnection()
	if err != nil {
		logBridgeStartupError("failed to open inherited client connection", err)
		return err
	}
	slog.Info("bridge connected to inherited client fd", "fd", clientConnFD)

	userConfig, err := config.OpenUserStore(sess.User.Username)
	if err != nil {
		logBridgeStartupError("failed to open config store", err)
		return err
	}
	slog.Info("config store ready", "user", sess.User.Username, "path", userConfig.Path())

	rt := runtime.New(sess, userConfig)
	runBridge(clientConn, rt)
	slog.Info("bridge stopped")
	return nil
}

func logBridgeStartupError(message string, err error) {
	slog.Error(message, "error", err)
}
