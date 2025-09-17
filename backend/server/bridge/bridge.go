package bridge

import (
    "encoding/json"
    "errors"
    "fmt"
    "net"
    "os"
    "os/exec"
    "os/user"
    "path/filepath"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/mordilloSan/LinuxIO/common/ipc"
    "github.com/mordilloSan/LinuxIO/common/logger"
    "github.com/mordilloSan/LinuxIO/common/session"
)

var (
	processes   = make(map[string]*ipc.BridgeProcess)
	processesMu sync.Mutex
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

// StartBridge starts a bridge process for the given session
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
		"LINUXIO_SESSION_USER="+sess.User.Username,
		"LINUXIO_SESSION_UID="+sess.User.UID,
		"LINUXIO_SESSION_GID="+sess.User.GID,
		"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
	)

	needsSu := false
	if sess.Privileged && os.Geteuid() != 0 {
		needsSu = true
		if cu, err := user.Current(); err == nil {
			if cu.Username == sess.User.Username || (sess.User.UID != "" && cu.Uid == sess.User.UID) {
				needsSu = false
			}
		}
	}

	// Build command
	var cmd *exec.Cmd
	if sess.Privileged {
		preserve := "LINUXIO_SESSION_ID,LINUXIO_SESSION_USER,LINUXIO_SESSION_UID,LINUXIO_SESSION_GID,LINUXIO_BRIDGE_SECRET"
		sudoArgs := []string{"-S", "-p", "", "--preserve-env=" + preserve, "--", bridgeBinary}
		sudoArgs = append(sudoArgs, args...)

		if needsSu {
			cmdString := "sudo -k; " + shellJoin(append([]string{"sudo"}, sudoArgs...))
			cmd = exec.Command("su", "--preserve-environment", sess.User.Username, "-c", cmdString)
			cmd.Env = childEnv
		} else {
			cmd = exec.Command("sudo", sudoArgs...)
			cmd.Env = childEnv
		}

		if sudoPassword != "" {
			stdin, perr := cmd.StdinPipe()
			if perr != nil {
				logger.Errorf("Failed to get stdin pipe: %v", perr)
				return perr
			}
			pwSuffix := "\n"
			if needsSu {
				pwSuffix = "\n" + sudoPassword + "\n"
			}
			pwBytes := []byte(sudoPassword + pwSuffix)
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
		if !sess.Privileged || sudoPassword == "" {
			cmd.Stdin = devnull
		}
		cmd.Stdout = devnull
		cmd.Stderr = devnull
		defer func() { _ = devnull.Close() }() // close parent's copy after Start
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("Failed to start bridge for session %s using %s: %v", sess.SessionID, bridgeBinary, err)
		return err
	}

	if sess.Privileged {
		logger.Infof("Started privileged bridge")
	} else {
		logger.Infof("Started bridge")
	}

	logger.Debugf(
		"bridge started: session=%s pid=%d full_bin=%q",
		sess.SessionID, cmd.Process.Pid, bridgeBinary,
	)

	processes[sess.SessionID] = &ipc.BridgeProcess{
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

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

// GetBridgeBinaryPath returns an absolute or name-only path for the bridge.
// Search order:
//  1. explicit override (if provided and executable)
//  2. next to the server executable (prod-friendly)
//  3. in development: walk up from CWD for a few levels
//  4. PATH (current user)
//  5. fallback to plain name
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
	logger.Debugf("%s not found beside server, in dev tree, or in user $PATH; "+
		"will attempt plain name (sudo may resolve via secure_path). "+
		"Consider passing --bridge-binary or installing into a well-known path.",
		binaryName)
	return binaryName
}
