package main

import (
	"bufio"
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
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
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
	LogFD         int
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
	LogFD         int    `json:"log_fd,omitempty"` // FD for piped logging in dev mode
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
			LogFD:         b.LogFD,
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
	var envMode string
	var verbose bool
	var showVersion bool

	pflag.StringVar(&envMode, "env", appconfig.EnvProduction, "environment (development|production)")
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

	envMode = strings.ToLower(envMode)

	// Use saved socket path from environment config
	socketPath := strings.TrimSpace(envCfg.SocketPath)
	if socketPath == "" {
		// logger isn't initialized yet; print to stderr and exit
		fmt.Fprintln(os.Stderr, "bridge bootstrap error: empty socket path in FD3 JSON")
		os.Exit(1)
	}

	// Initialize logger ASAP
	// If we have a log FD in dev mode, redirect stdout/stderr to it for piped logging
	// (dev mode logger uses stdout for colored output)
	if envMode == appconfig.EnvDevelopment && envCfg.LogFD > 0 {
		logFile := os.NewFile(uintptr(envCfg.LogFD), "logpipe")
		if logFile != nil {
			// Redirect both stdout and stderr to the log pipe
			logFD := int(logFile.Fd())
			stdoutFD := int(os.Stdout.Fd())
			stderrFD := int(os.Stderr.Fd())

			if err := syscall.Dup2(logFD, stdoutFD); err != nil {
				fmt.Fprintf(os.Stderr, "bridge bootstrap error: redirect stdout failed: %v\n", err)
			}
			if err := syscall.Dup2(logFD, stderrFD); err != nil {
				fmt.Fprintf(os.Stderr, "bridge bootstrap error: redirect stderr failed: %v\n", err)
			}
			// Close the original FD to avoid leaking
			if envCfg.LogFD != stdoutFD && envCfg.LogFD != stderrFD {
				if err := logFile.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "bridge bootstrap error: close log fd failed: %v\n", err)
				}
			}
		}
	}

	// Initialize logger - in dev mode with pipe, stdout/stderr already point to the pipe
	// In production, C auth-helper redirects to journal or /dev/null
	logger.InitWithFile(envMode, verbose, "")

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
	config.EnsureConfigReady(Sess.User.Username)
	logger.Debugf("[bridge] config ready")

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan)

	// Register per-session terminal handlers (terminal starts lazily on first use)
	handlers.RegisterTerminalHandlers(Sess)

	// -------------------------------------------------------------------------
	// Background samplers for network
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
	fmt.Printf("linuxio-bridge %s\n", appconfig.Version)
}

// bufferedConn wraps a net.Conn with a buffered reader for protocol detection
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (bc *bufferedConn) Read(p []byte) (int, error) {
	return bc.r.Read(p)
}

// handleMainRequest processes incoming bridge requests.
// Auto-detects legacy JSON protocol vs new framed protocol.
func handleMainRequest(conn net.Conn, id string) {
	wg.Add(1)
	defer wg.Done()
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.WarnKV("bridge conn close failed", "conn_id", id, "error", cerr)
		}
	}()

	// Peek at first byte to detect protocol
	peekable := bufio.NewReader(conn)
	firstByte, err := peekable.Peek(1)
	if err != nil {
		logger.WarnKV("failed to peek connection", "conn_id", id, "error", err)
		return
	}

	// Wrap connection to include buffered data
	wrappedConn := &bufferedConn{Conn: conn, r: peekable}

	// Detect protocol type:
	// - Yamux protocol starts with version byte 0x00
	// - JSON protocol starts with '{' (0x7B)
	// - Framed protocol starts with message type (0x01, 0x02, 0x03)
	if ipc.IsYamuxConnection(firstByte[0]) {
		// Yamux multiplexed protocol
		handleYamuxSession(wrappedConn, id)
	} else if firstByte[0] == '{' {
		// Legacy JSON protocol
		handleLegacyJSONRequest(peekable, conn, id)
	} else if firstByte[0] >= 0x01 && firstByte[0] <= 0x03 {
		// New framed protocol
		handleFramedRequest(peekable, conn, id)
	} else {
		logger.WarnKV("unknown protocol", "conn_id", id, "first_byte", fmt.Sprintf("0x%02x", firstByte[0]))
	}
}

