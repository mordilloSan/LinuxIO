package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
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
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/protocol"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

// Bootstrap is the configuration passed from auth daemon via stdin
type Bootstrap = protocol.Bootstrap

// readBootstrap reads bootstrap JSON from stdin.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe.
func readBootstrap() *Bootstrap {
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
	if err != nil || len(b) == 0 {
		logger.Warnf("Failed to read bootstrap from stdin: %v", err)
		return &Bootstrap{}
	}

	var v Bootstrap
	if err := json.Unmarshal(b, &v); err != nil {
		logger.Warnf("Failed to unmarshal bootstrap JSON: %v", err)
		return &Bootstrap{}
	}

	if v.Secret == "" || v.SessionID == "" {
		logger.Warnf("Bootstrap missing required fields (secret or session_id)")
		return &Bootstrap{}
	}

	return &v
}

// Capture bootstrap config at package init time
var bootCfg = readBootstrap()

// Build minimal session object from our saved config
var Sess = &session.Session{
	SessionID: bootCfg.SessionID,
	User: session.User{
		Username: bootCfg.Username,
		UID:      bootCfg.UID,
		GID:      bootCfg.GID,
	},
	BridgeSecret: bootCfg.Secret,
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
		verbose = bootCfg.Verbose
	}

	// accept BOTH: ./linuxio-bridge --version  AND  ./linuxio-bridge version
	if showVersion || (len(pflag.Args()) > 0 && (pflag.Args()[0] == "version" || pflag.Args()[0] == "-version" || pflag.Args()[0] == "-v")) {
		printBridgeVersion()
		return
	}

	envMode = strings.ToLower(envMode)

	// Initialize logger ASAP
	// If we have a log FD in dev mode, redirect stdout/stderr to it for piped logging
	// (dev mode logger uses stdout for colored output)
	if envMode == appconfig.EnvDevelopment && bootCfg.LogFD > 0 {
		logFile := os.NewFile(uintptr(bootCfg.LogFD), "logpipe")
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
			if bootCfg.LogFD != stdoutFD && bootCfg.LogFD != stderrFD {
				if err := logFile.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "bridge bootstrap error: close log fd failed: %v\n", err)
				}
			}
		}
	}

	// Initialize logger - in dev mode with pipe, stdout/stderr already point to the pipe
	// In production, C auth-helper redirects to journal or /dev/null
	logger.InitWithFile(envMode, verbose, "")

	logger.Infof("[bridge] boot: euid=%d uid=%d gid=%d (environment cleared for security)",
		os.Geteuid(), Sess.User.UID, Sess.User.GID)

	// Use saved server cert from environment config
	if pem := strings.TrimSpace(bootCfg.ServerCert); pem != "" {
		if err := web.SetRootPoolFromPEM([]byte(pem)); err != nil {
			logger.Warnf("failed to load LINUXIO_SERVER_CERT: %v", err)
		} else {
			logger.Debugf("Loaded server cert from saved environment config")
		}
	}

	_ = syscall.Umask(0o077)
	logger.Infof("[bridge] starting (uid=%d)", os.Geteuid())

	// Get the client connection from FD 3 (inherited from auth daemon)
	const clientConnFD = 3
	clientFile := os.NewFile(uintptr(clientConnFD), "client-conn")
	if clientFile == nil {
		logger.Errorf("failed to open client connection FD %d", clientConnFD)
		os.Exit(1)
	}
	clientConn, err := net.FileConn(clientFile)
	clientFile.Close() // Close our reference; the net.Conn now owns the FD
	if err != nil {
		logger.Errorf("failed to create net.Conn from FD %d: %v", clientConnFD, err)
		os.Exit(1)
	}
	logger.Infof("[bridge] connected via inherited FD %d", clientConnFD)

	// Ensure per-user config exists and is valid
	config.EnsureConfigReady(Sess.User.Username)
	logger.Debugf("[bridge] config ready")

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan, Sess)

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

	// Handle the single client connection (no accept loop needed)
	connDone := make(chan struct{})
	go func() {
		id := uuid.NewString()
		handleMainRequest(clientConn, id)
		// Connection closed - trigger shutdown
		select {
		case ShutdownChan <- "client disconnected":
		default: // already shutting down
		}
		close(connDone)
	}()

	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-ShutdownChan

		// Brief delay to allow the shutdown response to be written back
		time.Sleep(50 * time.Millisecond)

		// Signal all goroutines that shutdown started
		close(bridgeClosing)

		// Close the client connection to unblock any reads
		if err := clientConn.Close(); err != nil {
			logger.DebugKV("client conn close", "error", err)
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
			if strings.Contains(cerr.Error(), "use of closed") {
				logger.DebugKV("bridge conn already closed", "conn_id", id)
			} else {
				logger.WarnKV("bridge conn close failed", "conn_id", id, "error", cerr)
			}
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

	// Only yamux protocol is supported (starts with version byte 0x00)
	if ipc.IsYamuxConnection(firstByte[0]) {
		handleYamuxSession(wrappedConn, id)
	} else {
		logger.WarnKV("unknown protocol (expected yamux)", "conn_id", id, "first_byte", fmt.Sprintf("0x%02x", firstByte[0]))
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
			goto waitForStreams
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

waitForStreams:
	// Wait for all streams to complete
	streamWg.Wait()
	logger.InfoKV("yamux session ended", "session_id", sessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Supports both JSON-encoded RPC and binary stream protocol.
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

	// Read the JSON-encoded request
	req, err := ipc.ReadRequest(peekable)
	if err != nil {
		if err == io.EOF {
			logger.DebugKV("yamux stream closed", "stream_id", id)
		} else {
			logger.WarnKV("failed to read yamux request", "stream_id", id, "error", err)
			_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: "invalid request"})
		}
		return
	}

	// Validate request
	if req.Secret != Sess.BridgeSecret {
		logger.WarnKV("invalid bridge secret", "stream_id", id)
		_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: "invalid secret"})
		return
	}
	if req.SessionID != Sess.SessionID {
		logger.WarnKV("session mismatch", "stream_id", id)
		_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: "session mismatch"})
		return
	}
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.WarnKV("invalid characters in request", "stream_id", id, "req_type", req.Type, "command", req.Command)
		_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.DebugKV("yamux request received", "stream_id", id, "req_type", req.Type, "command", req.Command, "args", req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.WarnKV("unknown request type", "stream_id", id, "req_type", req.Type)
		_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.WarnKV("unknown request command", "stream_id", id, "req_type", req.Type, "command", req.Command)
		_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
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
				logger.ErrorKV("handler panic", "stream_id", id, "req_type", req.Type, "command", req.Command, "panic", r)
				done <- result{nil, fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := handler(req.Args)
		done <- result{out, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			logger.ErrorKV("handler error", "stream_id", id, "req_type", req.Type, "command", req.Command, "error", r.err)
			_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: r.err.Error()})
			return
		}

		// Marshal handler output to JSON once (stored as RawMessage, no double-encoding)
		var output json.RawMessage
		if r.out != nil {
			var err error
			output, err = json.Marshal(r.out)
			if err != nil {
				logger.ErrorKV("failed to marshal handler output", "stream_id", id, "error", err)
				_ = ipc.WriteResponse(stream, &ipc.Response{Status: "error", Error: "marshal error"})
				return
			}
		}

		if err := ipc.WriteResponse(stream, &ipc.Response{Status: "ok", Output: output}); err != nil {
			logger.WarnKV("failed to send yamux response", "stream_id", id, "error", err)
			return
		}
		logger.DebugKV("yamux response sent", "stream_id", id, "req_type", req.Type, "command", req.Command)

	case <-bridgeClosing:
		_ = ipc.WriteResponse(stream, &ipc.Response{
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
	case "terminal":
		// Handle terminal stream - pass the connection for bidirectional I/O
		if err := terminal.HandleTerminalStream(Sess, conn, args); err != nil {
			logger.WarnKV("terminal stream error", "stream_id", id, "error", err)
		}
	case "container":
		// Handle container terminal stream - docker exec
		if err := terminal.HandleContainerTerminalStream(Sess, conn, args); err != nil {
			logger.WarnKV("container terminal stream error", "stream_id", id, "error", err)
		}
	case filebrowser.StreamTypeFBDownload, filebrowser.StreamTypeFBUpload, filebrowser.StreamTypeFBArchive, filebrowser.StreamTypeFBCompress, filebrowser.StreamTypeFBExtract:
		// Handle filebrowser stream - download, upload, archive, compress, extract operations
		if err := filebrowser.HandleFilebrowserStream(Sess, conn, streamType, args); err != nil {
			logger.WarnKV("filebrowser stream error", "stream_id", id, "type", streamType, "error", err)
		}
	case dbus.StreamTypePkgUpdate:
		// Handle package update stream - updates packages with real-time D-Bus progress
		if err := dbus.HandlePackageUpdateStream(conn, args); err != nil {
			logger.WarnKV("package update stream error", "stream_id", id, "error", err)
		}
	case "api":
		// Handle API stream - JSON API calls over yamux
		if err := HandleAPIStream(conn, args); err != nil {
			logger.WarnKV("api stream error", "stream_id", id, "error", err)
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

// HandleAPIStream handles a yamux stream for JSON API calls.
// This allows API calls to bypass HTTP and use the same stream infrastructure
// as terminal and file transfers.
//
// args format: [type, command, ...handlerArgs]
// - type: handler group (e.g., "system", "docker", "filebrowser")
// - command: handler command (e.g., "get_cpu_info", "list_containers")
// - handlerArgs: remaining args passed to the handler
//
// Response: OpStreamResult with JSON data, then OpStreamClose
func HandleAPIStream(stream net.Conn, args []string) error {
	logger.Debugf("[APIStream] Starting args=%v", args)

	// Validate args
	if len(args) < 2 {
		errMsg := "api stream requires at least [type, command]"
		logger.Warnf("[APIStream] %s, got: %v", errMsg, args)
		_ = ipc.WriteResultError(stream, 0, errMsg, 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	handlerType := args[0]
	command := args[1]
	handlerArgs := args[2:]

	// Look up handler group
	group, found := handlers.HandlersByType[handlerType]
	if !found {
		errMsg := fmt.Sprintf("unknown handler type: %s", handlerType)
		logger.Warnf("[APIStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Look up handler
	handler, ok := group[command]
	if !ok {
		errMsg := fmt.Sprintf("unknown command: %s/%s", handlerType, command)
		logger.Warnf("[APIStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Execute handler
	result, err := handler(handlerArgs)
	if err != nil {
		logger.Warnf("[APIStream] Handler error %s/%s: %v", handlerType, command, err)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// Marshal result
	var data json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			logger.Warnf("[APIStream] Marshal error: %v", err)
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("marshal error: %v", err), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return err
		}
		data = b
	}

	// Send result
	logger.Debugf("[APIStream] Success %s/%s, data len=%d", handlerType, command, len(data))
	_ = ipc.WriteResultFrame(stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
	_ = ipc.WriteStreamClose(stream, 0)

	return nil
}
