package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	indexerTimerUnitName   = "linuxio-indexer-index.timer"
	indexerTimerDropInPath = "/etc/systemd/system/linuxio-indexer-index.timer.d/linuxio.conf"
)

var (
	writeTimerDropIn  = utils.WriteFileAtomic
	removeTimerDropIn = os.Remove
	enableTimerUnit   = systemdapi.EnableUnit
	disableTimerUnit  = systemdapi.DisableUnit
	stopTimerUnit     = systemdapi.StopUnit
	restartTimerUnit  = systemdapi.RestartUnit
	getTimerInterval  = systemdapi.GetTimerInterval
)

func currentTimerInterval(ctx context.Context) (string, error) {
	interval, err := getTimerInterval(ctx, indexerTimerUnitName)
	if err != nil {
		return "", fmt.Errorf("read indexer timer interval: %w", err)
	}
	if interval == 0 {
		return "0", nil
	}
	return interval.String(), nil
}

func SetTimerInterval(ctx context.Context, raw string) (apischema.IndexerTimerSetResult, error) {
	interval, err := normalizeTimerInterval(raw)
	if err != nil {
		return apischema.IndexerTimerSetResult{}, err
	}

	if interval == "0" {
		if err := removeTimerDropIn(indexerTimerDropInPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apischema.IndexerTimerSetResult{}, fmt.Errorf("remove indexer timer override: %w", err)
		}
		if err := disableTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
		if err := stopTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
	} else {
		body := []byte("[Timer]\nOnUnitActiveSec=\nOnUnitActiveSec=" + interval + "\n")
		if err := writeTimerDropIn(indexerTimerDropInPath, body, 0o644); err != nil {
			return apischema.IndexerTimerSetResult{}, fmt.Errorf("write indexer timer override: %w", err)
		}
		if err := enableTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
		if err := restartTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
	}

	return apischema.IndexerTimerSetResult{
		Interval: interval,
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
