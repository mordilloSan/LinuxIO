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
		return nil, errors.New("bridge bootstrap missing session_id")
	}

	if b.Username == "" {
		return nil, errors.New("bridge bootstrap missing username")
	}

	return b, nil
}

// initializeBridgeSession reads bootstrap data and constructs the session
// object shared by handlers, routing, and audit metadata.
func initializeBridgeSession() (*session.Session, error) {
	bootstrap, err := readBootstrap()
	if err != nil {
		return nil, err
	}
	if bootstrap.Verbose {
		if configureErr := logging.Configure("linuxio-bridge", true); configureErr != nil {
			return nil, fmt.Errorf("failed to reconfigure logger: %w", configureErr)
		}
	}
	sess := &session.Session{
		SessionID:  bootstrap.SessionID,
		Privileged: bootstrap.Privileged,
		Timing: session.Timing{
			CreatedAt: time.Now(),
		},
		User: session.User{
			Username: bootstrap.Username,
			UID:      bootstrap.UID,
			GID:      bootstrap.GID,
		},
	}
	return sess, nil
}
