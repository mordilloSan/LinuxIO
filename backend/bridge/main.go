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

// systemdListenFDs returns the number of systemd socket-activation FDs.
func systemdListenFDs() int {
	nStr := strings.TrimSpace(os.Getenv("LISTEN_FDS"))
	if nStr == "" {
		return 0
	}
	pidStr := strings.TrimSpace(os.Getenv("LISTEN_PID"))
	if pidStr == "" {
		return 0
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid != os.Getpid() {
		return 0
	}
	n, err := strconv.Atoi(nStr)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

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
	SocketPath:   bootCfg.SocketPath,
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

	// Use saved socket path from environment config
	socketPath := strings.TrimSpace(bootCfg.SocketPath)
	if socketPath == "" {
		// logger isn't initialized yet; print to stderr and exit
		fmt.Fprintln(os.Stderr, "bridge bootstrap error: empty socket path in bootstrap JSON")
		os.Exit(1)
	}

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

	logger.Infof("[bridge] boot: euid=%d uid=%d gid=%d socket=%s (environment cleared for security)",
		os.Geteuid(), Sess.User.UID, Sess.User.GID, socketPath)

	// Use saved server cert from environment config
	if pem := strings.TrimSpace(bootCfg.ServerCert); pem != "" {
		if err := web.SetRootPoolFromPEM([]byte(pem)); err != nil {
			logger.Warnf("failed to load LINUXIO_SERVER_CERT: %v", err)
		} else {
			logger.Debugf("Loaded server cert from saved environment config")
		}
	}

	_ = syscall.Umask(0o077)
	logger.Infof("[bridge] starting (uid=%d) sock=%s", os.Geteuid(), socketPath)

	listener, err := listenBridge(socketPath, Sess.User.UID)
	if err != nil {
		logger.Errorf("create socket: %v", err)
		os.Exit(1)
	}
	logger.Infof("[bridge] LISTENING on %s", socketPath)

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

		// Brief delay to allow the shutdown response to be written back
		time.Sleep(50 * time.Millisecond)

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

func listenerFromSystemd() (net.Listener, bool, error) {
	n := systemdListenFDs()
	if n <= 0 {
		return nil, false, nil
	}
	if n != 1 {
		return nil, true, fmt.Errorf("expected 1 systemd listen FD, got %d", n)
	}

	f := os.NewFile(uintptr(3), "systemd-listen")
	if f == nil {
		return nil, true, fmt.Errorf("failed to open systemd listen FD 3")
	}
	defer f.Close()

	l, err := net.FileListener(f)
	if err != nil {
		return nil, true, fmt.Errorf("systemd listen FD 3: %w", err)
	}

	return l, true, nil
}

func listenBridge(socketPath string, uid uint32) (net.Listener, error) {
	if l, ok, err := listenerFromSystemd(); ok {
		if err != nil {
			return nil, err
		}
		logger.Infof("[socket] using systemd socket activation on %s", socketPath)
		return l, nil
	}
	return createAndOwnSocket(socketPath, uid)
}

func createAndOwnSocket(socketPath string, uid uint32) (net.Listener, error) {
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

	// If running as root, chown socket to <uid>:linuxio-bridge-socket so server can connect.
	if os.Geteuid() == 0 {
		gid := resolveLinuxioGID()
		if err := os.Chown(socketPath, int(uid), gid); err != nil {
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
	grp, err := user.LookupGroup("linuxio-bridge-socket")
	if err != nil {
		return 0
	}
	gid, _ := strconv.Atoi(grp.Gid)
	return gid
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
