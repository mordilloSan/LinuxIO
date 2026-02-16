package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/protocol"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// readBootstrap reads binary bootstrap from stdin.
// The auth daemon writes bootstrap data to the bridge's stdin via a pipe.
// FAIL-FAST: If bootstrap is invalid, exit immediately with code 1.
// This ensures the auth daemon's exec-status pipe detects failure.
func readBootstrap() *protocol.Bootstrap {
	b, err := protocol.ReadBootstrap(os.Stdin)
	if err != nil {
		// Write to stderr because logger is not yet iniated(systemd captures to journal)
		fmt.Fprintf(os.Stderr, "bridge bootstrap error: failed to read: %v\n", err)
		os.Exit(1)
	}

	if b.SessionID == "" {
		fmt.Fprintf(os.Stderr, "bridge bootstrap error: missing required session_id)\n")
		os.Exit(1)
	}

	return b
}

// Bootstrap config and session - initialized in main() after CLI checks
var bootCfg *protocol.Bootstrap
var Sess *session.Session

// Global shutdown signal for all handlers: closed when shutdown starts.
var bridgeClosing = make(chan struct{})

// Track in-flight requests to allow bounded wait on shutdown.
var wg sync.WaitGroup

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
	if fileInfo, _ := os.Stdin.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		fmt.Println("(to be spawned by auth daemon, not for direct use)")
		return
	}

	// Read bootstrap from stdin (auth daemon pipes this)
	bootCfg = readBootstrap()
	Sess = &session.Session{
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
		os.Geteuid(), Sess.User.UID, Sess.User.GID)

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
		handleMainRequest(clientConn)
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

		logger.Debugf("Shutdown initiated: %s (user=%s, session=%s)",
			reason, Sess.User.Username, Sess.SessionID)

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
func handleMainRequest(conn net.Conn) {
	wg.Add(1)
	defer wg.Done()
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			if strings.Contains(cerr.Error(), "use of closed") {
				logger.DebugKV("bridge conn already closed", "session_id", Sess.SessionID)
			} else {
				logger.WarnKV("bridge conn close failed", "session_id", Sess.SessionID, "error", cerr)
			}
		}
	}()

	handleYamuxSession(conn)
}

// handleYamuxSession handles a yamux multiplexed connection.
// Each stream within the session is treated as an independent request.
func handleYamuxSession(conn net.Conn) {
	ymuxSession, err := ipc.NewYamuxServer(conn)
	if err != nil {
		logger.ErrorKV("failed to create yamux session", "session_id", Sess.SessionID, "error", err)
		return
	}
	defer ymuxSession.Close()

	logger.InfoKV("yamux session started", "session_id", Sess.SessionID)

	// Track active streams for graceful shutdown
	var streamWg sync.WaitGroup

	// Accept streams until session closes or bridge shuts down
	for {
		select {
		case <-bridgeClosing:
			logger.DebugKV("yamux session closing due to bridge shutdown", "session_id", Sess.SessionID)
			goto waitForStreams
		default:
		}

		stream, err := ymuxSession.Accept()
		if err != nil {
			if ymuxSession.IsClosed() {
				logger.DebugKV("yamux session closed", "session_id", Sess.SessionID)
			} else {
				logger.WarnKV("yamux accept error", "session_id", Sess.SessionID, "error", err)
			}
			break
		}

		var idBytes [16]byte
		n, err := rand.Read(idBytes[:])
		if err != nil {
			logger.WarnKV("failed to generate stream id", "session_id", Sess.SessionID, "error", err)
			continue
		}
		if n != len(idBytes) {
			logger.WarnKV("short random read for stream id", "session_id", Sess.SessionID, "read", n)
			continue
		}
		streamID := hex.EncodeToString(idBytes[:])
		s := stream
		sid := streamID
		streamWg.Go(func() {
			defer s.Close()

			handleYamuxStream(Sess, s, sid)
		})
	}

waitForStreams:
	// Wait for all streams to complete
	streamWg.Wait()
	logger.InfoKV("yamux session ended", "session_id", Sess.SessionID)
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
	handler, found := handlers.StreamHandlers[streamType]
	if !found {
		logger.WarnKV("unknown stream type", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType)
		// Send close frame
		_ = ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamClose,
			StreamID: frame.StreamID,
		})
		return
	}

	// Execute stream handler
	if err := handler(sess, stream, args); err != nil {
		if !errors.Is(err, ipc.ErrAborted) {
			logger.WarnKV("stream handler error", "session_id", sess.SessionID, "stream_id", streamID, "type", streamType, "error", err)
		}
	}
}
