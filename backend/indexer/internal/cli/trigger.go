package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/logging"
)

func runIndexTrigger(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer timer trigger", flag.ContinueOnError)
	socketPath := fs.String("socket-path", "/run/linuxio/indexer.sock", "Unix socket path")
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), fmt.Sprintf("index trigger does not accept arguments: %s\n", fs.Arg(0)))
	}

	logging.Configure("indexer-index-trigger", false)
	if err := triggerIndex(*socketPath); err != nil {
		slog.Error("trigger index", "err", err)
		return 1
	}
	return 0
}

func triggerIndex(socketPath string) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+api.RouteIndex, nil)
	if err != nil {
		return fmt.Errorf("create index request: %w", err)
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return fmt.Errorf("request index over %s: %w", socketPath, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read index response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("index request returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
