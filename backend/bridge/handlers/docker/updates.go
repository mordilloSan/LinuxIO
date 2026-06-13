package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/watchtower"
)

// watchtowerRun is a seam for tests; production code always execs the
// installed linuxio-watchtower binary.
var watchtowerRun = watchtower.Run

// RefreshDockerImageUpdates runs a Watchtower monitor-only one-shot over all
// running containers and refreshes the update cache from its report.
func RefreshDockerImageUpdates(ctx context.Context) (apischema.DockerUpdateCheckResult, error) {
	results, err := watchtowerRun(ctx, watchtower.Target{All: true}, watchtower.Options{MonitorOnly: true})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}

	cli, err := getClient()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("failed to list containers: %w", err)
	}
	summaries := make(map[string]container.Summary, len(containers.Items))
	for _, ctr := range containers.Items {
		summaries[primaryContainerName(ctr)] = ctr
	}

	statuses, result := updateCheckStatuses(results, summaries, time.Now())
	replaceImageUpdateCache(statuses)
	return result, nil
}

// RefreshContainerImageUpdate runs a Watchtower monitor-only one-shot for a
// single container and refreshes only that container's update cache entry.
func RefreshContainerImageUpdate(ctx context.Context, containerID string) (apischema.DockerUpdateCheckResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("inspect container: %w", err)
	}
	inspect := inspectResult.Container
	name := strings.TrimPrefix(inspect.Name, "/")
	if name == "" {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("container %s has no name", inspect.ID)
	}

	results, err := watchtowerRun(ctx, watchtower.Target{Names: []string{name}}, watchtower.Options{MonitorOnly: true})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}
	res, ok := watchtowerResultFor(results, name)
	if !ok {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("watchtower reported no result for container %q", name)
	}
	if res.Image == "" && inspect.Config != nil {
		res.Image = inspect.Config.Image
	}

	statuses, result := updateCheckStatuses(
		[]watchtower.Result{res},
		map[string]container.Summary{
			name: {
				ID:      inspect.ID,
				ImageID: inspect.Image,
			},
		},
		time.Now(),
	)
	updateImageUpdateCache(statuses)
	return result, nil
}

// updateCheckStatuses maps Watchtower results onto cache entries, resolving
// container/image IDs through the running-container summaries (porcelain
// output only carries names).
func updateCheckStatuses(results []watchtower.Result, summaries map[string]container.Summary, now time.Time) ([]imageUpdateStatus, apischema.DockerUpdateCheckResult) {
	statuses := make([]imageUpdateStatus, 0, len(results))
	var result apischema.DockerUpdateCheckResult
	for _, res := range results {
		status := imageUpdateStatus{
			ContainerName:   res.Name,
			ImageRef:        res.Image,
			UpdateAvailable: res.State == watchtower.StateStale,
			CheckedAt:       now,
			Err:             res.Err,
		}
		if res.State == watchtower.StateFailed && status.Err == "" {
			status.Err = "update check failed"
		}
		if ctr, ok := summaries[res.Name]; ok {
			status.ContainerID = ctr.ID
			status.ImageID = ctr.ImageID
		}
		statuses = append(statuses, status)

		result.Checked++
		if status.Err != "" {
			result.Errors++
		}
		if status.UpdateAvailable {
			result.Updates++
		}
	}
	return statuses, result
}

// UpdateContainer updates a single container through a Watchtower one-shot
// run targeting it by name.
func UpdateContainer(ctx context.Context, containerID string) (apischema.DockerContainerUpdateResult, error) {
	result, err := updateContainer(ctx, containerID)
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

func updateContainer(ctx context.Context, containerID string) (apischema.DockerContainerUpdateResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("inspect container: %w", err)
	}
	inspect := inspectResult.Container
	name := strings.TrimPrefix(inspect.Name, "/")
	result := apischema.DockerContainerUpdateResult{
		ContainerID:     inspect.ID,
		ContainerName:   name,
		PreviousImageID: inspect.Image,
	}
	if inspect.Config != nil {
		result.Image = inspect.Config.Image
	}
	if name == "" {
		return result, fmt.Errorf("container %s has no name", inspect.ID)
	}

	results, err := watchtowerRun(ctx, watchtower.Target{Names: []string{name}}, watchtower.Options{})
	if err != nil {
		return result, err
	}
	res, ok := watchtowerResultFor(results, name)
	if !ok {
		return result, fmt.Errorf("watchtower reported no result for container %q", name)
	}

	reinspect, err := applyUpdateOutcome(&result, res)
	if err != nil || !reinspect {
		return result, err
	}

	// An updated container is recreated under the same name with a new ID;
	// re-inspect by name for the post-update image and refresh the cache.
	after, inspectErr := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if inspectErr == nil {
		result.NewImageID = after.Container.Image
		markContainerCurrent(inspect.ID, after.Container)
	}
	return result, nil
}

// applyUpdateOutcome maps a Watchtower per-container result onto the API
// result. It reports whether the container should be re-inspected because
// Watchtower may have replaced or restarted it.
func applyUpdateOutcome(result *apischema.DockerContainerUpdateResult, res watchtower.Result) (bool, error) {
	switch res.State {
	case watchtower.StateUpdated:
		result.Updated = true
		return true, nil
	case watchtower.StateRestarted:
		return true, nil
	case watchtower.StateFresh, watchtower.StateScanned:
		return false, nil
	case watchtower.StateStale:
		// Update available but not applied — Watchtower treated the
		// container as monitor-only.
		result.Error = "update available but not applied (container is monitor-only)"
		return false, nil
	case watchtower.StateSkipped:
		result.Error = res.Err
		if result.Error == "" {
			result.Error = "update skipped"
		}
		return false, nil
	case watchtower.StateFailed:
		msg := res.Err
		if msg == "" {
			msg = "unknown error"
		}
		return false, fmt.Errorf("watchtower update failed: %s", msg)
	default:
		return false, fmt.Errorf("unexpected watchtower state %q for container %q", res.State, result.ContainerName)
	}
}

func watchtowerResultFor(results []watchtower.Result, name string) (watchtower.Result, bool) {
	for _, res := range results {
		if res.Name == name {
			return res, true
		}
	}
	return watchtower.Result{}, false
}
