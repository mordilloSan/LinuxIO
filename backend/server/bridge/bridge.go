package bridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
)

// Use everywhere for bridge actions: returns *raw* JSON response string (for HTTP handler to decode output as needed)
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	socketPath := sess.SocketPath()
	return callViaSocket(socketPath, reqType, command, args, sess.BridgeSecret, sess.SessionID)
}

func callViaSocket(socketPath, reqType, command string, args []string, secret string, sessionID string) ([]byte, error) {
	req := ipc.Request{Type: reqType, Command: command, Secret: secret, Args: args, SessionID: sessionID}

	// Be tolerant to a just-started bridge: retry for a short window when
	// the socket may not exist yet or not accept connections immediately.
	var conn net.Conn
	var err error
	const (
		totalWait   = 2 * time.Second
		step        = 100 * time.Millisecond
		dialTimeout = 500 * time.Millisecond
	)
	deadline := time.Now().Add(totalWait)
	for {
		conn, err = net.DialTimeout("unix", socketPath, dialTimeout)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to connect to bridge: %w", err)
		}
		time.Sleep(step)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close connection: %v", cerr)
		}
	}()

	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)

	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request to bridge: %w", err)
	}

	// Read exactly one JSON value from the stream and return it as-is.
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode response from bridge: %w", err)
	}
	return []byte(raw), nil
}

// StartBridge launches linuxio-bridge via the setuid helper.
// The helper handles PAM, privilege mode (root vs user), and double-forking.
// We only wait for a single "OK\n" line and then return.
func StartBridge(sess *session.Session, password string, envMode string, verbose bool, bridgeBinary string) error {
	// Resolve bridge binary (helper also validates)
	if bridgeBinary == "" {
		bridgeBinary = GetBridgeBinaryPath("", envMode)
	}
	if bridgeBinary == "" {
		return errors.New("bridge binary not found (looked beside server and in PATH)")
	}

	helperPath := getAuthHelperPath()
	if helperPath == "" {
		return errors.New("auth helper not found; expected /usr/local/bin/linuxio-auth-helper or LINUXIO_PAM_HELPER override")
	}

	// Build env for the helper
	env := append(os.Environ(),
		"LINUXIO_TARGET_USER="+sess.User.Username,
		"LINUXIO_ENV="+strings.ToLower(envMode),
		"LINUXIO_BRIDGE_BIN="+bridgeBinary,
		"LINUXIO_PRIV="+map[bool]string{true: "1", false: "0"}[sess.Privileged],

		"LINUXIO_SESSION_ID="+sess.SessionID,
		"LINUXIO_SESSION_USER="+sess.User.Username,
		"LINUXIO_SESSION_UID="+sess.User.UID,
		"LINUXIO_SESSION_GID="+sess.User.GID,
		"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
	)
	if v := os.Getenv("LINUXIO_SERVER_BASE_URL"); v != "" {
		env = append(env, "LINUXIO_SERVER_BASE_URL="+v)
	}
	if v := os.Getenv("LINUXIO_SERVER_CERT"); v != "" {
		env = append(env, "LINUXIO_SERVER_CERT="+v)
	}

	cmd := exec.Command(helperPath)
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("helper stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("helper stderr pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("helper stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start helper: %w", err)
	}

	// Send password (one line)
	go func() {
		defer func() { _ = stdin.Close() }()
		_, _ = io.WriteString(stdin, password+"\n")
	}()

	// Wait for single "OK\n" with timeout
	okCh := make(chan string, 1)
	errCh := make(chan error, 1)
	seCh := make(chan string, 1)

	go func() {
		br := bufio.NewReader(stdout)
		line, rerr := br.ReadString('\n')
		if rerr != nil {
			errCh <- rerr
			return
		}
		okCh <- strings.TrimSpace(line)
	}()

	// capture stderr for diagnostics
	go func() {
		b, _ := io.ReadAll(stderr)
		seCh <- strings.TrimSpace(string(b))
	}()

	select {
	case line := <-okCh:
		if line != "OK" {
			var serr string
			select {
			case serr = <-seCh:
			case <-time.After(200 * time.Millisecond):
			}
			_ = cmd.Wait()
			if serr != "" {
				return fmt.Errorf("helper did not confirm: %q (%s)", line, serr)
			}
			return fmt.Errorf("helper did not confirm: %q", line)
		}
	case e := <-errCh:
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		_ = cmd.Wait()
		if serr != "" {
			return fmt.Errorf("helper error: %v (%s)", e, serr)
		}
		return fmt.Errorf("helper error: %v", e)
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		if serr != "" {
			return fmt.Errorf("helper timeout waiting for OK: %s", serr)
		}
		return errors.New("helper timeout waiting for OK")
	}

	// Reap the parent helper (nanny owns bridge)
	if err := cmd.Wait(); err != nil {
		logger.Warnf("auth helper exited non-zero after OK: %v", err)
	}

	logger.Infof("Bridge launch acknowledged (session=%s, user=%s, privileged=%v)",
		sess.SessionID, sess.User.Username, sess.Privileged)
	return nil
}

// GetBridgeBinaryPath returns an absolute or name-only path for the bridge.
func GetBridgeBinaryPath(override, envMode string) string {
	const binaryName = "linuxio-bridge"

	if override != "" && isExec(override) {
		return override
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if isExec(candidate) {
			return candidate
		}
	}
	if path, err := exec.LookPath(binaryName); err == nil {
		return path
	}
	logger.Debugf("%s not found beside server, or in user $PATH; consider installing into a well-known path or pass --bridge-binary.", binaryName)
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0111 != 0
}

func getAuthHelperPath() string {
	if v := os.Getenv("LINUXIO_PAM_HELPER"); v != "" && isExec(v) {
		return v
	}
	const legacy = "/usr/local/bin/linuxio-auth-helper"
	if isExec(legacy) {
		return legacy
	}
	if p, err := exec.LookPath("linuxio-auth-helper"); err == nil && isExec(p) {
		return p
	}
	return ""
}
