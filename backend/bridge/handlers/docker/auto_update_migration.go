package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	legacyWatchtowerUnitName  = "linuxio-watchtower.service"
	legacyWatchtowerTimerName = "linuxio-watchtower.timer"
)

type legacyDockerUpdateArtifacts struct {
	binaryPath string
	envPath    string
	timerPath  string
	unitPath   string
}

// MigrateLegacyContainerUpdateSchedule preserves an existing selected-container
// schedule in the native LinuxIO format, then removes the obsolete Watchtower
// binary and units. It is idempotent and never changes an existing native
// configuration.
func MigrateLegacyContainerUpdateSchedule(ctx context.Context) error {
	return migrateLegacyContainerUpdateSchedule(
		ctx,
		defaultContainerAutoUpdateStore,
		defaultContainerUpdateSystemdOps,
		legacyDockerUpdateArtifacts{
			binaryPath: filepath.Join(version.BinDir, "linuxio-watchtower"),
			envPath:    legacyWatchtowerEnvPath,
			timerPath:  legacyWatchtowerTimerPath,
			unitPath:   "/etc/systemd/system/" + legacyWatchtowerUnitName,
		},
	)
}

func migrateLegacyContainerUpdateSchedule(
	ctx context.Context,
	store containerAutoUpdateStore,
	ops containerUpdateSystemdOps,
	artifacts legacyDockerUpdateArtifacts,
) error {
	legacyPresent, err := anyPathExists(artifacts.binaryPath, artifacts.envPath, artifacts.timerPath, artifacts.unitPath)
	if err != nil {
		return err
	}
	if !legacyPresent {
		return nil
	}

	nativeConfigCreated, legacyEnabled, err := prepareNativeUpdateSchedule(ctx, store, ops, artifacts)
	if err != nil {
		return err
	}
	if err := stopLegacyUpdateUnits(ctx, ops, artifacts); err != nil {
		return err
	}
	if err := ops.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd for native Docker update schedule: %w", err)
	}
	if err := activateMigratedUpdateTimer(ctx, ops, nativeConfigCreated && legacyEnabled); err != nil {
		return err
	}
	if err := removeLegacyUpdateArtifacts(artifacts); err != nil {
		return err
	}
	if err := ops.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd after removing legacy Docker update units: %w", err)
	}
	return nil
}

func prepareNativeUpdateSchedule(
	ctx context.Context,
	store containerAutoUpdateStore,
	ops containerUpdateSystemdOps,
	artifacts legacyDockerUpdateArtifacts,
) (bool, bool, error) {
	nativeConfigExists, err := pathExists(store.configPath)
	if err != nil || nativeConfigExists {
		return false, false, err
	}
	opts, err := store.readLegacyOptions()
	if err != nil {
		return false, false, err
	}
	legacyEnabled, err := legacyUpdateTimerEnabled(ctx, ops, artifacts.timerPath)
	if err != nil {
		return false, false, err
	}
	opts.Enabled = legacyEnabled
	if err := store.writeOptions(opts); err != nil {
		return false, false, err
	}
	return true, legacyEnabled, nil
}

func legacyUpdateTimerEnabled(ctx context.Context, ops containerUpdateSystemdOps, timerPath string) (bool, error) {
	timerExists, err := pathExists(timerPath)
	if err != nil || !timerExists {
		return false, err
	}
	state, err := ops.getUnitFileState(ctx, legacyWatchtowerTimerName)
	if err != nil {
		return false, fmt.Errorf("read legacy %s state: %w", legacyWatchtowerTimerName, err)
	}
	return state == "enabled" || state == "enabled-runtime", nil
}

func stopLegacyUpdateUnits(ctx context.Context, ops containerUpdateSystemdOps, artifacts legacyDockerUpdateArtifacts) error {
	timerExists, err := pathExists(artifacts.timerPath)
	if err != nil {
		return err
	}
	if timerExists {
		if err := ops.stopUnit(ctx, legacyWatchtowerTimerName); err != nil {
			return fmt.Errorf("stop legacy %s: %w", legacyWatchtowerTimerName, err)
		}
		if err := ops.disableUnit(ctx, legacyWatchtowerTimerName); err != nil {
			return fmt.Errorf("disable legacy %s: %w", legacyWatchtowerTimerName, err)
		}
	}
	unitExists, err := pathExists(artifacts.unitPath)
	if err != nil {
		return err
	}
	if unitExists {
		if err := ops.stopUnit(ctx, legacyWatchtowerUnitName); err != nil {
			return fmt.Errorf("stop legacy %s: %w", legacyWatchtowerUnitName, err)
		}
	}
	return nil
}

func activateMigratedUpdateTimer(ctx context.Context, ops containerUpdateSystemdOps, activate bool) error {
	if !activate {
		return nil
	}
	if err := ops.enableUnit(ctx, dockerUpdateTimerName); err != nil {
		return fmt.Errorf("enable migrated %s: %w", dockerUpdateTimerName, err)
	}
	if err := ops.startUnit(ctx, dockerUpdateTimerName); err != nil {
		return fmt.Errorf("start migrated %s: %w", dockerUpdateTimerName, err)
	}
	return nil
}

func removeLegacyUpdateArtifacts(artifacts legacyDockerUpdateArtifacts) error {
	var removeErrs []error
	for _, path := range []string{artifacts.binaryPath, artifacts.envPath, artifacts.timerPath, artifacts.unitPath} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			removeErrs = append(removeErrs, fmt.Errorf("remove legacy Docker update artifact %s: %w", path, err))
		}
	}
	return errors.Join(removeErrs...)
}

func anyPathExists(paths ...string) (bool, error) {
	for _, path := range paths {
		exists, err := pathExists(path)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func pathExists(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %s: %w", path, err)
}