// handleLegacyJSONRequest handles the original JSON-only protocol
func handleLegacyJSONRequest(reader io.Reader, conn net.Conn, id string) {
	decoder := json.NewDecoder(reader)
	encoder := json.NewEncoder(conn)
	encoder.SetEscapeHTML(false)

	for {
		var req ipc.Request
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				logger.DebugKV("connection closed", "conn_id", id)
			} else {
				logger.WarnKV("invalid JSON from client", "conn_id", id, "error", err)
				_ = encoder.Encode(ipc.Response{Status: "error", Error: "invalid JSON"})
			}
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
			continue
		}
		handler, ok := group[req.Command]
		if !ok {
			logger.WarnKV("unknown request command", "conn_id", id, "req_type", req.Type, "command", req.Command)
			_ = encoder.Encode(ipc.Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
			continue
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
			out, err := handler(nil, req.Args)
			done <- result{out, err}
		}()

		select {
		case r := <-done:
			if r.err != nil {
				logger.ErrorKV("handler error", "conn_id", id, "req_type", req.Type, "command", req.Command, "error", r.err)
				_ = encoder.Encode(ipc.Response{Status: "error", Error: r.err.Error()})
				continue
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
					continue
				}
				raw = b
			}

			if err := encoder.Encode(ipc.Response{Status: "ok", Output: raw}); err != nil {
				logger.WarnKV("failed to send legacy response", "conn_id", id, "error", err)
				return
			}
			logger.DebugKV("bridge response sent", "conn_id", id, "req_type", req.Type, "command", req.Command, "bytes", len(raw))

		case <-bridgeClosing:
			_ = encoder.Encode(ipc.Response{
				Status: "error",
				Error:  "canceled: bridge shutting down",
			})
			return
		}
	}
}

// handleFramedRequest handles the new framed protocol (supports binary + streaming)
func handleFramedRequest(reader io.Reader, conn net.Conn, id string) {
	for {
		var req ipc.Request
		msgType, err := ipc.ReadJSONFrame(reader, &req)
		if err != nil {
			if err == io.EOF {
				logger.DebugKV("connection closed", "conn_id", id)
			} else {
				logger.WarnKV("failed to read framed request", "conn_id", id, "error", err)
				_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "invalid framed request"})
			}
			return
		}

		if msgType != ipc.MsgTypeJSON {
			logger.WarnKV("expected JSON request frame", "conn_id", id, "msg_type", fmt.Sprintf("0x%02x", msgType))
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "expected JSON request"})
			return
		}

		// Validate request
		if req.Secret != Sess.BridgeSecret {
			logger.WarnKV("invalid bridge secret", "conn_id", id)
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "invalid secret"})
			return
		}
		if req.SessionID != Sess.SessionID {
			logger.WarnKV("session mismatch", "conn_id", id)
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "session mismatch"})
			return
		}
		if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
			logger.WarnKV("invalid characters in request", "conn_id", id, "req_type", req.Type, "command", req.Command)
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "invalid characters in command/type"})
			return
		}

		logger.DebugKV("bridge request received", "conn_id", id, "req_type", req.Type, "command", req.Command, "args", req.Args)

		group, found := handlers.HandlersByType[req.Type]
		if !found || group == nil {
			logger.WarnKV("unknown request type", "conn_id", id, "req_type", req.Type)
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
			continue
		}
		handler, ok := group[req.Command]
		if !ok {
			logger.WarnKV("unknown request command", "conn_id", id, "req_type", req.Type, "command", req.Command)
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
			continue
		}

		type result struct {
			out any
			err error
		}
		done := make(chan result, 1)

		reqCtx := ipc.NewRequestContext(conn)
		go func(ctx *ipc.RequestContext) {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorKV("handler panic", "conn_id", id, "req_type", req.Type, "command", req.Command, "panic", r)
					done <- result{nil, fmt.Errorf("panic: %v", r)}
				}
			}()
			out, err := handler(ctx, req.Args)
			done <- result{out, err}
		}(reqCtx)

		select {
		case r := <-done:
			if r.err != nil {
				logger.ErrorKV("handler error", "conn_id", id, "req_type", req.Type, "command", req.Command, "error", r.err)
				_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: r.err.Error()})
				continue
			}

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
					_ = ipc.WriteResponseFrame(conn, &ipc.Response{Status: "error", Error: "marshal output failed: " + err.Error()})
					continue
				}
				raw = b
			}

			if err := ipc.WriteResponseFrame(conn, &ipc.Response{Status: "ok", Output: raw}); err != nil {
				logger.WarnKV("failed to send framed response", "conn_id", id, "error", err)
				return
			}
			logger.DebugKV("bridge response sent", "conn_id", id, "req_type", req.Type, "command", req.Command, "bytes", len(raw))

		case <-bridgeClosing:
			_ = ipc.WriteResponseFrame(conn, &ipc.Response{
				Status: "error",
				Error:  "canceled: bridge shutting down",
			})
			return
		}
	}
}

