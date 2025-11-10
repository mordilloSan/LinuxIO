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

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Use everywhere for bridge actions: returns *raw* JSON response string (for HTTP handler to decode output as needed)
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	// Log the incoming bridge call
	logger.DebugKV("bridge call initiated",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	socketPath := sess.SocketPath // <-- field, not method
	if socketPath == "" {
		err := fmt.Errorf("empty session.SocketPath")
		logger.ErrorKV("bridge call failed: invalid socket path",
			"user", sess.User.Username,
			"error", err)
		return nil, err
	}

	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	// Try to get persistent connection from session
	conn := sess.GetBridgeConn()

	// If no persistent connection or it's broken, create a new one and store it
	if conn == nil {
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
				err2 := fmt.Errorf("failed to connect to bridge (%s): %w", socketPath, err)
				logger.ErrorKV("bridge call failed: connection timeout",
					"user", sess.User.Username,
					"socket_path", socketPath,
					"error", err2)
				return nil, err
			}
			time.Sleep(step)
		}
		// Store the new connection in the session
		sess.SetBridgeConn(conn)
		logger.DebugKV("bridge persistent connection established",
			"user", sess.User.Username,
			"socket_path", socketPath)
	}

	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		// Connection is broken, clear it and retry
		closeBridgeConnWithLog(sess, "CallWithSession encode")
		err2 := fmt.Errorf("failed to send request to bridge: %w", err)
		logger.ErrorKV("bridge call failed: encoding error (clearing connection)",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err2)
		return CallWithSession(sess, reqType, command, args) // Retry with fresh connection
	}

	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		// Connection is broken, clear it
		closeBridgeConnWithLog(sess, "CallWithSession decode")
		err2 := fmt.Errorf("failed to decode response from bridge: %w", err)
		logger.ErrorKV("bridge call failed: decoding error (clearing connection)",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err2)
		return nil, err
	}

	// Log successful response
	logger.DebugKV("bridge call completed",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"response_bytes", len(raw))

	return []byte(raw), nil
}

func closeBridgeConnWithLog(sess *session.Session, context string) {
	if err := sess.CloseBridgeConn(); err != nil {
		logger.WarnKV("bridge connection close failed",
			"user", sess.User.Username,
			"context", context,
			"error", err)
	}
}

// CallWithSessionStream sends a request and reads multiple streaming responses.
// Each response chunk is passed to the onChunk callback until the stream ends.
func CallWithSessionStream(sess *session.Session, reqType, command string, args []string,
	onChunk func([]byte) error) error {

	logger.DebugKV("bridge streaming call initiated",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	socketPath := sess.SocketPath
	if socketPath == "" {
		err := fmt.Errorf("empty session.SocketPath")
		logger.ErrorKV("bridge call failed: invalid socket path",
			"user", sess.User.Username,
			"error", err)
		return err
	}

	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	// Get or create persistent connection
	conn := sess.GetBridgeConn()
	if conn == nil {
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
				err2 := fmt.Errorf("failed to connect to bridge (%s): %w", socketPath, err)
				logger.ErrorKV("bridge call failed: connection timeout",
					"user", sess.User.Username,
					"socket_path", socketPath,
					"error", err2)
				return err
			}
			time.Sleep(step)
		}
		sess.SetBridgeConn(conn)
		logger.DebugKV("bridge persistent connection established",
			"user", sess.User.Username,
			"socket_path", socketPath)
	}

	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		closeBridgeConnWithLog(sess, "CallWithSessionStream encode")
		err2 := fmt.Errorf("failed to send request to bridge: %w", err)
		logger.ErrorKV("bridge call failed: encoding error (clearing connection)",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err2)
		return err2
	}

	// Read responses until stream ends
	chunkCount := 0
	for {
		var resp ipc.Response
		if err := dec.Decode(&resp); err != nil {
			if err == io.EOF {
				// Normal stream end
				logger.DebugKV("bridge stream completed",
					"user", sess.User.Username,
					"type", reqType,
					"command", command,
					"chunks", chunkCount)
				return nil
			}
			closeBridgeConnWithLog(sess, "CallWithSessionStream decode")
			err2 := fmt.Errorf("failed to decode stream response: %w", err)
			logger.ErrorKV("bridge stream failed: decoding error (clearing connection)",
				"user", sess.User.Username,
				"type", reqType,
				"command", command,
				"error", err2)
			return err2
		}

		// Check for error in response
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}

		// Convert output to JSON bytes and pass to callback
		outBytes, err := json.Marshal(resp.Output)
		if err != nil {
			return fmt.Errorf("failed to marshal chunk: %w", err)
		}

		if err := onChunk(outBytes); err != nil {
			return err
		}

		chunkCount++
	}
}

