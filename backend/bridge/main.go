package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// readBootstrap reads binary bootstrap from stdin.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe.
// FAIL-FAST: If bootstrap is invalid, exit immediately with code 1.
// This ensures the auth daemon's exec-status pipe detects failure.
func readBootstrap() *ipc.Bootstrap {
	b, err := ipc.ReadBootstrap(os.Stdin)
	if err != nil {
		slog.Error("failed to read bridge bootstrap", "error", err)
		os.Exit(1)
	}

	if b.SessionID == "" {
		slog.Error("bridge bootstrap missing session_id")
		os.Exit(1)
	}

	if b.Username == "" {
		slog.Error("bridge bootstrap missing username")
		os.Exit(1)
	}

	return b
}

// Bootstrap config and session - initialized in main() after CLI checks
var bootCfg *ipc.Bootstrap
var sess *session.Session

// Global shutdown signal for all handlers: closed when shutdown starts.
var bridgeClosing = make(chan struct{})

// Track in-flight requests to allow bounded wait on shutdown.
var wg sync.WaitGroup

// Atomic counter for stream IDs (used for log tracing).
var streamCounter atomic.Uint64

func main() {
	if handledArgs := handleBridgeArgs(); handledArgs {
		return
	}

	if isDirectBridgeInvocation() {
		fmt.Println("(to be spawned by auth daemon, not for direct use)")
		return
	}

	if configureErr := logging.Configure("linuxio-bridge", false); configureErr != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", configureErr)
		os.Exit(1)
	}

	initializeBridgeSession()
	slog.Info("bridge boot",
		"uid", os.Geteuid(),
		"user", sess.User.Username,
		"session_id", sess.SessionID,
		"privileged", sess.Privileged,
		"linuxio_uid", sess.User.UID,
		"linuxio_gid", sess.User.GID,
	)

	syscall.Umask(0o077)
	slog.Info("bridge starting", "uid", os.Geteuid())

	clientConn := openClientConnection()
	slog.Info("bridge connected to inherited client fd", "fd", clientConnFD)

	// Ensure per-user config exists and is valid
	config.EnsureConfigReady(sess.User.Username)
	slog.Debug("bridge config ready", "user", sess.User.Username)

	runBridge(clientConn)
	slog.Info("bridge stopped")
}

const clientConnFD = 3

func handleBridgeArgs() bool {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			printBridgeVersion()
		default:
			printBridgeVersion()
			fmt.Println("(to be spawned by auth daemon, not for direct use)")
		}
		return true
	}
	return false
}

func isDirectBridgeInvocation() bool {
	fileInfo, err := os.Stdin.Stat()
	return err != nil || (fileInfo.Mode()&os.ModeCharDevice) != 0
}

func initializeBridgeSession() {
	bootCfg = readBootstrap()
	if bootCfg.Verbose {
		if configureErr := logging.Configure("linuxio-bridge", true); configureErr != nil {
			fmt.Fprintf(os.Stderr, "failed to reconfigure logger: %v\n", configureErr)
			os.Exit(1)
		}
	}
	sess = &session.Session{
		SessionID:  bootCfg.SessionID,
		Privileged: bootCfg.Privileged,
		User: session.User{
			Username: bootCfg.Username,
			UID:      bootCfg.UID,
			GID:      bootCfg.GID,
		},
	}
}

func openClientConnection() net.Conn {
	clientFile := os.NewFile(uintptr(clientConnFD), "client-conn")
	if clientFile == nil {
		slog.Error("failed to open client connection", "fd", clientConnFD)
		os.Exit(1)
	}
	clientConn, err := net.FileConn(clientFile)
	clientFile.Close()
	if err != nil {
		slog.Error("failed to create client connection", "fd", clientConnFD, "error", err)
		os.Exit(1)
	}
	return clientConn
}

func runBridge(clientConn net.Conn) {
	shutdownCh := make(chan string, 1)
	handlers.RegisterAllHandlers(sess)
	startBridgeSignalHandler(shutdownCh)

	closeClientConn := newClientConnCloser(clientConn)
	startMainRequestLoop(clientConn, closeClientConn, shutdownCh)
	<-startBridgeCleanup(shutdownCh, closeClientConn)
}

func startBridgeSignalHandler(shutdownCh chan<- string) {
	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigc
		select {
		case shutdownCh <- "signal: " + s.String():
		default:
		}
	}()
}

func newClientConnCloser(clientConn net.Conn) func() {
	var closeOnce sync.Once
	return func() {
		closeOnce.Do(func() {
			if err := clientConn.Close(); err != nil {
				slog.Debug("client conn close", "error", err)
			}
		})
	}
}