// handleYamuxSession handles a yamux multiplexed connection.
// Each stream within the session is treated as an independent request.
func handleYamuxSession(conn net.Conn, sessionID string) {
	session, err := ipc.NewYamuxServer(conn)
	if err != nil {
		logger.ErrorKV("failed to create yamux session", "session_id", sessionID, "error", err)
		return
	}
	defer session.Close()

	logger.InfoKV("yamux session started", "session_id", sessionID)

	// Track active streams for graceful shutdown
	var streamWg sync.WaitGroup

	// Accept streams until session closes or bridge shuts down
	for {
		select {
		case <-bridgeClosing:
			logger.DebugKV("yamux session closing due to bridge shutdown", "session_id", sessionID)
			return
		default:
		}

		stream, err := session.Accept()
		if err != nil {
			if session.IsClosed() {
				logger.DebugKV("yamux session closed", "session_id", sessionID)
			} else {
				logger.WarnKV("yamux accept error", "session_id", sessionID, "error", err)
			}
			break
		}

		streamID := uuid.NewString()
		streamWg.Add(1)
		wg.Add(1)

		go func(s net.Conn, id string) {
			defer streamWg.Done()
			defer wg.Done()
			defer s.Close()

			handleYamuxStream(s, sessionID, id)
		}(stream, streamID)
	}

	// Wait for all streams to complete
	streamWg.Wait()
	logger.InfoKV("yamux session ended", "session_id", sessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Supports both framed JSON protocol and binary stream protocol.
func handleYamuxStream(stream net.Conn, sessionID, streamID string) {
	id := fmt.Sprintf("%s/%s", sessionID, streamID)

	// Peek first byte to detect protocol
	peekable := bufio.NewReader(stream)
	firstByte, err := peekable.Peek(1)
	if err != nil {
		if err == io.EOF {
			logger.DebugKV("yamux stream closed", "stream_id", id)
		} else {
			logger.WarnKV("failed to peek yamux stream", "stream_id", id, "error", err)
		}
		return
	}

	// Check if this is a binary stream frame (0x80+)
	if ipc.IsStreamFrame(firstByte[0]) {
		handleBinaryStream(&bufferedConn{Conn: stream, r: peekable}, id)
		return
	}

	// Read the framed JSON request
	var req ipc.Request
	msgType, err := ipc.ReadJSONFrame(peekable, &req)
	if err != nil {
		if err == io.EOF {
			logger.DebugKV("yamux stream closed", "stream_id", id)
		} else {
			logger.WarnKV("failed to read yamux request", "stream_id", id, "error", err)
			_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "invalid request"})
		}
		return
	}

	if msgType != ipc.MsgTypeJSON {
		logger.WarnKV("expected JSON request frame", "stream_id", id, "msg_type", fmt.Sprintf("0x%02x", msgType))
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "expected JSON request"})
		return
	}

	// Validate request
	if req.Secret != Sess.BridgeSecret {
		logger.WarnKV("invalid bridge secret", "stream_id", id)
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "invalid secret"})
		return
	}
	if req.SessionID != Sess.SessionID {
		logger.WarnKV("session mismatch", "stream_id", id)
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "session mismatch"})
		return
	}
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.WarnKV("invalid characters in request", "stream_id", id, "req_type", req.Type, "command", req.Command)
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.DebugKV("yamux request received", "stream_id", id, "req_type", req.Type, "command", req.Command, "args", req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.WarnKV("unknown request type", "stream_id", id, "req_type", req.Type)
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.WarnKV("unknown request command", "stream_id", id, "req_type", req.Type, "command", req.Command)
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
		return
	}

	type result struct {
		out any
		err error
	}
	done := make(chan result, 1)

	reqCtx := ipc.NewRequestContext(stream)
	go func(ctx *ipc.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorKV("handler panic", "stream_id", id, "req_type", req.Type, "command", req.Command, "panic", r)
				done <- result{nil, fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := handler(ctx, req.Args)
		done <- result{out, err}
	}(reqCtx)

	select {
	case r := <-done:
		if r.err != nil {
			logger.ErrorKV("handler error", "stream_id", id, "req_type", req.Type, "command", req.Command, "error", r.err)
			_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: r.err.Error()})
			return
		}

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
				logger.ErrorKV("handler marshal error", "stream_id", id, "req_type", req.Type, "command", req.Command, "error", err)
				_ = ipc.WriteResponseFrame(stream, &ipc.Response{Status: "error", Error: "marshal output failed: " + err.Error()})
				return
			}
			raw = b
		}

		if err := ipc.WriteResponseFrame(stream, &ipc.Response{Status: "ok", Output: raw}); err != nil {
			logger.WarnKV("failed to send yamux response", "stream_id", id, "error", err)
			return
		}
		logger.DebugKV("yamux response sent", "stream_id", id, "req_type", req.Type, "command", req.Command, "bytes", len(raw))

	case <-bridgeClosing:
		_ = ipc.WriteResponseFrame(stream, &ipc.Response{
			Status: "error",
			Error:  "canceled: bridge shutting down",
		})
		return
	}
}

// handleBinaryStream handles binary stream protocol (terminal streaming, etc.)
func handleBinaryStream(conn net.Conn, id string) {
	// Read the first frame to determine stream type
	frame, err := ipc.ReadRelayFrame(conn)
	if err != nil {
		logger.WarnKV("failed to read stream open frame", "stream_id", id, "error", err)
		return
	}

	if frame.Opcode != ipc.OpStreamOpen {
		logger.WarnKV("expected OpStreamOpen frame", "stream_id", id, "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		return
	}

	// Parse stream type and args from payload
	streamType, args := ipc.ParseStreamOpenPayload(frame.Payload)
	logger.DebugKV("binary stream opened", "stream_id", id, "type", streamType, "args", args)

	switch streamType {
	case ipc.StreamTypeTerminal:
		// Handle terminal stream - pass the connection for bidirectional I/O
		if err := terminal.HandleTerminalStream(Sess, conn, args); err != nil {
			logger.WarnKV("terminal stream error", "stream_id", id, "error", err)
		}
	default:
		logger.WarnKV("unknown stream type", "stream_id", id, "type", streamType)
		// Send close frame
		_ = ipc.WriteRelayFrame(conn, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamClose,
			StreamID: frame.StreamID,
		})
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
