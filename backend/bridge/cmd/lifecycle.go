package cmd

import (
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const clientConnFD = 3

// Global shutdown signal for all handlers: closed when shutdown starts.
var bridgeClosing = make(chan struct{})

// Track in-flight requests to allow bounded wait on shutdown.
var wg sync.WaitGroup

// openClientConnection converts the inherited client file descriptor into the
// net.Conn used by yamux.
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

// runBridge wires route registration, signal handling, request serving, and
// shutdown cleanup for one authenticated bridge process.
func runBridge(clientConn net.Conn, rt runtime.Runtime) {
	shutdownCh := make(chan string, 1)
	router := handlers.RegisterAllHandlers(rt)
	startBridgeSignalHandler(shutdownCh)

	closeClientConn := newClientConnCloser(clientConn)
	startMainRequestLoop(rt, router, clientConn, closeClientConn, shutdownCh)
	<-startBridgeCleanup(shutdownCh, closeClientConn)
}

// startBridgeSignalHandler forwards SIGINT/SIGTERM into the bridge shutdown
// channel without blocking signal delivery.
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

// newClientConnCloser returns an idempotent closer for the inherited client
// connection so multiple shutdown paths can safely request closure.
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

// startMainRequestLoop runs the yamux serving loop and reports client
// disconnects as bridge shutdown reasons.
func startMainRequestLoop(rt runtime.Runtime, router *bridgeipc.Router, clientConn net.Conn, closeClientConn func(), shutdownCh chan<- string) {
	wg.Go(func() {
		defer closeClientConn()
		handleYamuxSession(rt, router, clientConn)
		select {
		case shutdownCh <- "client disconnected":
		default:
		}
	})
}

// startBridgeCleanup waits for a shutdown reason, closes the client connection,
// and gives in-flight stream handlers a bounded drain window.
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

// waitForInflightHandlers waits briefly for active stream handlers to finish
// before allowing the bridge process to exit.
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
