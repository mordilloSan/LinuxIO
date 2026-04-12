package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	commonconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	commonpcpapi "github.com/mordilloSan/LinuxIO/backend/common/pcpapi"
	internalpcp "github.com/mordilloSan/LinuxIO/backend/internal/pcp"
)

func main() {
	switch parseCommand(os.Args[1:]) {
	case "help":
		printHelp()
	case "version":
		fmt.Printf("LinuxIO PCP API %s\n", commonconfig.Version)
	case "run":
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		printHelp()
		os.Exit(1)
	}
}

func parseCommand(args []string) string {
	if len(args) == 0 {
		return "run"
	}
	switch args[0] {
	case "run":
		return "run"
	case "version", "--version", "-v":
		return "version"
	case "help", "--help", "-h":
		return "help"
	default:
		return ""
	}
}

func printHelp() {
	fmt.Println(`LinuxIO PCP API

Usage:
  linuxio-pcp-api run
  linuxio-pcp-api version
  linuxio-pcp-api help`)
}

func run() error {
	logger.Init(logger.Config{
		Levels: []logger.Level{logger.InfoLevel, logger.WarnLevel, logger.ErrorLevel},
	})

	cfg, token, err := commonpcpapi.EnsureDefaultFiles(commonpcpapi.DefaultConfigPath, commonpcpapi.DefaultTokenPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	collector, err := internalpcp.NewLiveCollector("local:")
	if err != nil {
		return fmt.Errorf("start PCP collector: %w", err)
	}
	defer collector.Close()

	app := newApp(collector, cfg, token)

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	reloadSignals := make(chan os.Signal, 1)
	signal.Notify(reloadSignals, syscall.SIGHUP)
	defer signal.Stop(reloadSignals)

	go func() {
		for range reloadSignals {
			if err := app.reloadRuntime(); err != nil {
				logger.Errorf("reload failed: %v", err)
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Infof("linuxio-pcp-api listening on %s", cfg.ListenAddress)
		errCh <- server.ListenAndServe()
	}()

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-shutdownCtx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
