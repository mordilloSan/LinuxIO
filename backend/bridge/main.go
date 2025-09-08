package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/pflag"

	"github.com/mordilloSan/LinuxIO/bridge/cleanup"
	"github.com/mordilloSan/LinuxIO/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/config"
	"github.com/mordilloSan/LinuxIO/version"
)

// Build minimal session object from env (keeps secret out of argv)
var Sess = &session.Session{
	SessionID:    os.Getenv("LINUXIO_SESSION_ID"),
	User:         session.User{Username: os.Getenv("LINUXIO_SESSION_USER"), UID: os.Getenv("LINUXIO_SESSION_UID"), GID: os.Getenv("LINUXIO_SESSION_GID")},
	BridgeSecret: os.Getenv("LINUXIO_BRIDGE_SECRET"),
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

	// accept BOTH: ./linuxio-bridge --version  AND  ./linuxio-bridge version
	if showVersion || (len(pflag.Args()) > 0 && (pflag.Args()[0] == "version" || pflag.Args()[0] == "-version" || pflag.Args()[0] == "-v")) {
		printBridgeVersion()
		return
	}

	env = strings.ToLower(env)
	logger.Init(env, verbose)

	if len(Sess.BridgeSecret) < 64 {
		fmt.Fprintln(os.Stderr, "Bridge must be started by main LinuxIO process")
		os.Exit(1)
	}

	if Sess.User.UID == "" {
		fmt.Fprintln(os.Stderr, "Bridge must be started by main LinuxIO process")
		os.Exit(1)
	}

	_ = syscall.Umask(0o077)

	if err := prepareRuntimeDir(Sess); err != nil {
		logger.Error.Fatalf("prepare runtime dir: %v", err)
	}
	socketPath := Sess.SocketPath()

	listener, err := createAndOwnSocket(socketPath, Sess.User.UID, Sess.User.GID)
	if err != nil {
		logger.Error.Fatalf("create socket: %v", err)
	}

	if env == "production" {
		go func() {
			time.Sleep(300 * time.Millisecond)
			cleanup.KillOwnSudoParents()
		}()
	}

	// Ensure per-user config exists and is valid; logs internally.
	config.EnsureConfigReady(Sess.User.Username)

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan)

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
					logger.Warnf("⚠️ Accept failed: %v", err)
					time.Sleep(50 * time.Millisecond) // avoid tight loop during teardown
				}
				continue
			}
			id := uuid.NewString()
			logger.Debugf("MAIN: spawning handler %s", id)
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
			logger.Warnf("failed to close listener: %v", err)
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
			logger.Debugf("All in-flight requests finished before grace period.")
		case <-time.After(grace):
			logger.Warnf("⏳ In-flight handlers still running after %s; proceeding with cleanup.", grace)
		}

		// Cleanup artifacts regardless of handler state
		if err := cleanup.FullCleanup(reason, Sess); err != nil {
			logger.Warnf("FullCleanup failed (reason=%q): %v", reason, err)
		}
		cleanupDone <- struct{}{}
	}()

	// Wait for cleanup to complete; then exit
	<-cleanupDone
	logger.Infof("Bridge stopped.")
}

func printBridgeVersion() {
	fmt.Printf("linuxio-bridge %s (commit %s, sha256 %s)\n",
		version.Version, version.CommitSHA, version.SelfSHA256())
}

