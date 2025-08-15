package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/cmd/bridge/handlers/types"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

var (
	processes   = make(map[string]*types.BridgeProcess)
	processesMu sync.Mutex
)

// cache lookups to avoid repeated NSS calls
var uidCache sync.Map // username -> uid string

// BridgeSocketPath returns the per-session bridge socket path for the user.
// It does NOT include the SessionID anywhere in the path to avoid leaking it.
// Preferred base is /run/user/<uid>; falls back to /tmp/linuxio-run/<uid>.
func BridgeSocketPath(sess *session.Session) (string, error) {
	uid, err := lookupUIDCached(sess.User.ID)
	if err != nil {
		return "", fmt.Errorf("lookup user %q: %w", sess.User.ID, err)
	}

	// Prefer systemd user runtime dir
	base := filepath.Join("/run/user", uid)
	return filepath.Join(base, bridgeSocketFilename(sess.BridgeSecret)), nil
}

func lookupUIDCached(username string) (string, error) {
	if v, ok := uidCache.Load(username); ok {
		uidStr, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("cached UID for %q has invalid type %T", username, v)
		}
		return uidStr, nil
	}

	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}

	uid := u.Uid
	uidCache.Store(username, uid)
	return uid, nil
}

// bridgeSocketFilename builds a short, opaque name from the BridgeSecret.
// Example: linuxio-bridge-8f1c...f2a3.sock
func bridgeSocketFilename(secret string) string {
	// Derive a stable, non-reversible token from the secret (no SessionID).
	sum := sha256.Sum256([]byte("linuxio-sock:" + secret))
	// Keep it short to stay well under AF_UNIX path limits: first 12 bytes -> 24 hex chars.
	token := hex.EncodeToString(sum[:12])
	return "linuxio-bridge-" + token + ".sock"
}

// Use everywhere for bridge actions: returns *raw* JSON response string (for HTTP handler to decode output as needed)
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	socketPath, err := BridgeSocketPath(sess)
	if err != nil {
		return nil, fmt.Errorf("could not determine bridge socket path: %w", err)
	}
	return callViaSocket(socketPath, reqType, command, args, sess.BridgeSecret)
}

func callViaSocket(socketPath, reqType, command string, args []string, secret string) ([]byte, error) {
	req := map[string]any{
		"type":    reqType,
		"command": command,
		"secret":  secret,
	}
	if args != nil {
		req["args"] = args
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to bridge: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close connection: %v", cerr)
		}
	}()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	err = enc.Encode(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to bridge: %w", err)
	}

	var resp types.BridgeResponse
	err = dec.Decode(&resp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response from bridge: %w", err)
	}

	var b []byte
	b, err = json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bridge response: %w", err)
	}

	return b, nil
}