func startMainRequestLoop(clientConn net.Conn, closeClientConn func(), shutdownCh chan<- string) {
	wg.Go(func() {
		handleMainRequest(clientConn, closeClientConn)
		select {
		case shutdownCh <- "client disconnected":
		default:
		}
	})
}

func startBridgeCleanup(shutdownCh <-chan string, closeClientConn func()) <-chan struct{} {
	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-shutdownCh
		time.Sleep(50 * time.Millisecond)
		close(bridgeClosing)
		closeClientConn()
		waitForInflightHandlers()
		slog.Debug("shutdown initiated", "reason", reason, "user", sess.User.Username, "session_id", sess.SessionID)
		cleanupDone <- struct{}{}
	}()
	return cleanupDone
}

func waitForInflightHandlers() {
	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	const grace = 5 * time.Second
	select {
	case <-waitCh:
		slog.Debug("in-flight handlers drained", "grace_period", grace)
	case <-time.After(grace):
		slog.Warn("in-flight handlers exceeded grace", "grace_period", grace)
	}
}

func printBridgeVersion() {
	fmt.Printf("LinuxIO Bridge %s\n", version.Version)
}

// handleMainRequest sets up a yamux session for the client connection.
func handleMainRequest(conn net.Conn, closeConn func()) {
	defer closeConn()
	handleYamuxSession(conn)
}

// handleYamuxSession handles a yamux multiplexed connection.
// Each stream within the session is treated as an independent request.
func handleYamuxSession(conn net.Conn) {
	ymuxSession, err := ipc.NewYamuxServer(conn)
	if err != nil {
		slog.Error("failed to create yamux session", "session_id", sess.SessionID, "error", err)
		return
	}
	defer ymuxSession.Close()
	slog.Info("yamux session started", "session_id", sess.SessionID)

	// Track active streams for graceful shutdown
	var streamWg sync.WaitGroup

	// Accept streams until session closes or bridge shuts down.
	// The loop exits when ymuxSession.Accept() returns an error
	// (e.g., the session is closed by the shutdown goroutine).
	for {
		stream, err := ymuxSession.Accept()
		if err != nil {
			if ymuxSession.IsClosed() {
				slog.Debug("yamux session closed", "session_id", sess.SessionID)
			} else {
				slog.Warn("yamux accept error", "session_id", sess.SessionID, "error", err)
			}
			break
		}

		streamID := strconv.FormatUint(streamCounter.Add(1), 10)
		s := stream
		sid := streamID
		streamWg.Go(func() {
			defer s.Close()

			handleYamuxStream(sess, s, sid)
		})
	}

	// Wait for all streams to complete
	streamWg.Wait()
	slog.Info("yamux session ended", "session_id", sess.SessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Reads the OpStreamOpen frame, looks up the registered handler, and executes it.
func handleYamuxStream(sess *session.Session, stream net.Conn, streamID string) {
	// Read the first frame to determine stream type
	frame, err := ipc.ReadRelayFrame(stream)
	if err != nil {
		slog.Warn("failed to read stream open frame", "session_id", sess.SessionID, "stream_id", streamID, "error", err)
		return
	}

	if frame.Opcode != ipc.OpStreamOpen {
		slog.Warn("expected OpStreamOpen frame", "session_id", sess.SessionID, "stream_id", streamID, "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		return
	}

	// Parse stream type and args from payload
	streamType, args := ipc.ParseStreamOpenPayload(frame.Payload)
	startedAt := time.Now()
	if streamType != "bridge" {
		slog.Debug("stream started: "+streamType,
			"session_id", sess.SessionID,
			"stream_id", streamID,
			"stream_type", streamType,
			"arg_count", len(args))
	}

	// Look up registered stream handler
	handler, found := handlers.GetStreamHandler(streamType)
	if !found {
		slog.Warn("unknown stream type", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType)
		// Send close frame
		if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamClose,
			StreamID: frame.StreamID,
		}); err != nil {
			slog.Debug("failed to write close frame for unknown stream type", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType, "error", err)
		}
		return
	}

	// Execute stream handler
	if err := handler(sess, stream, args); err != nil {
		if streamType == "bridge" {
			return
		}
		if errors.Is(err, ipc.ErrAborted) {
			slog.Debug("stream aborted: "+streamType,
				"session_id", sess.SessionID,
				"stream_id", streamID,
				"stream_type", streamType,
				"duration", time.Since(startedAt))
			return
		}
		slog.Warn("stream failed: "+streamType,
			"session_id", sess.SessionID,
			"stream_id", streamID,
			"stream_type", streamType,
			"duration", time.Since(startedAt),
			"error", err)
		return
	}

	if streamType != "bridge" {
		slog.Debug("stream completed: "+streamType,
			"session_id", sess.SessionID,
			"stream_id", streamID,
			"stream_type", streamType,
			"duration", time.Since(startedAt))
	}
}