// handleMainRequest processes incoming bridge requests.
func handleMainRequest(conn net.Conn, id string) {
	wg.Add(1)
	defer wg.Done()

	logger.Debugf("HANDLECONNECTION: [%s] called!", id)
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close connection [%s]: %v", id, cerr)
		}
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	encoder.SetEscapeHTML(false)

	var req ipc.Request
	if err := decoder.Decode(&req); err != nil {
		if err == io.EOF {
			logger.Debugf("🔁 [%s] connection closed without data", id)
		} else {
			logger.Warnf("❌ [%s] invalid JSON from client: %v", id, err)
		}
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid JSON"})
		return
	}

	if req.Secret != Sess.BridgeSecret {
		logger.Warnf("❌ [%s] invalid secret", id)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid secret"})
		return
	}
	if req.SessionID != Sess.SessionID {
		logger.Warnf("❌ [%s] session mismatch: got %q want %q", id, req.SessionID, Sess.SessionID)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "session mismatch"})
		return
	}
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.Warnf("❌ [%s] Invalid characters in type/command: type=%q, command=%q", id, req.Type, req.Command)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.Debugf("Received request: type=%s, command=%s, args=%v", req.Type, req.Command, req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.Warnf("❌ Unknown type: %s", req.Type)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.Warnf("❌ Unknown command for type %s: %s", req.Type, req.Command)
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
				logger.Errorf("🔥 Panic in %s command handler: %v", req.Type, r)
				done <- result{nil, fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := handler(req.Args)
		done <- result{out, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			logger.Errorf("❌ %s %s failed: %v", req.Type, req.Command, r.err)
			_ = encoder.Encode(ipc.Response{Status: "error", Error: r.err.Error()})
			return
		}

		// Prefer zero-copy paths when the handler already returns JSON.
		var raw json.RawMessage
		switch v := r.out.(type) {
		case nil:
			// leave raw nil → {"status":"ok"}
		case json.RawMessage:
			raw = v
		case []byte:
			// Assume handler returned JSON bytes; if not, caller bug.
			raw = json.RawMessage(v)
		default:
			b, marshalErr := json.Marshal(v)
			if marshalErr != nil {
				_ = encoder.Encode(ipc.Response{Status: "error", Error: "failed to marshal handler output"})
				return
			}
			raw = json.RawMessage(b)
		}

		logger.Debugf("Responding to [%s]: status=ok, output-len=%d", id, len(raw))
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

// CreateAndOwnSocket creates a unix socket at socketPath and sets 0600.
func createAndOwnSocket(socketPath, uidStr, gidStr string) (net.Listener, error) {
	if uidStr == "" {
		return nil, fmt.Errorf("empty uid")
	}

	// Remove any stale socket first (idempotent).
	_ = os.Remove(socketPath)

	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return nil, fmt.Errorf("atoi uid: %w", err)
	}

	// Bind the Unix socket.
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Unlink the path when the listener is closed.
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(true)
	}

	// Lock down permissions.
	if permErr := os.Chmod(socketPath, 0o600); permErr != nil {
		_ = l.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("chmod socket: %w", permErr)
	}

	var gid int
	if os.Geteuid() == 0 {
		// Root may change ownership; require a gidStr.
		if gidStr == "" {
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("empty gid (required when running as root)")
		}
		gid, err = strconv.Atoi(gidStr)
		if err != nil {
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("atoi gid: %w", err)
		}
		if err := os.Chown(socketPath, uid, gid); err != nil {
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("chown socket: %w", err)
		}
	}

	return l, nil
}

func prepareRuntimeDir(sess *session.Session) error {
	dir := sess.RuntimeDir()
	tmpBase := filepath.Join(os.TempDir(), "linuxio-run")

	// If using /tmp fallback, ensure parent is 0755 so user can traverse.
	if strings.HasPrefix(dir, tmpBase+string(os.PathSeparator)) {
		if err := os.MkdirAll(tmpBase, 0o755); err != nil {
			return fmt.Errorf("ensure tmp base %q: %w", tmpBase, err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure runtime dir %q: %w", dir, err)
	}
	// If running as root, chown the dir to the session user.
	if os.Geteuid() == 0 && sess.User.UID != "" && sess.User.GID != "" {
		uid, err := strconv.Atoi(sess.User.UID)
		if err != nil {
			return fmt.Errorf("atoi uid: %w", err)
		}
		gid, err := strconv.Atoi(sess.User.GID)
		if err != nil {
			return fmt.Errorf("atoi gid: %w", err)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("chown %q: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod %q: %w", dir, err)
		}
	}
	return nil
}
