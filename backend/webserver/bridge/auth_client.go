// Package bridge provides a client for the linuxio-auth daemon.
package bridge

import (
	"fmt"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
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
	User       session.User
	Privileged bool
}

// AuthError carries a structured auth result from the auth daemon.
type AuthError struct {
	Code    ipc.AuthResultCode
	Message string
}

func (e *AuthError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *AuthError) IsUnauthorized() bool {
	return e != nil && e.Code.IsUnauthorized()
}

// Authenticate sends an auth request to the auth daemon.
// On success, returns the connection which is now connected to the forked bridge
// process (the auth daemon passed our FD to the bridge via dup2).
// The caller owns the connection and must close it.
func Authenticate(req *ipc.AuthRequest) (*AuthResult, error) {
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
	if err = ipc.WriteAuthRequest(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send auth request: %w", err)
	}

	// Read response
	if err = conn.SetReadDeadline(time.Now().Add(authReadTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	resp, err := ipc.ReadAuthResponse(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read auth response: %w", err)
	}

	if !resp.IsOK() {
		conn.Close()
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = resp.ResultCode.DefaultMessage()
		}
		return nil, &AuthError{
			Code:    resp.ResultCode,
			Message: errMsg,
		}
	}

	// Clear deadlines for Yamux use
	privileged := resp.IsPrivileged()
	if err = conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to clear deadlines: %w", err)
	}

	return &AuthResult{
		Conn:       conn,
		User:       resp.User,
		Privileged: privileged,
	}, nil
}

// BuildRequest creates a Request from auth parameters.
func BuildRequest(username, sessionID, password string, verbose bool) *ipc.AuthRequest {
	return &ipc.AuthRequest{
		User:      username,
		Password:  password,
		SessionID: sessionID,
		Verbose:   verbose,
	}
}
