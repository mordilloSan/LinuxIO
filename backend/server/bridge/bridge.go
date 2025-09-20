package bridge

import (
	"bufio"
	"bytes"
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
	helperPath := getAuthHelperPath()

	logger.Infof("[bridge] env=%s privileged=%v", strings.ToLower(envMode), sess.Privileged)
	logger.Infof("[bridge] bridgeBinary=%q", bridgeBinary)
	logger.Infof("[bridge] authHelper=%q", helperPath)

	if bridgeBinary == "" {
		return errors.New("bridge binary not found (looked beside server and in PATH)")
	}
	if helperPath == "" {
		return errors.New("auth helper not found; expected /usr/local/bin/linuxio-auth-helper or LINUXIO_PAM_HELPER override")
	}

	env := append(os.Environ(),
		"LANG=C", // ensure predictable PAM error text
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
		return fmt.Errorf("helper stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer // <-- new
	cmd.Stderr = &stderrBuf    // <-- capture stderr into buffer

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

	go func() {
		br := bufio.NewReader(stdout)
		line, rerr := br.ReadString('\n')
		if rerr != nil {
			errCh <- rerr
			return
		}
		okCh <- strings.TrimSpace(line)
	}()

	// choose a slightly longer timeout to be kind to PAM on cold systems????
	timeout := 2 * time.Second

	select {
	case line := <-okCh:
		if line != "OK" {
			_ = cmd.Wait() // ensure stderr is complete
			serr := strings.TrimSpace(stderrBuf.String())
			if serr != "" {
				return fmt.Errorf("helper did not confirm: %q (%s)", line, serr)
			}
			return fmt.Errorf("helper did not confirm: %q", line)
		}

	case e := <-errCh:
		_ = cmd.Wait() // ensure stderr is complete
		serr := strings.TrimSpace(stderrBuf.String())
		if serr != "" {
			return fmt.Errorf("helper error: %v (%s)", e, serr)
		}
		return fmt.Errorf("helper error: %v", e)

	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		serr := strings.TrimSpace(stderrBuf.String())
		if serr != "" {
			return fmt.Errorf("helper timeout waiting for OK: %s", serr)
		}
		return errors.New("helper timeout waiting for OK")
	}

	// Reap the parent helper (nanny owns bridge now)
	if err := cmd.Wait(); err != nil {
		// in normal path helper parent may exit non-zero; log but don’t fail
		logger.Warnf("auth helper exited non-zero after OK: %v", err)
	}

	logger.Infof("Bridge launch acknowledged (session=%s, user=%s, privileged=%v)",
		sess.SessionID, sess.User.Username, sess.Privileged)
	return nil
}

// GetBridgeBinaryPath returns the path to the bridge binary.
// In dev mode, prefer local (repo root / CWD). In prod, use beside server → PATH.
// If 'override' is non-empty and executable, it wins.
func GetBridgeBinaryPath(override, envMode string) string {
	const name = "linuxio-bridge"
	env := strings.ToLower(strings.TrimSpace(envMode))
	isDev := env == "dev" || env == "development"

	if override != "" && isExec(override) {
		return override
	}

	if isDev {
		// 1) explicit repo root (set by Makefile/Air)
		if root := strings.TrimSpace(os.Getenv("LINUXIO_PROJECT_ROOT")); root != "" {
			if p := filepath.Join(root, name); isExec(p) {
				return p
			}
		}
		// 2) current working dir
		if wd, _ := os.Getwd(); wd != "" {
			if p := filepath.Join(wd, name); isExec(p) {
				return p
			}
		}
	}

	// prod (or dev fallback): beside server binary
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); isExec(p) {
			return p
		}
	}
	// PATH
	if p, err := exec.LookPath(name); err == nil && isExec(p) {
		return p
	}

	logger.Debugf("%s not found (env=%s). Consider LINUXIO_PROJECT_ROOT or --bridge-binary.", name, env)
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0111 != 0
}

// BEFORE:
// func getAuthHelperPath(envMode string) string {

// AFTER:
func getAuthHelperPath() string {
	// 1) explicit override
	if v := os.Getenv("LINUXIO_PAM_HELPER"); v != "" && isExec(v) {
		return v
	}

	// 2) installed SUID helper (preferred)
	const installed = "/usr/local/bin/linuxio-auth-helper"
	if isExec(installed) {
		// soft check for setuid bit; still return if stat fails
		if st, err := os.Stat(installed); err == nil {
			if (st.Mode() & os.ModeSetuid) != 0 {
				return installed
			}
		}
		return installed
	}
	if p, err := exec.LookPath("linuxio-auth-helper"); err == nil && isExec(p) {
		return p
	}

	// 3) optional local fallback for CI/mocks only
	allowLocal := strings.EqualFold(os.Getenv("LINUXIO_USE_LOCAL_HELPER"), "1") ||
		strings.EqualFold(os.Getenv("LINUXIO_USE_LOCAL_HELPER"), "true")
	if allowLocal {
		if root := strings.TrimSpace(os.Getenv("LINUXIO_PROJECT_ROOT")); root != "" {
			if p := filepath.Join(root, "linuxio-auth-helper"); isExec(p) {
				return p
			}
		}
		if wd, _ := os.Getwd(); wd != "" {
			if p := filepath.Join(wd, "linuxio-auth-helper"); isExec(p) {
				return p
			}
		}
	}

	return ""
}
