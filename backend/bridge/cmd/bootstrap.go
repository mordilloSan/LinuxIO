package cmd

import (
	"errors"
	"fmt"
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
// Bootstrap errors are returned to main as exit code 1 so the auth daemon's
// exec-status pipe detects startup failure.
func readBootstrap() (*authipc.Bootstrap, error) {
	b, err := authipc.ReadBootstrap(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read bridge bootstrap: %w", err)
	}

	if b.SessionID == "" {
		return b, errors.New("bridge bootstrap missing session_id")
	}

	if b.Username == "" {
		return b, errors.New("bridge bootstrap missing username")
	}

	return b, nil
}

// initializeBridgeSession reads bootstrap data and constructs the session
// object shared by handlers, routing, and audit metadata.
func initializeBridgeSession() error {
	var err error
	bootCfg, err = readBootstrap()
	if err != nil {
		return err
	}
	if bootCfg.Verbose {
		if configureErr := logging.Configure("linuxio-bridge", true); configureErr != nil {
			return fmt.Errorf("failed to reconfigure logger: %w", configureErr)
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
	return nil
}
