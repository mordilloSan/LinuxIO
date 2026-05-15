package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Bootstrap config and session - initialized in main() after CLI checks.
var bootCfg *authipc.Bootstrap
var sess *session.Session

// readBootstrap reads binary bootstrap from stdin.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe.
// FAIL-FAST: If bootstrap is invalid, exit immediately with code 1.
// This ensures the auth daemon's exec-status pipe detects failure.
func readBootstrap() *authipc.Bootstrap {
	b, err := authipc.ReadBootstrap(os.Stdin)
	if err != nil {
		slog.Error("failed to read bridge bootstrap", "error", err)
		os.Exit(1)
	}

	if b.SessionID == "" {
		slog.Error("bridge bootstrap missing session_id")
		os.Exit(1)
	}

	if b.Username == "" {
		slog.Error("bridge bootstrap missing username")
		os.Exit(1)
	}

	return b
}

// initializeBridgeSession reads bootstrap data and constructs the session
// object shared by handlers, routing, and audit metadata.
func initializeBridgeSession() {
	bootCfg = readBootstrap()
	if bootCfg.Verbose {
		if configureErr := logging.Configure("linuxio-bridge", true); configureErr != nil {
			fmt.Fprintf(os.Stderr, "failed to reconfigure logger: %v\n", configureErr)
			os.Exit(1)
		}
	}
	sess = &session.Session{
		SessionID:  bootCfg.SessionID,
		Privileged: bootCfg.Privileged,
		Timing: session.Timing{
			CreatedAt: time.Now(),
		},
		User: session.User{
			Username: bootCfg.Username,
			UID:      bootCfg.UID,
			GID:      bootCfg.GID,
		},
	}
}
