package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mordilloSan/go_logger/logger"
	"github.com/spf13/pflag"

	"github.com/mordilloSan/LinuxIO/backend/bridge/cleanup"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	userconfig "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

// envConfig holds all environment values we need, captured at startup
type envConfig struct {
	SessionID     string
	Username      string
	UID           string
	GID           string
	Secret        string
	Verbose       string
	SocketPath    string
	ServerBaseURL string
	ServerCert    string
}

type boot struct {
	SessionID     string `json:"session_id"`
	Username      string `json:"username"`
	UID           string `json:"uid"`
	GID           string `json:"gid"`
	Secret        string `json:"secret"`
	ServerBaseURL string `json:"server_base_url,omitempty"`
	ServerCert    string `json:"server_cert,omitempty"`
	SocketPath    string `json:"socket_path,omitempty"`
	Verbose       string `json:"verbose,omitempty"`
}

func readBootstrapFromFD3() (*boot, error) {
	f := os.NewFile(uintptr(3), "bootstrapfd")
	if f == nil {
		return nil, os.ErrNotExist
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64*1024))
	if err != nil || len(b) == 0 {
		return nil, err
	}
	var v boot
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Secret == "" || v.SessionID == "" {
		return nil, io.ErrUnexpectedEOF
	}
	return &v, nil
}

func initEnvCfg() envConfig {
	if b, err := readBootstrapFromFD3(); err == nil && b != nil {
		// prefer FD3 path
		cfg := envConfig{
			SessionID:     b.SessionID,
			Username:      b.Username,
			UID:           b.UID,
			GID:           b.GID,
			Secret:        b.Secret,
			Verbose:       b.Verbose,
			SocketPath:    b.SocketPath,
			ServerBaseURL: b.ServerBaseURL,
			ServerCert:    b.ServerCert,
		}
		return cfg
	}
	logger.Warnf("Failed to read bootstrap info from FD3")
	return envConfig{}
}

// Capture config NOW (package init time)
var envCfg = initEnvCfg()

// Build minimal session object from our saved config
var Sess = &session.Session{
	SessionID: envCfg.SessionID,
	User: session.User{
		Username: envCfg.Username,
		UID:      envCfg.UID,
		GID:      envCfg.GID,
	},
	BridgeSecret: envCfg.Secret,
	SocketPath:   envCfg.SocketPath,
}

// Global shutdown signal for all handlers: closed when shutdown starts.
var bridgeClosing = make(chan struct{})

// Track in-flight requests to allow bounded wait on shutdown.
var wg sync.WaitGroup

