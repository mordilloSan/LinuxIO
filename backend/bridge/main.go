package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// readBootstrap reads binary bootstrap from stdin.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe.
// FAIL-FAST: If bootstrap is invalid, exit immediately with code 1.
// This ensures the auth daemon's exec-status pipe detects failure.
func readBootstrap() *ipc.Bootstrap {
	b, err := ipc.ReadBootstrap(os.Stdin)
	if err != nil {
		// Write to stderr because logger is not yet initiated (systemd captures to journal)
		fmt.Fprintf(os.Stderr, "bridge bootstrap error: failed to read: %v\n", err)
		os.Exit(1)
	}

	if b.SessionID == "" {
		fmt.Fprintf(os.Stderr, "bridge bootstrap error: missing required session_id\n")
		os.Exit(1)
	}

	if b.Username == "" {
		fmt.Fprintf(os.Stderr, "bridge bootstrap error: missing required username\n")
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
	// Handle CLI arguments first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			printBridgeVersion()
			return
		default:
			// Any other argument - show usage
			printBridgeVersion()
			fmt.Println("(to be spawned by auth daemon, not for direct use)")
			return
		}
	}

	// If stdin is a terminal, user ran this directly - show version and exit
	fileInfo, err := os.Stdin.Stat()
	if err != nil || (fileInfo.Mode()&os.ModeCharDevice) != 0 {
		fmt.Println("(to be spawned by auth daemon, not for direct use)")
		return
	}

	// Read bootstrap from stdin (auth daemon pipes this)
	bootCfg = readBootstrap()
	sess = &session.Session{
		SessionID:  bootCfg.SessionID,
		Privileged: bootCfg.Privileged,
		User: session.User{
			Username: bootCfg.Username,
			UID:      bootCfg.UID,
			GID:      bootCfg.GID,
		},
	}

	// Initialize logger (stdout/stderr → systemd journal)
	// Configure log levels based on verbose flag
	var levels []logger.Level
	verbose := bootCfg.Verbose
	if verbose {
		levels = logger.AllLevels() // Includes DEBUG
	} else {
		levels = []logger.Level{logger.InfoLevel, logger.WarnLevel, logger.ErrorLevel}
	}

	logger.Init(logger.Config{
		Levels:           levels,
		IncludeCallerTag: true,
	})

	logger.Infof("[bridge] boot: euid=%d uid=%d gid=%d (environment cleared for security)",
		os.Geteuid(), sess.User.UID, sess.User.GID)

	syscall.Umask(0o077)
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
	config.EnsureConfigReady(sess.User.Username)
	logger.Debugf("[bridge] config ready")

	// Ensure the shared linuxio-docker network exists (fails silently if Docker unavailable)
	docker.EnsureLinuxIONetwork()

	ShutdownChan := make(chan string, 1)
	handlers.RegisterAllHandlers(ShutdownChan, sess)

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

	// Guard clientConn.Close() so it is safe to call from multiple goroutines.
	var closeOnce sync.Once
	closeClientConn := func() {
		closeOnce.Do(func() {
			if err := clientConn.Close(); err != nil {
				logger.DebugKV("client conn close", "error", err)
			}
		})
	}

	// Handle the single client connection (no accept loop needed)
	connDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		handleMainRequest(clientConn, closeClientConn)
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
		closeClientConn()

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

		logger.Debugf("Shutdown initiated: %s (user=%s, session=%s)",
			reason, sess.User.Username, sess.SessionID)

		cleanupDone <- struct{}{}
	}()

	// Wait for cleanup to complete; then exit
	<-cleanupDone
	logger.InfoKV("bridge stopped")
}

func printBridgeVersion() {
	fmt.Printf("LinuxIO Bridge %s\n", appconfig.Version)
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
		logger.ErrorKV("failed to create yamux session", "session_id", sess.SessionID, "error", err)
		return
	}
	defer ymuxSession.Close()

	logger.InfoKV("yamux session started", "session_id", sess.SessionID)

	// Track active streams for graceful shutdown
	var streamWg sync.WaitGroup

	// Sync Watchtower on first incoming request (after capabilities checks have run).
	// SyncWatchtowerStack involves multiple docker API calls and takes longer than the
	// initial capabilities check, so starting it here ensures its log appears after
	// the capabilities logs rather than during early bridge startup.
	var watchtowerOnce sync.Once

	// Accept streams until session closes or bridge shuts down.
	// The loop exits when ymuxSession.Accept() returns an error
	// (e.g., the session is closed by the shutdown goroutine).
	for {
		stream, err := ymuxSession.Accept()
		if err != nil {
			if ymuxSession.IsClosed() {
				logger.DebugKV("yamux session closed", "session_id", sess.SessionID)
			} else {
				logger.WarnKV("yamux accept error", "session_id", sess.SessionID, "error", err)
			}
			break
		}

		watchtowerOnce.Do(func() { go docker.SyncWatchtowerStack(sess.User.Username) })

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
	logger.InfoKV("yamux session ended", "session_id", sess.SessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Reads the OpStreamOpen frame, looks up the registered handler, and executes it.
func handleYamuxStream(sess *session.Session, stream net.Conn, streamID string) {
	// Read the first frame to determine stream type
	frame, err := ipc.ReadRelayFrame(stream)
	if err != nil {
		logger.WarnKV("failed to read stream open frame", "session_id", sess.SessionID, "stream_id", streamID, "error", err)
		return
	}

	if frame.Opcode != ipc.OpStreamOpen {
		logger.WarnKV("expected OpStreamOpen frame", "session_id", sess.SessionID, "stream_id", streamID, "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		return
	}

	// Parse stream type and args from payload
	streamType, args := ipc.ParseStreamOpenPayload(frame.Payload)
	logger.DebugKV("stream opened", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType, "args", args)

	// Look up registered stream handler
	handler, found := handlers.GetStreamHandler(streamType)
	if !found {
		logger.WarnKV("unknown stream type", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType)
		// Send close frame
		if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamClose,
			StreamID: frame.StreamID,
		}); err != nil {
			logger.DebugKV("failed to write close frame for unknown stream type", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType, "error", err)
		}
		return
	}

	// Execute stream handler
	if err := handler(sess, stream, args); err != nil {
		if !errors.Is(err, ipc.ErrAborted) {
			logger.WarnKV("stream handler error", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType, "error", err)
		}
	}
}
