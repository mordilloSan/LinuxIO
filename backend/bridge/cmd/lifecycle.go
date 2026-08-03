package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const clientConnFD = 3

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

	done := startMainRequestLoop(sessionCtx, rt, router, clientConn, shutdownCh)
	reason := <-shutdownCh
	shutdownBridge(clientConn, router.Registry(), rt.Session.SessionID, sessionCancel, done)
	slog.Debug("shutdown initiated", "reason", reason, "user", rt.Session.User.Username, "session_id", rt.Session.SessionID)
}

// shutdownBridge cancels owned work before closing the transport that releases
// the yamux accept loop, then gives that loop and its streams a bounded drain.
func shutdownBridge(clientConn net.Conn, registry *bridgeipc.Registry, sessionID string, sessionCancel context.CancelFunc, done <-chan struct{}) {
	sessionCancel()
	registry.CancelForSession(sessionID)
	// Closing the transport unblocks yamux Accept before waiting for the loop.
	if err := clientConn.Close(); err != nil {
		slog.Debug("client conn close", "error", err)
	}
	waitForBridgeLoop(done)
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

// startMainRequestLoop runs the yamux serving loop and reports client
// disconnects as bridge shutdown reasons.
func startMainRequestLoop(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, clientConn net.Conn, shutdownCh chan<- string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleYamuxSession(ctx, rt, router, clientConn)
		select {
		case shutdownCh <- "client disconnected":
		default:
		}
	}()
	return done
}

// waitForBridgeLoop waits briefly for the session loop and its stream handlers.
func waitForBridgeLoop(done <-chan struct{}) {
	const grace = 5 * time.Second
	select {
	case <-done:
		slog.Debug("bridge session drained", "grace_period", grace)
	case <-time.After(grace):
		slog.Warn("bridge session exceeded drain grace", "grace_period", grace)
	}
}
