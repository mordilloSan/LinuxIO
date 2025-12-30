// Package bridge provides a client for the linuxio-auth daemon.
package bridge

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/protocol"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/go_logger/logger"
)

const (
	// DefaultAuthSocketPath is the Unix socket where linuxio-auth daemon listens
	DefaultAuthSocketPath = "/run/linuxio/auth.sock"

	// Timeouts for auth daemon communication
	authDialTimeout  = 5 * time.Second
	authReadTimeout  = 30 * time.Second // sudo check can take time
	authWriteTimeout = 5 * time.Second
)

// GetAuthSocketPath returns the auth socket path from env var or default
func GetAuthSocketPath() string {
	if path := os.Getenv("LINUXIO_AUTH_SOCKET"); path != "" {
		return path
	}
	return DefaultAuthSocketPath
}

// DaemonAvailable checks if the auth daemon socket exists and is connectable
func DaemonAvailable() bool {
	info, err := os.Stat(GetAuthSocketPath())
	if err != nil {
		return false
	}
	// Check it's a socket
	if info.Mode()&os.ModeSocket == 0 {
		return false
	}
	return true
}

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
	if !DaemonAvailable() {
		return nil, errors.New("auth daemon not available")
	}

	// Connect to daemon
	conn, err := net.DialTimeout("unix", GetAuthSocketPath(), authDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to auth daemon: %w", err)
	}

	// Set timeouts
	if deadlineErr := conn.SetWriteDeadline(time.Now().Add(authWriteTimeout)); deadlineErr != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set write deadline: %w", deadlineErr)
	}

	// Write binary request
	if err := protocol.WriteAuthRequest(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send auth request: %w", err)
	}

	// Read response
	if err := conn.SetReadDeadline(time.Now().Add(authReadTimeout)); err != nil {
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
func BuildRequest(sess *session.Session, password, bridgePath, envMode string, verbose bool) *protocol.AuthRequest {
	// Convert envMode string to binary value
	var envModeBin uint8 = protocol.ProtoEnvProduction
	if envMode == appconfig.EnvDevelopment {
		envModeBin = protocol.ProtoEnvDevelopment
	}

	req := &protocol.AuthRequest{
		User:       sess.User.Username,
		Password:   password,
		SessionID:  sess.SessionID,
		BridgePath: bridgePath,
		EnvMode:    envModeBin,
		Verbose:    verbose,
		Secret:     sess.BridgeSecret,
	}

	// Pass server URL and cert for bridge callback
	if v := os.Getenv("LINUXIO_SERVER_BASE_URL"); v != "" {
		req.ServerBaseURL = v
	}
	if v := os.Getenv("LINUXIO_SERVER_CERT"); v != "" {
		req.ServerCert = v
	}

	return req
}
