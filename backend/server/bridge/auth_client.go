// Package bridge provides a client for the linuxio-auth daemon.
package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

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

// Type aliases for backward compatibility
type Request = protocol.AuthRequest
type Response = protocol.AuthResponse

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

// Authenticate sends an auth request to the auth daemon.
// Returns (privileged, motd, error). If the daemon is not available or fails,
// returns an error so the caller can fall back to exec mode.
func Authenticate(req *Request) (privileged bool, motd string, err error) {
	if !DaemonAvailable() {
		return false, "", errors.New("auth daemon not available")
	}

	// Connect to daemon
	conn, err := net.DialTimeout("unix", GetAuthSocketPath(), authDialTimeout)
	if err != nil {
		return false, "", fmt.Errorf("failed to connect to auth daemon: %w", err)
	}
	defer conn.Close()

	// Set timeouts
	if dearlineErr := conn.SetWriteDeadline(time.Now().Add(authWriteTimeout)); dearlineErr != nil {
		return false, "", fmt.Errorf("failed to set write deadline: %w", dearlineErr)
	}

	// Encode and send request (newline-terminated)
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return false, "", fmt.Errorf("failed to marshal auth request: %w", err)
	}
	reqBytes = append(reqBytes, '\n')

	if _, err := conn.Write(reqBytes); err != nil {
		return false, "", fmt.Errorf("failed to send auth request: %w", err)
	}

	// Read response
	if err := conn.SetReadDeadline(time.Now().Add(authReadTimeout)); err != nil {
		return false, "", fmt.Errorf("failed to set read deadline: %w", err)
	}

	// Read until newline or EOF
	buf := make([]byte, 4096)
	total := 0
	for total < len(buf)-1 {
		n, err := conn.Read(buf[total:])
		if n > 0 {
			total += n
			// Check for newline
			for i := 0; i < total; i++ {
				if buf[i] == '\n' {
					total = i + 1
					break
				}
			}
		}
		if err != nil {
			break
		}
		// Check if we found newline
		if total > 0 && buf[total-1] == '\n' {
			break
		}
	}

	if total == 0 {
		return false, "", errors.New("empty response from auth daemon")
	}

	// Parse response
	var resp Response
	if err := json.Unmarshal(buf[:total], &resp); err != nil {
		return false, "", fmt.Errorf("failed to parse auth response: %w (raw: %q)", err, string(buf[:total]))
	}

	if !resp.IsOK() {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "authentication failed"
		}
		return false, "", fmt.Errorf("auth daemon error: %s", errMsg)
	}

	privileged = resp.IsPrivileged()
	logger.InfoKV("auth daemon: bridge spawned",
		"user", req.User,
		"privileged", privileged,
		"socket_path", resp.SocketPath)

	return privileged, resp.Motd, nil
}

// BuildRequest creates a Request from a session and additional auth parameters
func BuildRequest(sess *session.Session, password, bridgePath, envMode string, verbose bool) *Request {
	req := &Request{
		User:       sess.User.Username,
		Password:   password,
		SessionID:  sess.SessionID,
		SocketPath: sess.SocketPath,
		BridgePath: bridgePath,
		Env:        envMode,
		Secret:     sess.BridgeSecret,
	}

	if verbose {
		req.Verbose = "1"
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
