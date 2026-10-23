package dockerupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const (
	dockerUpdateAlertSource = "docker-update"
	dockerUpdateAlertLink   = "/docker/containers"
)

// dockerUpdateAlertKey is the container name; alerts normalises it into a key.
func dockerUpdateAlertKey(containerName string) string {
	if containerName == "" {
		return "unknown"
	}
	return containerName
}

// raiseDockerUpdateAlert is best effort: failures are logged, never returned.
func raiseDockerUpdateAlert(ctx context.Context, containerName string, err error) {
	name := strings.TrimPrefix(containerName, "/")
	sum := sha256.Sum256([]byte(err.Error()))
	obs := alerts.Observation{
		Source:       dockerUpdateAlertSource,
		Key:          dockerUpdateAlertKey(name),
		Severity:     alerts.SeverityError,
		Title:        "Docker update failed: " + name,
		Message:      err.Error(),
		Link:         dockerUpdateAlertLink,
		OccurrenceID: hex.EncodeToString(sum[:]),
	}
	if raiseErr := alerts.Raise(ctx, obs); raiseErr != nil {
		slog.Warn("failed to record Docker update alert", "component", "docker", "container", name, "error", raiseErr)
	}
}

// resolveDockerUpdateAlert is best effort: failures are logged, never returned.
func resolveDockerUpdateAlert(ctx context.Context, containerName string) {
	name := strings.TrimPrefix(containerName, "/")
	if err := alerts.Resolve(ctx, dockerUpdateAlertSource, dockerUpdateAlertKey(name)); err != nil {
		slog.Debug("failed to clear Docker update alert", "component", "docker", "container", name, "error", err)
	}
}
