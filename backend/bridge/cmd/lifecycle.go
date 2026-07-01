package cmd

import (
	"context"
	"fmt"
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
func openClientConnection() (net.Conn, error) {
	clientFile := os.NewFile(uintptr(clientConnFD), "client-conn")
	if clientFile == nil {
		return nil, fmt.Errorf("open client connection fd %d", clientConnFD)
	}
	clientConn, err := net.FileConn(clientFile)
	clientFile.Close()
	if err != nil {
		return nil, fmt.Errorf("create client connection from fd %d: %w", clientConnFD, err)
	}
	return clientConn, nil
}

// runBridge wires route registration, signal handling, request serving, and
// shutdown cleanup for one authenticated bridge process.
func runBridge(clientConn net.Conn, rt runtime.Runtime) {
	shutdownCh := make(chan string, 1)
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	router := handlers.RegisterAllHandlers(rt)
	startBridgeSignalHandler(shutdownCh)

	closeClientConn := newClientConnCloser(clientConn)
	startMainRequestLoop(sessionCtx, rt, router, clientConn, shutdownCh)
	sessionID := ""
	if rt.Session != nil {
		sessionID = rt.Session.SessionID
	}
	<-startBridgeCleanup(shutdownCh, closeClientConn, router.Registry(), sessionID, sessionCancel)
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
func startMainRequestLoop(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, clientConn net.Conn, shutdownCh chan<- string) {
	wg.Go(func() {
		handleYamuxSession(ctx, rt, router, clientConn)
		select {
		case shutdownCh <- "client disconnected":
		default:
		}
	})
}

// startBridgeCleanup waits for a shutdown reason, cancels session work, then
// closes the client connection and gives in-flight stream handlers a bounded
// drain window.
func startBridgeCleanup(shutdownCh <-chan string, closeClientConn func(), registry *bridgeipc.Registry, sessionID string, sessionCancel context.CancelFunc) <-chan struct{} {
	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-shutdownCh
		time.Sleep(50 * time.Millisecond)
		close(bridgeClosing)
		sessionCancel()
		if registry != nil {
			registry.CancelForSession(sessionID)
		}
		time.Sleep(100 * time.Millisecond)
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
