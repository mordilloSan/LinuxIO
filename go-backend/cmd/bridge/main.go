package main

import (
	"fmt"
	"os"

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

	secret := os.Getenv("LINUXIO_BRIDGE_SECRET")
	if secret == "" || len(secret) < 64 { // 32 bytes hex = 64 chars
		ppid := os.Getppid()
		parentCmd := ""
		if ppid > 1 {
			if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ppid)); err == nil && len(cmdline) > 0 {
				parentCmd = string(cmdline)
			}
		}
		if _, err := fmt.Fprintln(os.Stdout, "\n🚫 This program cannot be started directly! - (must be started by main LinuxIO process)"); err != nil {
			logger.Warnf("Failed to write to stdout: %v", err)
		}
		if _, err := fmt.Fprintf(os.Stdout, "  🧑‍💻  Parent PID: %d\n", ppid); err != nil {
			logger.Warnf("Failed to write to stdout: %v", err)
		}
		if parentCmd != "" {
			if _, err := fmt.Fprintf(os.Stdout, "  🖥️  Parent Cmd: %s\n", parentCmd); err != nil {
				logger.Warnf("Failed to write to stdout: %v", err)
			}
		}
		os.Exit(1)
	}

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

	cleanupDone := make(chan struct{}, 1)
	go func() {
		reason := <-ShutdownChan
		// Step 1: Signal accept loop to stop and close listener
		close(acceptDone)
		if err := listener.Close(); err != nil {
			logger.Warnf("failed to close listener: %v", err)
		}

		// Step 2: Do cleanup
		if err := cleanup.FullCleanup(reason, Sess, socketPath); err != nil {
			logger.Warnf("FullCleanup failed (reason=%q): %v", reason, err)
		}

		cleanupDone <- struct{}{}
	}()

	go func() {
		<-cleanupDone
		logger.Infof("✅ Bridge cleanup complete, exiting.")
		os.Exit(0)
	}()

	select {} // Block forever (until os.Exit)
}