// StartBridge launches linuxio-bridge via the setuid helper.
// Returns (privilegedMode, error). privilegedMode reflects the helper's decision.
func StartBridge(sess *session.Session, password string, envMode string, verbose bool, bridgeBinary string) (bool, error) {
	// Resolve bridge binary (helper also validates)
	if bridgeBinary == "" {
		bridgeBinary = GetBridgeBinaryPath("")
	}
	if bridgeBinary == "" {
		return false, errors.New("bridge binary not found (looked beside server and in PATH)")
	}

	helperPath := getAuthHelperPath()
	if helperPath == "" {
		return false, errors.New("auth helper not found; expected /usr/local/bin/linuxio-auth-helper or LINUXIO_PAM_HELPER override")
	}

	logger.Debugf("Using bridge binary: %s", bridgeBinary)
	logger.Debugf("Using auth helper: %s", helperPath)

	// Build env for the helper (helper now decides privilege itself)
	env := append(os.Environ(),
		"LINUXIO_ENV="+strings.ToLower(envMode),
		"LINUXIO_BRIDGE_BIN="+bridgeBinary,
		"LINUXIO_SESSION_ID="+sess.SessionID,
		"LINUXIO_SESSION_USER="+sess.User.Username,
		"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
		"LINUXIO_SOCKET_PATH="+sess.SocketPath,
	)
	if verbose {
		env = append(env, "LINUXIO_VERBOSE=1")
	}
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
		return false, fmt.Errorf("helper stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, fmt.Errorf("helper stderr pipe: %w", err)
	}
	// If your helper expects one line of password (or an empty line), just:
	if password == "" {
		cmd.Stdin = strings.NewReader("\n") // harmless if helper ignores it
	} else {
		cmd.Stdin = strings.NewReader(password + "\n")
	}

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start helper: %w", err)
	}

	// Read first line = MODE=...
	br := bufio.NewReader(stdout)

	readLine := func(timeout time.Duration) (string, error) {
		type res struct {
			s string
			e error
		}
		ch := make(chan res, 1)
		go func() {
			line, e := br.ReadString('\n')
			ch <- res{line, e}
		}()
		select {
		case r := <-ch:
			return strings.TrimSpace(r.s), r.e
		case <-time.After(timeout):
			return "", fmt.Errorf("timeout waiting for helper line")
		}
	}

	// capture stderr for diagnostics (non-blocking)
	seCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(stderr)
		seCh <- strings.TrimSpace(string(b))
	}()

	// Expect MODE line, then OK
	modeLine, e1 := readLine(2 * time.Second)
	if e1 != nil && e1 != io.EOF {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		if serr != "" {
			return false, fmt.Errorf("helper mode read error: %v (%s)", e1, serr)
		}
		return false, fmt.Errorf("helper mode read error: %v", e1)
	}

	privileged := false
	if strings.HasPrefix(modeLine, "MODE=") {
		if strings.EqualFold(modeLine, "MODE=privileged") {
			privileged = true
		} else {
			privileged = false
		}
		// read the next line for OK
		okLine, e2 := readLine(2 * time.Second)
		if e2 != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			var serr string
			select {
			case serr = <-seCh:
			default:
			}
			if serr != "" {
				return false, fmt.Errorf("helper did not confirm OK: %v (%s)", e2, serr)
			}
			return false, fmt.Errorf("helper did not confirm OK: %v", e2)
		}
		if okLine != "OK" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			var serr string
			select {
			case serr = <-seCh:
			default:
			}
			if serr != "" {
				return false, fmt.Errorf("helper did not confirm: %q (%s)", okLine, serr)
			}
			return false, fmt.Errorf("helper did not confirm: %q", okLine)
		}
	} else {
		// Unexpected first line
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		if serr != "" {
			return false, fmt.Errorf("unexpected helper output: %q (%s)", modeLine, serr)
		}
		return false, fmt.Errorf("unexpected helper output: %q", modeLine)
	}

	// Reap the parent helper (nanny owns bridge)
	if err := cmd.Wait(); err != nil {
		logger.WarnKV("auth helper exited non-zero after OK", "error", err)
	}

	logger.InfoKV("bridge launch acknowledged", "user", sess.User.Username, "privileged", privileged)
	return privileged, nil
}

// GetBridgeBinaryPath returns an absolute or name-only path for the bridge.
func GetBridgeBinaryPath(override string) string {
	const binaryName = "linuxio-bridge"

	if override != "" && isExec(override) {
		return override
	}
	if v := os.Getenv("LINUXIO_BRIDGE_BIN"); v != "" && isExec(v) {
		return v
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if isExec(candidate) {
			return candidate
		}
	}
	if path, err := exec.LookPath(binaryName); err == nil && isExec(path) {
		return path
	}
	logger.Debugf("[bridge.GetBridgeBinaryPath] %s not found beside server, or in user $PATH; consider installing into a well-known path or setting LINUXIO_BRIDGE_BIN.", binaryName)
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
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
