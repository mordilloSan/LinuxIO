package cmd

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/common/utils"
	"github.com/mordilloSan/LinuxIO/server/bridge"
	"github.com/mordilloSan/LinuxIO/server/cleanup"
	"github.com/mordilloSan/LinuxIO/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/server/web"
)

func RunServer(cfg ServerConfig) {
	// -------------------------------------------------------------------------
	// Env + logging (from flags)
	// -------------------------------------------------------------------------
	env := strings.ToLower(cfg.Env) // "development" | "production"
	verbose := cfg.Verbose
	logger.Init(env, verbose)
	if !(env == "development" && verbose) {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Infof("🌱 Starting server in %s mode...", env)

	// -------------------------------------------------------------------------
	// Sessions + cleanup hooks
	// -------------------------------------------------------------------------
	ms := session.New()
	sm := session.NewManager(ms, session.SessionConfig{
		Cookie: session.CookieConfig{
			Secure: env == "production",
		},
	})
	sm.RegisterOnDelete(func(sess session.Session, reason session.DeleteReason) {
		if sess.User.Username != "" {
			if _, err := bridge.CallWithSession(&sess, "control", "shutdown", []string{string(reason)}); err != nil {
				logger.Warnf("Bridge shutdown for %s failed: %v", sess.SessionID, err)
			}
		}
	})

	// -------------------------------------------------------------------------
	// Frontend assets
	// -------------------------------------------------------------------------
	ui, err := web.UI()
	if err != nil {
		logger.Error.Fatalf("failed to mount embedded frontend: %v", err)
	}

	// -------------------------------------------------------------------------
	// Start required services
	// -------------------------------------------------------------------------
	filebrowserSecret := utils.GenerateSecretKey(32)
	filebrowser.StartServices(filebrowserSecret, verbose)

	// -------------------------------------------------------------------------
	// Router
	// -------------------------------------------------------------------------
	router := BuildRouter(Config{
		Env:               env,
		Verbose:           verbose,
		VitePort:          cfg.ViteDevPort,
		FilebrowserSecret: filebrowserSecret,
		UI:                ui,
	}, sm)

	// -------------------------------------------------------------------------
	// HTTP(S) server
	// -------------------------------------------------------------------------
	srv := &http.Server{
		Handler:  router,
		ErrorLog: log.New(HTTPErrorLogAdapter{}, "", 0),
	}

	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// self-bind (dev and local prod)
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv.Addr = addr

	go func() {
		var err error
		if env == "production" {
			cert, cErr := web.GenerateSelfSignedCert()
			if cErr != nil {
				logger.Error.Fatalf(" Failed to generate self-signed certificate: %v", cErr)
			}
			srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			web.SetRootPoolFromServerCert(cert)

			// Export base URL and certificate for child bridge processes.
			baseURL := fmt.Sprintf("https://localhost:%d", cfg.Port)
			_ = os.Setenv("LINUXIO_SERVER_BASE_URL", baseURL)
			if len(cert.Certificate) > 0 {
				pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
				if len(pemBytes) > 0 {
					_ = os.Setenv("LINUXIO_SERVER_CERT", string(pemBytes))
				}
			}

			fmt.Printf("🚀 Server running at https://localhost:%d\n", cfg.Port)
			logger.Infof("HTTP server listening at https://localhost:%d", cfg.Port)
			err = srv.ListenAndServeTLS("", "")
		} else {
			// Development: advertise HTTP base URL for bridge processes.
			baseURL := fmt.Sprintf("http://localhost:%d", cfg.Port)
			_ = os.Setenv("LINUXIO_SERVER_BASE_URL", baseURL)
			logger.Infof("HTTP server listening at http://localhost:%d", cfg.Port)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error.Fatalf("server error: %v", err)
		}
		close(done)
	}()

	// -------------------------------------------------------------------------
	// Shutdown coordination
	// -------------------------------------------------------------------------
	select {
	case <-quit:
		logger.Infof("🛑 Shutdown signal received")
	case <-done:
		logger.Infof("🚨 HTTP server stopped unexpectedly, beginning shutdown...")
	}

	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warnf("⏳ Graceful HTTP shutdown timed out; forcing close of remaining connections.")
			if cerr := srv.Close(); cerr != nil && !errors.Is(cerr, http.ErrServerClosed) {
				logger.Warnf("HTTP server force-close error: %v", cerr)
			}
		} else {
			logger.Warnf("HTTP server shutdown error: %v", err)
		}
	} else {
		logger.Infof("HTTP server closed")
	}

	// Stop background/attached services
	cleanup.CleanupFilebrowserContainer()

	// Tell bridges to quit before sessions close
	cleanup.ShutdownAllBridges(sm, "server_quit")

	// Close sessions
	sm.Close()

	if env == "production" {
		fmt.Println("Server stopped.")
	}
	logger.Infof("Server stopped.")
}
