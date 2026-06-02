package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const (
	indexerTimerUnitName       = "indexer-index.timer"
	indexerTimerCommandTimeout = 45 * time.Second
)

var (
	indexerCLILookPath = exec.LookPath
	indexerCLIStat     = os.Stat
	indexerCLIOutput   = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

var indexerCLIFallbackDirs = []string{
	"/usr/local/bin",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
}

func SetTimerInterval(ctx context.Context, raw string) (apischema.IndexerTimerSetResult, error) {
	interval, err := normalizeTimerInterval(raw)
	if err != nil {
		return apischema.IndexerTimerSetResult{}, err
	}
	binary, err := findIndexerCLI()
	if err != nil {
		return apischema.IndexerTimerSetResult{}, err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, indexerTimerCommandTimeout)
	defer cancel()

	output, runErr := indexerCLIOutput(cmdCtx, binary, "config", "set", "--interval", interval)
	if runErr != nil {
		return apischema.IndexerTimerSetResult{}, indexerCLICommandError("set timer interval", runErr, output)
	}

	cfg, err := readIndexerCLIConfig(cmdCtx, binary)
	if err != nil {
		return apischema.IndexerTimerSetResult{
			Interval:  interval,
			TimerUnit: indexerTimerUnitName,
		}, err
	}

	return apischema.IndexerTimerSetResult{
		Config:    cfg,
		Interval:  cfg.Interval,
		TimerUnit: indexerTimerUnitName,
	}, nil
}

func normalizeTimerInterval(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", bridgeipc.ErrInvalidArgs
	}
	if trimmed == "0" {
		return "0", nil
	}
	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid timer interval %q: %w", trimmed, err)
	}
	if duration < 0 {
		return "", fmt.Errorf("timer interval must be non-negative")
	}
	if duration == 0 {
		return "0", nil
	}
	return duration.String(), nil
}

func findIndexerCLI() (string, error) {
	if path, err := indexerCLILookPath("indexer"); err == nil {
		return path, nil
	}
	for _, dir := range indexerCLIFallbackDirs {
		path := filepath.Join(dir, "indexer")
		info, err := indexerCLIStat(path)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("indexer CLI not found")
}

func readIndexerCLIConfig(ctx context.Context, binary string) (apischema.IndexerConfig, error) {
	output, err := indexerCLIOutput(ctx, binary, "config")
	if err != nil {
		return apischema.IndexerConfig{}, indexerCLICommandError("read timer config", err, output)
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(output), maxIndexerConfigPayloadBytes))
	var cfg apischema.IndexerConfig
	if err := decoder.Decode(&cfg); err != nil {
		return apischema.IndexerConfig{}, fmt.Errorf("decode indexer config: %w", err)
	}
	return cfg, nil
}

func indexerCLICommandError(action string, err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, message)
}