// StartBridge now takes env/verbose/bridgeBinaryPath from the caller (flags),
// and uses ONLY the three per-session env vars for secure hand-off.
func StartBridge(sess *session.Session, sudoPassword string, envMode string, verbose bool, bridgeBinary string) error {
	processesMu.Lock()
	defer processesMu.Unlock()

	if _, exists := processes[sess.SessionID]; exists {
		return errors.New("bridge already running for this session")
	}

	// Resolve bridge path if needed
	if bridgeBinary == "" {
		bridgeBinary = GetBridgeBinaryPath("", envMode)
	}

	// Verify binary
	info, err := os.Stat(bridgeBinary)
	if err != nil {
		logger.Errorf("Bridge binary not found at path: %s", bridgeBinary)
		return fmt.Errorf("bridge binary not found at path: %s", bridgeBinary)
	}
	if info.Mode()&0111 == 0 {
		logger.Errorf("Bridge binary at %s is not executable", bridgeBinary)
		return fmt.Errorf("bridge binary at %s is not executable", bridgeBinary)
	}

	// ---- Build argv flags for bridge ----
	args := []string{"--env", strings.ToLower(envMode)}
	if verbose {
		args = append(args, "--verbose")
	}

	// ---- Minimal env just for session hand-off (safer than argv) ----
	childEnv := append(os.Environ(),
		"LINUXIO_SESSION_ID="+sess.SessionID,
		"LINUXIO_SESSION_USER="+sess.User.ID,
		"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
	)

	// Build command
	var cmd *exec.Cmd
	if sess.Privileged {
		// Preserve only the three session vars
		preserve := "LINUXIO_SESSION_ID,LINUXIO_SESSION_USER,LINUXIO_BRIDGE_SECRET"
		sudoArgs := []string{"-S", "--preserve-env=" + preserve, "--", bridgeBinary}
		sudoArgs = append(sudoArgs, args...)
		cmd = exec.Command("sudo", sudoArgs...)
		cmd.Env = childEnv

		if sudoPassword != "" {
			stdin, perr := cmd.StdinPipe()
			if perr != nil {
				logger.Errorf("Failed to get stdin pipe: %v", perr)
				return perr
			}
			pwBytes := []byte(sudoPassword + "\n")
			go func() {
				defer func() {
					if cerr := stdin.Close(); cerr != nil {
						logger.Warnf("failed to close stdin: %v", cerr)
					}
					for i := range pwBytes {
						pwBytes[i] = 0
					}
				}()
				if _, werr := stdin.Write(pwBytes); werr != nil {
					logger.Warnf("failed to write sudo password to stdin: %v", werr)
				}
			}()
		}
	} else {
		cmd = exec.Command(bridgeBinary, args...)
		cmd.Env = childEnv
	}

	prod := strings.ToLower(envMode) == "production"

	if prod {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		devnull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		// Keep stdin only if providing a sudo password
		if !sess.Privileged || sudoPassword == "" {
			cmd.Stdin = devnull
		}
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var stdoutBuf, stderrBuf bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("Failed to start bridge for session %s using %s: %v", sess.SessionID, bridgeBinary, err)
		return err
	}

	if sess.Privileged {
		logger.Infof("Started privileged bridge for session %s (pid=%d) using %s", sess.SessionID, cmd.Process.Pid, bridgeBinary)
	} else {
		logger.Infof("Started bridge for session %s (pid=%d) using %s", sess.SessionID, cmd.Process.Pid, bridgeBinary)
	}

	processes[sess.SessionID] = &types.BridgeProcess{
		Cmd:       cmd,
		SessionID: sess.SessionID,
		StartedAt: time.Now(),
	}

	go func(sessID string, cmd *exec.Cmd) {
		err := cmd.Wait()
		processesMu.Lock()
		delete(processes, sessID)
		processesMu.Unlock()
		if err != nil {
			logger.Warnf("Bridge for session %s exited with error: %v", sessID, err)
		}
	}(sess.SessionID, cmd)

	return nil
}

// isExec reports whether path exists and is executable (not a dir).
func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0111 != 0
}

// GetBridgeBinaryPath returns an absolute or name-only path for the bridge.
// Search order:
//  1. explicit override (if provided and executable)
//  2. next to the server executable (prod-friendly)
//  3. in development: walk up from CWD for a few levels
//  4. PATH (current user)
//  5. fallback to plain name (sudo may still find it via secure_path)
func GetBridgeBinaryPath(override, envMode string) string {
	const binaryName = "linuxio-bridge"

	// 1) explicit override
	if override != "" {
		if isExec(override) {
			return override
		}
		logger.Warnf("bridge override is not executable: %s", override)
	}

	// 2) next to the server executable
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if isExec(candidate) {
			return candidate
		}
	}

	// 3) dev: walk upward from CWD (project-root style)
	if strings.ToLower(envMode) == "development" {
		if dir, err := os.Getwd(); err == nil {
			for i := 0; i < 5 && dir != string(filepath.Separator); i++ {
				candidate := filepath.Join(dir, binaryName)
				if isExec(candidate) {
					return candidate
				}
				dir = filepath.Dir(dir)
			}
		}
	}

	// 4) PATH (current user)
	if path, err := exec.LookPath(binaryName); err == nil {
		return path
	}

	// 5) last resort: name only (sudo may resolve via secure_path)
	// (downgrade to debug to avoid noisy false alarms when sudo finds it)
	logger.Debugf("%s not found beside server, in dev tree, or in user $PATH; "+
		"will attempt plain name (sudo may resolve via secure_path). "+
		"Consider passing --bridge-binary or installing into a well-known path.",
		binaryName)
	return binaryName
}
