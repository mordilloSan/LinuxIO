// Package bridge provides a client for the linuxio-auth daemon.
package bridge

import (
	"fmt"
	"net"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/protocol"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	// DefaultAuthSocketPath is the Unix socket where linuxio-auth daemon listens
	DefaultAuthSocketPath = "/run/linuxio/auth.sock"

	// Timeouts for auth daemon communication
	authDialTimeout  = 5 * time.Second
	authReadTimeout  = 30 * time.Second // sudo check can take time
	authWriteTimeout = 5 * time.Second
)

// AuthResult contains the result of a successful authentication
type AuthResult struct {
	Conn       net.Conn // Connection to bridge (same socket, now connected to forked bridge)
	Privileged bool
	Motd       string
}

// Authenticate sends an auth request to the auth daemon.
// On success, returns the connection which is now connected to the forked bridge
// process (the auth daemon passed our FD to the bridge via dup2).
// The caller owns the connection and must close it.
func Authenticate(req *protocol.AuthRequest) (*AuthResult, error) {
	// Connect to daemon
	conn, err := net.DialTimeout("unix", DefaultAuthSocketPath, authDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to auth daemon: %w", err)
	}

	// Set timeouts
	if deadlineErr := conn.SetWriteDeadline(time.Now().Add(authWriteTimeout)); deadlineErr != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set write deadline: %w", deadlineErr)
	}

	// Write binary request
	if err = protocol.WriteAuthRequest(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send auth request: %w", err)
	}

	// Read response
	if err = conn.SetReadDeadline(time.Now().Add(authReadTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	resp, err := protocol.ReadAuthResponse(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read auth response: %w", err)
	}

	if !resp.IsOK() {
		conn.Close()
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "authentication failed"
		}
		return nil, fmt.Errorf("auth daemon error: %s", errMsg)
	}

	privileged := resp.IsPrivileged()
	logger.InfoKV("auth daemon: bridge spawned",
		"user", req.User,
		"privileged", privileged)

	// Clear deadlines for Yamux use
	_ = conn.SetDeadline(time.Time{})

	return &AuthResult{
		Conn:       conn,
		Privileged: privileged,
		Motd:       resp.Motd,
	}, nil
}

// BuildRequest creates a Request from a session and additional auth parameters
func BuildRequest(sess *session.Session, password string, verbose bool) *protocol.AuthRequest {
	return &protocol.AuthRequest{
		User:      sess.User.Username,
		Password:  password,
		SessionID: sess.SessionID,
		Verbose:   verbose,
	}
}
