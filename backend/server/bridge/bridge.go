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

// CallTypedWithSession makes a bridge call and decodes the response directly into result.
// This helper wraps CallWithSession and decodes the bridge response.
func CallTypedWithSession(sess *session.Session, reqType, command string, args []string, result interface{}) error {
	raw, err := CallWithSession(sess, reqType, command, args)
	if err != nil {
		return err
	}

	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode bridge response: %w", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("bridge error: %s", resp.Error)
	}

	if result == nil || len(resp.Output) == 0 {
		return nil
	}

	if err := json.Unmarshal(resp.Output, result); err != nil {
		return fmt.Errorf("decode bridge output: %w", err)
	}

	return nil
}

// CallWithSession makes a bridge call and returns raw JSON response bytes.
// Returns *raw* JSON response string (for HTTP handler to decode output as needed)
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	// Log the incoming bridge call
	logger.DebugKV("bridge call initiated",
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
		return nil, err
	}

	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	conn, err := dialBridge(socketPath)
	if err != nil {
		logger.ErrorKV("bridge call failed: connection timeout",
			"user", sess.User.Username,
			"socket_path", socketPath,
			"error", err)
		return nil, err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.WarnKV("bridge conn close failed", "socket_path", socketPath, "error", cerr)
		}
	}()

	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		err2 := fmt.Errorf("failed to send request to bridge: %w", err)
		logger.ErrorKV("bridge call failed: encoding error",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err2)
		return nil, err
	}

	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		err2 := fmt.Errorf("failed to decode response from bridge: %w", err)
		logger.ErrorKV("bridge call failed: decoding error",
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

// ============================================================================
// NEW: Framed Protocol Support (Binary + Streaming)
// ============================================================================

// dialBridge creates a connection to the bridge socket
func dialBridge(socketPath string) (net.Conn, error) {
	const (
		totalWait   = 2 * time.Second
		step        = 100 * time.Millisecond
		dialTimeout = 500 * time.Millisecond
	)

	var conn net.Conn
	var err error
	deadline := time.Now().Add(totalWait)

	for {
		conn, err = net.DialTimeout("unix", socketPath, dialTimeout)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to connect to bridge (%s): %w", socketPath, err)
		}
		time.Sleep(step)
	}
}

// StreamWithSession opens a streaming connection for continuous responses
// Returns a StreamReader that must be closed by the caller
// Use this for: journald logs, tail -f, live stats, etc.
func StreamWithSession(sess *session.Session, reqType, command string, args []string) (*ipc.StreamReader, error) {
	logger.DebugKV("bridge stream initiated",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	if sess.SocketPath == "" {
		return nil, fmt.Errorf("empty session.SocketPath")
	}

	conn, err := dialBridge(sess.SocketPath)
	if err != nil {
		logger.ErrorKV("bridge stream failed: connection error",
			"user", sess.User.Username,
			"socket_path", sess.SocketPath,
			"error", err)
		return nil, err
	}

	// Send request using framed protocol
	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	if err := ipc.WriteRequestFrame(conn, &req); err != nil {
		conn.Close()
		logger.ErrorKV("bridge stream failed: send error",
			"user", sess.User.Username,
			"error", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	logger.DebugKV("bridge stream established",
		"user", sess.User.Username,
		"type", reqType,
		"command", command)

	return ipc.NewStreamReader(conn), nil
}

// DownloadFromSession downloads binary data from bridge
// Returns an io.ReadCloser that streams binary chunks
// Use this for: file downloads, archive downloads, etc.
func DownloadFromSession(sess *session.Session, reqType, command string, args []string) (io.ReadCloser, error) {
	logger.DebugKV("bridge download initiated",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	stream, err := StreamWithSession(sess, reqType, command, args)
	if err != nil {
		return nil, err
	}

	// First frame should be a status response
	resp, msgType, err := stream.Read()
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to read initial response: %w", err)
	}

	if msgType != ipc.MsgTypeJSON {
		stream.Close()
		return nil, fmt.Errorf("expected JSON response, got type 0x%02x", msgType)
	}

	if resp.Status != "ok" {
		stream.Close()
		return nil, fmt.Errorf("bridge error: %s", resp.Error)
	}

	// Now stream is ready to read binary chunks
	return ipc.NewBinaryReader(stream), nil
}

// UploadToSession uploads binary data to bridge
// Reads from the provided reader and sends as binary chunks
// Use this for: file uploads, archive uploads, etc.
func UploadToSession(sess *session.Session, reqType, command string, args []string, data io.Reader) error {
	logger.DebugKV("bridge upload initiated",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	if sess.SocketPath == "" {
		return fmt.Errorf("empty session.SocketPath")
	}

	conn, err := dialBridge(sess.SocketPath)
	if err != nil {
		logger.ErrorKV("bridge upload failed: connection error",
			"user", sess.User.Username,
			"error", err)
		return err
	}
	defer conn.Close()

	// Send request
	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	if err := ipc.WriteRequestFrame(conn, &req); err != nil {
		logger.ErrorKV("bridge upload failed: send request error",
			"user", sess.User.Username,
			"error", err)
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Read initial response
	var resp ipc.Response
	msgType, err := ipc.ReadJSONFrame(conn, &resp)
	if err != nil {
		return fmt.Errorf("failed to read initial response: %w", err)
	}

	if msgType != ipc.MsgTypeJSON {
		return fmt.Errorf("expected JSON response, got type 0x%02x", msgType)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("bridge error: %s", resp.Error)
	}

	// Stream binary data in chunks
	const chunkSize = 512 * 1024 // 512KB chunks
	buf := make([]byte, chunkSize)

	for {
		n, err := data.Read(buf)
		if n > 0 {
			if err := ipc.WriteBinaryFrame(conn, buf[:n]); err != nil {
				return fmt.Errorf("failed to send binary chunk: %w", err)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read data: %w", err)
		}
	}

	// Send empty binary frame to signal end
	if err := ipc.WriteBinaryFrame(conn, nil); err != nil {
		return fmt.Errorf("failed to send end marker: %w", err)
	}

	// Read final response
	msgType, err = ipc.ReadJSONFrame(conn, &resp)
	if err != nil {
		return fmt.Errorf("failed to read final response: %w", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("upload failed: %s", resp.Error)
	}

	logger.DebugKV("bridge upload completed",
		"user", sess.User.Username,
		"type", reqType,
		"command", command)

	return nil
}
