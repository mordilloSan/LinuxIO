package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

const (
	indexerTimerUnitName         = "linuxio-indexer-index.timer"
	indexerTimerDropInPath       = "/etc/systemd/system/linuxio-indexer-index.timer.d/linuxio.conf"
	indexerLegacyTimerDropInPath = "/etc/systemd/system/linuxio-indexer-index.timer.d/override.conf"
)

var (
	updateTimerConfig = UpdateConfig
	writeTimerDropIn  = utils.WriteFileAtomic
	removeTimerDropIn = os.Remove
	enableTimerUnit   = systemdapi.EnableUnit
	disableTimerUnit  = systemdapi.DisableUnit
	stopTimerUnit     = systemdapi.StopUnit
	restartTimerUnit  = systemdapi.RestartUnit
)

func SetTimerInterval(ctx context.Context, raw string) (apischema.IndexerTimerSetResult, error) {
	interval, err := normalizeTimerInterval(raw)
	if err != nil {
		return apischema.IndexerTimerSetResult{}, err
	}

	patch, err := json.Marshal(indexerapi.ConfigPatch{Interval: &interval})
	if err != nil {
		return apischema.IndexerTimerSetResult{}, fmt.Errorf("encode indexer interval: %w", err)
	}
	cfg, _, err := updateTimerConfig(ctx, patch)
	if err != nil {
		return apischema.IndexerTimerSetResult{}, err
	}

	if interval == "0" {
		for _, path := range []string{indexerTimerDropInPath, indexerLegacyTimerDropInPath} {
			if err := removeTimerDropIn(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return apischema.IndexerTimerSetResult{}, fmt.Errorf("remove indexer timer override: %w", err)
			}
		}
		if err := disableTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
		if err := stopTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
	} else {
		body := []byte("[Timer]\nOnActiveSec=\nOnUnitActiveSec=\nOnActiveSec=" + interval + "\nOnUnitActiveSec=" + interval + "\n")
		if err := writeTimerDropIn(indexerTimerDropInPath, body, 0o644); err != nil {
			return apischema.IndexerTimerSetResult{}, fmt.Errorf("write indexer timer override: %w", err)
		}
		if err := removeTimerDropIn(indexerLegacyTimerDropInPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apischema.IndexerTimerSetResult{}, fmt.Errorf("remove legacy indexer timer override: %w", err)
		}
		if err := enableTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
		if err := restartTimerUnit(ctx, indexerTimerUnitName); err != nil {
			return apischema.IndexerTimerSetResult{}, err
		}
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
