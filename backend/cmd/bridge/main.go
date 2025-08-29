package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mordilloSan/LinuxIO/cmd/bridge/cleanup"
	"github.com/mordilloSan/LinuxIO/cmd/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/cmd/server/config"
	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/spf13/pflag"
)

// Build minimal session object from env (keeps secret out of argv)
var Sess = &session.Session{
	SessionID:    os.Getenv("LINUXIO_SESSION_ID"),
	User:         session.User{ID: os.Getenv("LINUXIO_SESSION_USER"), Name: os.Getenv("LINUXIO_SESSION_USER")},
	BridgeSecret: os.Getenv("LINUXIO_BRIDGE_SECRET"),
}

// Global shutdown signal for all handlers: closed when shutdown starts.
var bridgeClosing = make(chan struct{})

// Track in-flight requests to allow bounded wait on shutdown.
var wg sync.WaitGroup

func main() {
	// Flags for mode only
	var env string
	var verbose bool
	pflag.StringVar(&env, "env", "production", "environment (development|production)")
	pflag.BoolVar(&verbose, "verbose", false, "enable verbose logs") // presence-only: --verbose
	pflag.Parse()

	env = strings.ToLower(env)
	logger.Init(env, verbose)

	if len(Sess.BridgeSecret) < 64 {
		fmt.Fprintln(os.Stderr, "Missing or invalid LINUXIO_BRIDGE_SECRET; bridge must be started by main LinuxIO process")
		os.Exit(1)
	}

	socketPath, err := ipc.SocketPathFor(Sess)
	if err != nil {
		logger.Errorf("❌ Failed to determine bridge socket path: %v", err)
		os.Exit(1)
	}
	listener, _, _, err := createAndOwnSocket(socketPath, Sess.User.ID)
	if err != nil {
		logger.Error.Fatalf("❌ %v", err)
		os.Exit(1)
	}

	if env == "production" {
		go func() {
			time.Sleep(300 * time.Millisecond)
			cleanup.KillOwnSudoParents()
		}()
	}

	// Ensure per-user config exists and is valid; logs internally.
	config.EnsureConfigReady(Sess.User.ID)

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

// CreateAndOwnSocket creates a unix socket at socketPath, ensures only the target user can access it.
func createAndOwnSocket(socketPath, username string) (net.Listener, int, int, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to lookup user %s: %w", username, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	_ = os.Remove(socketPath) // it's okay to ignore error here (socket might not exist)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to listen on socket: %w", err)
	}

	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(true) // ensures the socket path is unlinked on Close()
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		if cerr := listener.Close(); cerr != nil {
			logger.Warnf("failed to close listener after chmod failure: %v", cerr)
		}
		if rerr := os.Remove(socketPath); rerr != nil {
			logger.Warnf("failed to remove socket after chmod failure: %v", rerr)
		}
		return nil, 0, 0, fmt.Errorf("failed to chmod socket: %w", err)
	}
	if err := os.Chown(socketPath, uid, gid); err != nil {
		if cerr := listener.Close(); cerr != nil {
			logger.Warnf("failed to close listener after chown failure: %v", cerr)
		}
		if rerr := os.Remove(socketPath); rerr != nil {
			logger.Warnf("failed to remove socket after chown failure: %v", rerr)
		}
		return nil, 0, 0, fmt.Errorf("failed to chown socket: %w", err)
	}

	return listener, uid, gid, nil
}
