package dockerupdate

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ConfigPath is where the scheduled-update configuration is persisted.
const ConfigPath = version.ConfigDir + "/docker-update.json"

const (
	dockerUpdateConfigVersion = 1
	defaultDockerUpdateTime   = "04:00"
)

var dailyDockerUpdateTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

type containerAutoUpdateStore struct {
	configPath string
	timerPath  string
	unitPath   string
}

type dockerUpdateScheduleDocument struct {
	Version        int                           `json:"version"`
	Mode           DockerContainerAutoUpdateMode `json:"mode"`
	Time           string                        `json:"time"`
	Cleanup        bool                          `json:"cleanup"`
	ContainerNames []string                      `json:"container_names"`
	IncludeStopped bool                          `json:"include_stopped"`
	UpdateAll      bool                          `json:"update_all"`
	UpdateStopped  bool                          `json:"update_stopped"`
	ReviveStopped  bool                          `json:"revive_stopped"`
}

var defaultContainerAutoUpdateStore = containerAutoUpdateStore{
	configPath: ConfigPath,
	timerPath:  dockerUpdateTimerPath,
	unitPath:   dockerUpdateUnitPath,
}

func (s containerAutoUpdateStore) readOptions() (DockerContainerAutoUpdateOptions, error) {
	data, err := os.ReadFile(s.configPath)
	if err == nil {
		var document dockerUpdateScheduleDocument
		if decodeErr := json.Unmarshal(data, &document); decodeErr != nil {
			return defaultContainerAutoUpdateOptions(), fmt.Errorf("decode %s: %w", s.configPath, decodeErr)
		}
		if document.Version != dockerUpdateConfigVersion {
			return defaultContainerAutoUpdateOptions(), fmt.Errorf("unsupported Docker update config version %d", document.Version)
		}
		return normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{
			Mode:           document.Mode,
			Time:           document.Time,
			Cleanup:        document.Cleanup,
			ContainerNames: document.ContainerNames,
			IncludeStopped: document.IncludeStopped,
			UpdateAll:      document.UpdateAll,
			UpdateStopped:  document.UpdateStopped,
			ReviveStopped:  document.ReviveStopped,
		})
	}
	if !errors.Is(err, os.ErrNotExist) {
		return defaultContainerAutoUpdateOptions(), fmt.Errorf("read %s: %w", s.configPath, err)
	}
	return defaultContainerAutoUpdateOptions(), nil
}

func (s containerAutoUpdateStore) writeOptions(opts DockerContainerAutoUpdateOptions) error {
	document := dockerUpdateScheduleDocument{
		Version:        dockerUpdateConfigVersion,
		Mode:           opts.Mode,
		Time:           opts.Time,
		Cleanup:        opts.Cleanup,
		ContainerNames: opts.ContainerNames,
		IncludeStopped: opts.IncludeStopped,
		UpdateAll:      opts.UpdateAll,
		UpdateStopped:  opts.UpdateStopped,
		ReviveStopped:  opts.ReviveStopped,
	}
	configBytes, err := json.Marshal(document, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode Docker update config: %w", err)
	}
	configBytes = append(configBytes, '\n')
	timerBytes, err := renderDockerUpdateTimer(opts.Time)
	if err != nil {
		return err
	}

	if err := atomicfile.Write(s.configPath, configBytes, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.configPath, err)
	}
	if err := atomicfile.Write(s.unitPath, renderDockerUpdateUnit(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.unitPath, err)
	}
	if err := atomicfile.Write(s.timerPath, timerBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.timerPath, err)
	}
	return nil
}

func defaultContainerAutoUpdateOptions() DockerContainerAutoUpdateOptions {
	return DockerContainerAutoUpdateOptions{Mode: "update", Time: defaultDockerUpdateTime}
}

func normalizeContainerAutoUpdateOptions(opts DockerContainerAutoUpdateOptions) (DockerContainerAutoUpdateOptions, error) {
	mode := strings.TrimSpace(string(opts.Mode))
	if mode == "" {
		mode = "update"
	}
	if mode != "update" && mode != "check_only" {
		return DockerContainerAutoUpdateOptions{}, fmt.Errorf("invalid Docker update mode %q", opts.Mode)
	}
	timeOfDay := strings.TrimSpace(opts.Time)
	if timeOfDay == "" {
		timeOfDay = defaultDockerUpdateTime
	}
	if !dailyDockerUpdateTimePattern.MatchString(timeOfDay) {
		return DockerContainerAutoUpdateOptions{}, fmt.Errorf("invalid Docker update time %q", opts.Time)
	}
	if opts.ReviveStopped && !opts.UpdateStopped {
		return DockerContainerAutoUpdateOptions{}, errors.New("starting stopped containers after update requires stopped-container updates")
	}
	return DockerContainerAutoUpdateOptions{
		Cleanup:        opts.Cleanup,
		ContainerNames: normalizeContainerNames(opts.ContainerNames),
		Enabled:        opts.Enabled,
		IncludeStopped: opts.IncludeStopped,
		Mode:           DockerContainerAutoUpdateMode(mode),
		ReviveStopped:  opts.ReviveStopped,
		Time:           timeOfDay,
		UpdateAll:      opts.UpdateAll,
		UpdateStopped:  opts.UpdateStopped,
	}, nil
}

func normalizeContainerNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