func main() {
	var env string
	var verbose bool
	var showVersion bool

	pflag.StringVar(&env, "env", "production", "environment (development|production)")
	pflag.BoolVar(&verbose, "verbose", false, "enable verbose logs")
	pflag.BoolVar(&showVersion, "version", false, "print version and exit")
	pflag.Parse()

	// If --verbose wasn't passed, check our saved environment config
	if !pflag.Lookup("verbose").Changed {
		if s := strings.TrimSpace(envCfg.Verbose); s != "" {
			switch strings.ToLower(s) {
			case "1", "true", "yes", "on":
				verbose = true
			}
		}
	}

	// accept BOTH: ./linuxio-bridge --version  AND  ./linuxio-bridge version
	if showVersion || (len(pflag.Args()) > 0 && (pflag.Args()[0] == "version" || pflag.Args()[0] == "-version" || pflag.Args()[0] == "-v")) {
		printBridgeVersion()
		return
	}

	env = strings.ToLower(env)

	// Use saved socket path from environment config
	socketPath := strings.TrimSpace(envCfg.SocketPath)
	if socketPath == "" {
		// logger isn't initialized yet; print to stderr and exit
		fmt.Fprintln(os.Stderr, "bridge bootstrap error: empty socket path in FD3 JSON")
		os.Exit(1)
	}

	// Initialize logger ASAP
	// In development mode, log to file since stdout/stderr are redirected by auth-helper
	var logFile string
	if env == "development" {
		logDir := "/tmp/linuxio"
		_ = os.MkdirAll(logDir, 0o755)
		logFile = filepath.Join(logDir, fmt.Sprintf("bridge-%s.log", Sess.SessionID))
	}
	logger.InitWithFile(env, verbose, logFile)

	logger.Infof("[bridge] boot: euid=%d uid=%s gid=%s socket=%s (environment cleared for security)",
		os.Geteuid(), Sess.User.UID, Sess.User.GID, socketPath)

	// Use saved server cert from environment config
	if pem := strings.TrimSpace(envCfg.ServerCert); pem != "" {
		if err := web.SetRootPoolFromPEM([]byte(pem)); err != nil {
			logger.Warnf("failed to load LINUXIO_SERVER_CERT: %v", err)
		} else {
			logger.Debugf("Loaded server cert from saved environment config")
		}
	}

	_ = syscall.Umask(0o077)
	logger.Infof("[bridge] starting (uid=%d) sock=%s", os.Geteuid(), socketPath)

	listener, err := createAndOwnSocket(socketPath, Sess.User.UID)
	if err != nil {
		logger.Errorf("create socket: %v", err)
		os.Exit(1)
	}
	logger.Infof("[bridge] LISTENING on %s", socketPath)

	// Ensure per-user config exists and is valid
	userconfig.EnsureConfigReady(Sess.User.Username)
	logger.Debugf("[bridge] userconfig ready")

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan)

	// Register per-session terminal handlers and eagerly start the main shell
	handlers.RegisterTerminalHandlers(Sess)
	if err := terminal.StartTerminal(Sess); err != nil {
		logger.Warnf("Failed to start session terminal: %v", err)
	}

	// -------------------------------------------------------------------------
	// Background samplers, GPU info
	// -------------------------------------------------------------------------
	system.StartSimpleNetInfoSampler()

	// Handle Ctrl-C / kill properly → request shutdown once
	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigc
		select {
		case ShutdownChan <- "signal: " + s.String():
		default: // already shutting down
		}
	}()

	acceptDone := make(chan struct{})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-acceptDone:
					return
				default:
					logger.WarnKV("accept failed", "error", err)
					time.Sleep(50 * time.Millisecond) // avoid tight loop during teardown
				}
				continue
			}
			id := uuid.NewString()
			go handleMainRequest(conn, id)
		}
	}()

	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-ShutdownChan

		// Signal all goroutines that shutdown started
		close(bridgeClosing)

		// Stop accepting new connections and close listener
		close(acceptDone)
		if err := listener.Close(); err != nil {
			logger.WarnKV("listener close failed", "error", err)
		}

		// Bounded wait for in-flight requests; do not block forever
		waitCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitCh)
		}()
		const grace = 5 * time.Second
		select {
		case <-waitCh:
			logger.DebugKV("in-flight handlers drained", "grace_period", grace)
		case <-time.After(grace):
			logger.WarnKV("in-flight handlers exceeded grace", "grace_period", grace)
		}

		// Cleanup artifacts regardless of handler state
		if err := cleanup.FullCleanup(reason, Sess); err != nil {
			logger.WarnKV("bridge cleanup failed", "reason", reason, "error", err)
		}
		cleanupDone <- struct{}{}
	}()

	// Wait for cleanup to complete; then exit
	<-cleanupDone
	logger.InfoKV("bridge stopped")
}

func printBridgeVersion() {
	fmt.Printf("linuxio-bridge %s\n", version.Version)
}

