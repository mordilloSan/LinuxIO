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
	"github.com/spf13/pflag"

	"github.com/mordilloSan/LinuxIO/bridge/cleanup"
	"github.com/mordilloSan/LinuxIO/bridge/filebrowser"
	"github.com/mordilloSan/LinuxIO/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/bridge/terminal"
	"github.com/mordilloSan/LinuxIO/bridge/userconfig"
	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/common/version"
	"github.com/mordilloSan/LinuxIO/server/web"
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
	var verbose, showVersion bool

	pflag.StringVar(&env, "env", "production", "environment (development|production)")
	pflag.BoolVar(&verbose, "verbose", false, "enable verbose logs")
	pflag.BoolVar(&showVersion, "version", false, "print version and exit")
	pflag.Parse()

	if showVersion || (len(pflag.Args()) > 0 && (pflag.Args()[0] == "version" || pflag.Args()[0] == "-version" || pflag.Args()[0] == "-v")) {
		printBridgeVersion()
		return
	}

	// optional fallback if someone runs bridge manually without flags
	if env == "" {
		if v := strings.TrimSpace(os.Getenv("LINUXIO_ENV")); v != "" {
			env = v
		} else {
			env = "production"
		}
	}
	env = strings.ToLower(strings.TrimSpace(env))

	effectiveVerbose := verbose
	if env == "production" {
		effectiveVerbose = true
	}

	logger.Init(env, effectiveVerbose)
	logger.Infof("Bridge starting in %s mode (verbose=%v)", env, effectiveVerbose)

	if len(Sess.BridgeSecret) < 64 {
		fmt.Fprintln(os.Stderr, "Bridge must be started by main LinuxIO process")
		os.Exit(1)
	}

	if Sess.User.UID == "" {
		fmt.Fprintln(os.Stderr, "Bridge must be started by main LinuxIO process")
		os.Exit(1)
	}

	if pem := strings.TrimSpace(os.Getenv("LINUXIO_SERVER_CERT")); pem != "" {
		if err := web.SetRootPoolFromPEM([]byte(pem)); err != nil {
			logger.Warnf("failed to load LINUXIO_SERVER_CERT: %v", err)
		} else {
			logger.Debugf("Loaded server cert from LINUXIO_SERVER_CERT")
		}
	}

	_ = syscall.Umask(0o077)

	socketPath := Sess.SocketPath()

	listener, err := createAndOwnSocket(socketPath, Sess.User.UID)
	if err != nil {
		logger.Error.Fatalf("create socket: %v", err)
	}

	// Ensure per-user config exists and is valid
	userconfig.EnsureConfigReady(Sess.User.Username)

	// Kick off Navigator defaults application in the background.
	if base := os.Getenv("LINUXIO_SERVER_BASE_URL"); base != "" {
		go func(baseURL string) {
			// Try a few times with small backoff to ride out races.
			var err error
			for i := 0; i < 8; i++ {
				err = filebrowser.ApplyNavigatorDefaults(baseURL, Sess)
				if err == nil {
					return
				}
				select {
				case <-bridgeClosing:
					return
				case <-time.After(time.Duration(250+100*i) * time.Millisecond):
				}
			}
			logger.Debugf("Gave up for user=%s: %v", Sess.User.Username, err)
		}(base)
	} else {
		logger.Debugf("No LINUXIO_SERVER_BASE_URL; skipping")
	}

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan)
	// Register per-session terminal handlers and eagerly start the main shell
	handlers.RegisterTerminalHandlers(Sess)
	if err := terminal.StartTerminal(Sess); err != nil {
		logger.Warnf("Failed to start session terminal: %v", err)
	}

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
					logger.Warnf(" Accept failed: %v", err)
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
			logger.Warnf(" [%s] invalid JSON from client: %v", id, err)
		}
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid JSON"})
		return
	}

	if req.Secret != Sess.BridgeSecret {
		logger.Warnf(" [%s] invalid secret", id)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid secret"})
		return
	}
	if req.SessionID != Sess.SessionID {
		logger.Warnf(" [%s] session mismatch: got %q want %q", id, req.SessionID, Sess.SessionID)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "session mismatch"})
		return
	}
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.Warnf(" [%s] Invalid characters in type/command: type=%q, command=%q", id, req.Type, req.Command)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.Debugf("Received request: type=%s, command=%s, args=%v", req.Type, req.Command, req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.Warnf(" Unknown type: %s", req.Type)
		_ = encoder.Encode(ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.Warnf(" Unknown command for type %s: %s", req.Type, req.Command)
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
			logger.Errorf(" %s %s failed: %v", req.Type, req.Command, r.err)
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
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket: %w", err)
	}

	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(true)
	}

	// perms first, then ownership
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = l.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("chmod: %w", err)
	}

	if os.Geteuid() == 0 {
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("atoi uid: %w", err)
		}
		gid := resolveLinuxioGID() // or parse gidStr if you want to use the passed-in value
		if err := os.Chown(socketPath, uid, gid); err != nil {
			_ = l.Close()
			_ = os.Remove(socketPath)
			return nil, fmt.Errorf("chown: %w", err)
		}
	}
	return l, nil
}

func resolveLinuxioGID() int {
	grp, err := user.LookupGroup("linuxio")
	if err != nil {
		return 0
	} // fallback to root group if missing
	gid, _ := strconv.Atoi(grp.Gid)
	return gid
}
