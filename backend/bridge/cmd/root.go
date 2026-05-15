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

// runBridgeProcess validates invocation mode, builds the authenticated bridge
// session, and hands the inherited client connection to the bridge runtime.
func RunBridgeProcess() {
	if handledArgs := handleBridgeArgs(); handledArgs {
		return
	}

	if isDirectBridgeInvocation() {
		fmt.Println("(to be spawned by auth daemon, not for direct use)")
		return
	}

	if configureErr := logging.Configure("linuxio-bridge", false); configureErr != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", configureErr)
		os.Exit(1)
	}

	initializeBridgeSession()
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

	clientConn := openClientConnection()
	slog.Info("bridge connected to inherited client fd", "fd", clientConnFD)

	userConfig, err := config.OpenUserStore(sess.User.Username)
	if err != nil {
		slog.Error("failed to open config store", "user", sess.User.Username, "error", err)
		os.Exit(1)
	}
	slog.Info("config store ready", "user", sess.User.Username, "path", userConfig.Path())

	rt := runtime.New(sess, userConfig)
	runBridge(clientConn, rt)
	slog.Info("bridge stopped")
}