// handleMainRequest processes incoming bridge requests.
func handleMainRequest(conn net.Conn, id string) {
	wg.Add(1)
	defer wg.Done()
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.WarnKV("bridge conn close failed", "conn_id", id, "error", cerr)
		}
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	encoder.SetEscapeHTML(false)

	var req ipc.Request
	if err := decoder.Decode(&req); err != nil {
		if err == io.EOF {
			logger.DebugKV("connection closed without data", "conn_id", id)
		} else {
			logger.WarnKV("invalid JSON from client", "conn_id", id, "error", err)
		}
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid JSON"})
		return
	}

	if req.Secret != Sess.BridgeSecret {
		logger.WarnKV("invalid bridge secret", "conn_id", id)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid secret"})
		return
	}
	if req.SessionID != Sess.SessionID {
		logger.WarnKV("session mismatch", "conn_id", id)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "session mismatch"})
		return
	}
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.WarnKV("invalid characters in request", "conn_id", id, "req_type", req.Type, "command", req.Command)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.DebugKV("bridge request received", "conn_id", id, "req_type", req.Type, "command", req.Command, "args", req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.WarnKV("unknown request type", "conn_id", id, "req_type", req.Type)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.WarnKV("unknown request command", "conn_id", id, "req_type", req.Type, "command", req.Command)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
		return
	}

	type result struct {
		out any
		err error
	}
	done := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorKV("handler panic", "conn_id", id, "req_type", req.Type, "command", req.Command, "panic", r)
				done <- result{nil, fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := handler(req.Args)
		done <- result{out, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			logger.ErrorKV("handler error", "conn_id", id, "req_type", req.Type, "command", req.Command, "error", r.err)
			_ = encoder.Encode(ipc.Response{Status: "error", Error: r.err.Error()})
			return
		}

		// Coerce r.out into json.RawMessage
		var raw json.RawMessage
		switch v := r.out.(type) {
		case nil:
			raw = nil
		case json.RawMessage:
			raw = v
		case []byte:
			raw = json.RawMessage(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				logger.ErrorKV("handler marshal error", "conn_id", id, "req_type", req.Type, "command", req.Command, "error", err)
				_ = encoder.Encode(ipc.Response{Status: "error", Error: "marshal output failed: " + err.Error()})
				return
			}
			raw = b
		}

		_ = encoder.Encode(ipc.Response{Status: "ok", Output: raw})
		return

	case <-bridgeClosing:
		_ = encoder.Encode(ipc.Response{
			Status: "error",
			Error:  "canceled: bridge shutting down",
		})
		return
	}
}

func createAndOwnSocket(socketPath, uidStr string) (net.Listener, error) {
	logger.Debugf("[socket] unlink-if-exists %s", socketPath)
	_ = os.Remove(socketPath)

	// Sanity-check parent directory exists (fail fast with a clear error)
	parent := filepath.Dir(socketPath)
	if st, err := os.Stat(parent); err != nil {
		logger.Errorf("[socket] parent dir not accessible: %s: %v", parent, err)
		return nil, fmt.Errorf("parent dir %s not accessible: %w", parent, err)
	} else if !st.IsDir() {
		logger.Errorf("[socket] parent is not a directory: %s", parent)
		return nil, fmt.Errorf("parent path %s is not a directory", parent)
	}

	logger.Debugf("[socket] net.Listen(unix, %s)", socketPath)
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		logger.Errorf("[socket] listen FAILED on %s: %v", socketPath, err)
		return nil, fmt.Errorf("failed to listen on socket: %w", err)
	}
	logger.Debugf("[socket] listen OK: %s", socketPath)

	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(true)
	}

	if err := os.Chmod(socketPath, 0o660); err != nil {
		logger.Errorf("[socket] chmod 0660 FAILED on %s: %v", socketPath, err)
		_ = l.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("chmod: %w", err)
	}
	logger.Debugf("[socket] chmod 0660 OK: %s", socketPath)

	// If running as root, chown socket to <uid>:linuxio so server (group linuxio) can connect.
	if os.Geteuid() == 0 {
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			logger.Errorf("[socket] atoi uid FAILED (%q): %v", uidStr, err)
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("atoi uid: %w", err)
		}
		gid := resolveLinuxioGID()
		if err := os.Chown(socketPath, uid, gid); err != nil {
			logger.Errorf("[socket] chown %s to %d:%d FAILED: %v", socketPath, uid, gid, err)
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("chown: %w", err)
		}
		logger.Debugf("[socket] chown OK (uid=%d gid=%d): %s", uid, gid, socketPath)
	}

	logger.Infof("[socket] LISTENING on %s", socketPath)
	return l, nil
}

func resolveLinuxioGID() int {
	grp, err := user.LookupGroup("linuxio")
	if err != nil {
		return 0
	}
	gid, _ := strconv.Atoi(grp.Gid)
	return gid
}
