package docker

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

// dockerUpdateAlertKey maps a container name onto the alert key alphabet
// [a-z0-9._-], starting with [a-z0-9], at most 64 characters.
func dockerUpdateAlertKey(containerName string) string {
	name := strings.ToLower(strings.TrimPrefix(containerName, "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	key := b.String()
	if key == "" {
		return "unknown"
	}
	if key[0] == '.' || key[0] == '-' {
		key = "x" + key
	}
	if len(key) > 64 {
		key = key[:64]
	}
	return key
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
	if len(obs.Message) > alerts.MaxMessageLen {
		obs.Message = obs.Message[:alerts.MaxMessageLen]
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
