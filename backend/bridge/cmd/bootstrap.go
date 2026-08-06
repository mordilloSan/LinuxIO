package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// readBootstrap reads binary bootstrap from r.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe;
// callers outside tests pass os.Stdin.
// Bootstrap errors are returned to main as exit code 1; process exit closes
// the inherited startup-status fd so the auth launcher observes EOF.
func readBootstrap(r io.Reader) (*authipc.Bootstrap, error) {
	b, err := authipc.ReadBootstrap(r)
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
// object shared by handlers, routing, and audit metadata. The second return
// reports whether the launcher expects a ready/error ack on the inherited
// startup-status fd.
func initializeBridgeSession() (*session.Session, bool, error) {
	bootstrap, err := readBootstrap(os.Stdin)
	if err != nil {
		return nil, false, err
	}
	if bootstrap.Verbose {
		if configureErr := logging.Configure("linuxio-bridge", true); configureErr != nil {
			return nil, false, fmt.Errorf("failed to reconfigure logger: %w", configureErr)
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
	return sess, bootstrap.ReadyAck, nil
}
