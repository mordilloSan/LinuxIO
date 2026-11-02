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

// loadNotify reads what the worker should report; an unreadable settings
// file keeps the defaults, since the update itself must not depend on it.
func loadNotify() alerts.NotificationSettings {
	n, err := alerts.LoadNotifications()
	if err != nil {
		slog.Warn("Docker update notification settings unreadable", "component", "docker", "error", err)
		return alerts.NotificationSettings{DockerUpdates: alerts.DockerUpdateOnFailure}
	}
	return n
}

// raiseDockerUpdateAlert is best effort: failures are logged, never returned.
func raiseDockerUpdateAlert(ctx context.Context, containerName string, err error) {
	if loadNotify().DockerUpdates == alerts.DockerUpdateNever {
		return
	}
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

// raiseDockerUpdatedAlert records a successful scheduled replacement when
// the policy is on_change. Best effort: failures are logged, never returned.
func raiseDockerUpdatedAlert(ctx context.Context, containerName, oldImage, newImage string) {
	if loadNotify().DockerUpdates != alerts.DockerUpdateOnChange {
		return
	}
	name := strings.TrimPrefix(containerName, "/")
	obs := alerts.Observation{
		Source:       dockerUpdateAlertSource,
		Key:          "updated-" + dockerUpdateAlertKey(name),
		Severity:     alerts.SeverityInfo,
		Title:        "Docker container updated: " + name,
		Message:      "Image " + shortImageID(oldImage) + " was replaced by " + shortImageID(newImage) + ".",
		Link:         dockerUpdateAlertLink,
		OccurrenceID: newImage,
	}
	if err := alerts.Raise(ctx, obs); err != nil {
		slog.Warn("failed to record Docker updated alert", "component", "docker", "container", name, "error", err)
	}
}

// syncAvailableAlerts raises one alert per container with a newer image when
// the setting is on, and clears the alert of every container that no longer
// has one. Best effort: failures are logged, never returned.
func syncAvailableAlerts(ctx context.Context, statuses []ImageUpdateStatus) {
	wanted := loadNotify().DockerUpdatesAvailable
	for _, status := range statuses {
		name := strings.TrimPrefix(status.ContainerName, "/")
		key := "available-" + dockerUpdateAlertKey(name)
		if !wanted || status.CheckState != DockerUpdateCheckStateAvailable {
			if err := alerts.Resolve(ctx, dockerUpdateAlertSource, key); err != nil {
				slog.Debug("failed to clear Docker update available alert", "component", "docker", "container", name, "error", err)
			}
			continue
		}
		obs := alerts.Observation{
			Source:       dockerUpdateAlertSource,
			Key:          key,
			Severity:     alerts.SeverityInfo,
			Title:        "Docker update available: " + name,
			Message:      "A newer image is available for " + status.ImageRef + ".",
			Link:         dockerUpdateAlertLink,
			OccurrenceID: status.RemoteDigest,
		}
		if err := alerts.Raise(ctx, obs); err != nil {
			slog.Warn("failed to record Docker update available alert", "component", "docker", "container", name, "error", err)
		}
	}
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// resolveDockerUpdateAlert clears the failure and the update-available
// records of a container that was just replaced. Best effort: failures are
// logged, never returned.
func resolveDockerUpdateAlert(ctx context.Context, containerName string) {
	name := strings.TrimPrefix(containerName, "/")
	for _, key := range []string{dockerUpdateAlertKey(name), "available-" + dockerUpdateAlertKey(name)} {
		if err := alerts.Resolve(ctx, dockerUpdateAlertSource, key); err != nil {
			slog.Debug("failed to clear Docker update alert", "component", "docker", "container", name, "error", err)
		}
	}
}
