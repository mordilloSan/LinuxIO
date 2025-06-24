package main

import (
	"fmt"
	"os"
	"time"

	"go-backend/cmd/bridge/cleanup"
	"go-backend/cmd/bridge/handlers"
	"go-backend/internal/bridge"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"go-backend/internal/theme"
	"go-backend/internal/utils"

	"github.com/google/uuid"
)

// Build minimal session object
var Sess = &session.Session{
	SessionID: os.Getenv("LINUXIO_SESSION_ID"),
	User:      utils.User{ID: os.Getenv("LINUXIO_SESSION_USER"), Name: os.Getenv("LINUXIO_SESSION_USER")},
}

func main() {

	env := os.Getenv("GO_ENV")
	if env == "" {
		env = "production"
	}
	verbose := os.Getenv("VERBOSE") == "true"
	logger.Init(env, verbose)

	logger.Infof("📦 Checking for default configuration...")
	if err := utils.EnsureStartupDefaults(); err != nil {
		logger.Errorf("❌ Error setting config files: %v", err)
	}

	logger.Infof("📦 Loading theme config...")
	if err := theme.InitTheme(); err != nil {
		logger.Errorf("❌ Failed to initialize theme file: %v", err)
	}

	socketPath, err := bridge.BridgeSocketPath(Sess)
	if err != nil {
		logger.Errorf("❌ Failed to determine bridge socket path: %v", err)
		os.Exit(1)
	}
	listener, _, _, err := bridge.CreateAndOwnSocket(socketPath, Sess.User.ID)
	if err != nil {
		logger.Error.Fatalf("❌ %v", err)
		os.Exit(1)
	}

	if env == "production" {
		cleanup.KillLingeringBridgeStartupProcesses()
	}

	ShutdownChan := make(chan string, 1)
	handlers.ShutdownChan = ShutdownChan
	handlers.RegisterAllHandlers(ShutdownChan)

	acceptDone := make(chan struct{})

	// Accept loop (stops when acceptDone is closed)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-acceptDone:
					return
				default:
					logger.Warnf("⚠️ Accept failed: %v", err)
				}
				continue
			}
			id := uuid.NewString()
			logger.Debugf("MAIN: spawning handler %s", id)
			go bridge.HandleMainRequest(conn, id)
		}
	}()

	// Healthcheck goroutine
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			logger.LogToFile("Healthcheck: pinging main process")
			ok := cleanup.CheckMainProcessHealth(Sess)
			logger.LogToFile(fmt.Sprintf("Healthcheck result: %v", ok))
			if !ok {
				select {
				case ShutdownChan <- "Healthcheck failed (main process unreachable or session invalid)":
				default: // already shutting down
				}
				return
			}
		}
	}()

	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-ShutdownChan
		// Step 1: Signal accept loop to stop and close listener
		close(acceptDone)
		listener.Close()
		// Step 2: Do cleanup
		cleanup.FullCleanup(reason, Sess, socketPath)
		cleanupDone <- struct{}{}
	}()

	go func() {
		<-cleanupDone
		logger.LogToFile("✅ Bridge cleanup complete, exiting.")
		os.Exit(0)
	}()

	select {} // Block forever (until os.Exit)
}
