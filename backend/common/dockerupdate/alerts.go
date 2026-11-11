package dockerupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const (
	dockerUpdateAlertSource = "docker-update"
	// dockerCheckAlertSource is the source of the failed-check warnings.
	dockerCheckAlertSource = "docker-update-check"
	dockerUpdateAlertLink  = "/docker/containers"
	// dockerUpdatePassAlertKey is the key of the every_run pass notice
	// ("nothing to update", "N waiting"). It shares the source with the per-container alerts, whose keys
	// are the container name itself or it behind "updated-" or "available-",
	// and every key the store accepts is also a valid container name. A short
	// word such as "run" would let a container of that name have its failure
	// replaced by the notice, so the key is a name no one gives a container.
	dockerUpdatePassAlertKey = "linuxio-update-pass"
)

// dockerUpdateAlertKey is the container name; alerts normalises it into a key.
func dockerUpdateAlertKey(containerName string) string {
	if containerName == "" {
		return "unknown"
	}
	return containerName
}

// raiseDockerUpdateAlert records a failed scheduled update whatever the
// notification settings say: a failure is always reported. Best effort:
// failures are logged, never returned.
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

// raiseDockerUpdatedAlert records a successful scheduled replacement when
// alertReport, the Docker updates' alerts.ReportFor, asks for changes or every
// run. Best effort: failures are logged, never returned.
func raiseDockerUpdatedAlert(ctx context.Context, alertReport, containerName, oldImage, newImage string) {
	if alertReport != alerts.ReportChanges && alertReport != alerts.ReportEveryRun {
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

// raiseDockerPassAlert records a pass the every_run report asks for that
// raised nothing else. waiting is how many containers the pass left with a
// newer image; none is "nothing to update". Each pass is a new occurrence of
// the one notice. Best effort: failures are logged, never returned.
func raiseDockerPassAlert(ctx context.Context, now time.Time, waiting int) {
	title := "Docker updates: nothing to update"
	if waiting > 0 {
		title = fmt.Sprintf("Docker updates: %d waiting", waiting)
	}
	obs := alerts.Observation{
		Source:       dockerUpdateAlertSource,
		Key:          dockerUpdatePassAlertKey,
		Severity:     alerts.SeverityInfo,
		Title:        title,
		Link:         dockerUpdateAlertLink,
		OccurrenceID: now.UTC().Format(time.RFC3339Nano),
	}
	if err := alerts.Raise(ctx, obs); err != nil {
		slog.Warn("failed to record Docker update run notice", "component", "docker", "error", err)
	}
}

// syncAvailableAlerts raises one alert per container with a newer image when
// wanted, the DockerUpdatesAvailable setting, is on, and clears the alert of
// every container that no longer has one. Best effort: failures are logged,
// never returned.
func syncAvailableAlerts(ctx context.Context, statuses []ImageUpdateStatus, wanted bool) {
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

// syncCheckAlerts raises a warning for each container whose update check
// failed, such as one whose registry could not be reached, and clears the
// warning of every container this pass did not find failing: one whose check
// succeeded, and one that is gone or no longer checked. A check that keeps
// failing is one occurrence, so it is delivered once; the reason, which can
// change from pass to pass, is on the containers page. The warnings have
// their own source, so a container's name is its key. Best effort: failures
// are logged, never returned.
func syncCheckAlerts(ctx context.Context, statuses []ImageUpdateStatus) {
	failing := make(map[string]bool)
	for _, status := range statuses {
		if status.Err == "" {
			continue
		}
		name := strings.TrimPrefix(status.ContainerName, "/")
		key := dockerUpdateAlertKey(name)
		// Kept even when the raise fails: its warning still stands.
		failing[alerts.NormalizeKey(key)] = true
		obs := alerts.Observation{
			Source:       dockerCheckAlertSource,
			Key:          key,
			Severity:     alerts.SeverityWarning,
			Title:        "Docker update check failed: " + name,
			Message:      "The image " + status.ImageRef + " could not be checked for updates. The containers page shows the reason.",
			Link:         dockerUpdateAlertLink,
			OccurrenceID: "failing",
		}
		if err := alerts.Raise(ctx, obs); err != nil {
			slog.Warn("failed to record Docker update check alert", "component", "docker", "container", name, "error", err)
		}
	}
	doc, err := alerts.Load()
	if err != nil {
		slog.Warn("failed to read alerts to clear Docker update check alerts", "component", "docker", "error", err)
		return
	}
	for _, a := range doc.Alerts {
		if a.Source != dockerCheckAlertSource || failing[a.Key] {
			continue
		}
		if err := alerts.Resolve(ctx, a.Source, a.Key); err != nil {
			slog.Warn("failed to clear Docker update check alert", "component", "docker", "key", a.Key, "error", err)
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

// resolveDockerUpdateAlert clears the failure, the failed-check and the
// update-available records of a container that was just replaced. Best effort: failures are
// logged, never returned.
func resolveDockerUpdateAlert(ctx context.Context, containerName string) {
	name := strings.TrimPrefix(containerName, "/")
	for _, key := range []string{dockerUpdateAlertKey(name), "available-" + dockerUpdateAlertKey(name)} {
		if err := alerts.Resolve(ctx, dockerUpdateAlertSource, key); err != nil {
			slog.Debug("failed to clear Docker update alert", "component", "docker", "container", name, "error", err)
		}
	}
	if err := alerts.Resolve(ctx, dockerCheckAlertSource, dockerUpdateAlertKey(name)); err != nil {
		slog.Debug("failed to clear Docker update check alert", "component", "docker", "container", name, "error", err)
	}
}
