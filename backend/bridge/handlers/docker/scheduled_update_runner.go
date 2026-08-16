package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type scheduledComposeTarget struct {
	target   composeProjectTarget
	services []string
	before   []container.InspectResponse
}

type scheduledPassOperations struct {
	list   func(context.Context) ([]container.Summary, error)
	check  func(context.Context, []container.Summary) error
	update func(context.Context, []container.Summary, []container.Summary, apischema.DockerContainerAutoUpdateOptions) error
}

// RunScheduledContainerUpdates executes one configured check or update pass.
// It is called by the short-lived Docker update worker; no updater
// daemon remains resident between timer activations.
func RunScheduledContainerUpdates(ctx context.Context, configPath string) error {
	store := defaultContainerAutoUpdateStore
	if strings.TrimSpace(configPath) != "" {
		store.configPath = configPath
	}
	opts, err := store.readOptions()
	if err != nil {
		return err
	}

	release, err := acquireDockerUpdateLock(ctx)
	if err != nil {
		return err
	}
	defer release()

	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	if err := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); err != nil {
		return fmt.Errorf("recover previous standalone Docker update: %w", err)
	}

	return runScheduledPass(ctx, opts, scheduledPassOperations{
		list: func(ctx context.Context) ([]container.Summary, error) {
			containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
			return containers.Items, err
		},
		check: func(ctx context.Context, summaries []container.Summary) error {
			return runScheduledUpdateCheck(ctx, cli, summaries)
		},
		update: func(ctx context.Context, summaries, all []container.Summary, opts apischema.DockerContainerAutoUpdateOptions) error {
			return runScheduledUpdates(ctx, cli, summaries, all, opts)
		},
	})
}

func runScheduledPass(ctx context.Context, opts apischema.DockerContainerAutoUpdateOptions, operations scheduledPassOperations) error {
	containers, err := operations.list(ctx)
	if err != nil {
		return fmt.Errorf("list scheduled Docker update targets: %w", err)
	}
	targetByName := make(map[string]container.Summary, len(containers))
	for _, summary := range containers {
		if name := primaryContainerName(summary); name != "" {
			targetByName[name] = summary
		}
	}
	running := make([]container.Summary, 0, len(containers))
	for _, summary := range containers {
		if summary.State == container.StateRunning {
			running = append(running, summary)
		}
	}

	selected := make([]container.Summary, 0, len(opts.ContainerNames))
	var runErrs []error
	for _, name := range opts.ContainerNames {
		summary, ok := targetByName[name]
		if !ok {
			runErrs = append(runErrs, fmt.Errorf("scheduled container %q no longer exists", name))
			continue
		}
		selected = append(selected, summary)
	}

	checkTargets := running
	if opts.IncludeStopped {
		checkTargets = make([]container.Summary, 0, len(containers))
		for _, summary := range containers {
			if summary.State == container.StateRunning || summary.State == container.StateExited {
				checkTargets = append(checkTargets, summary)
			}
		}
	}
	// Every scheduled pass refreshes availability for the configured scope.
	// Selection only controls which containers are subsequently updated.
	checkErr := operations.check(ctx, checkTargets)
	if opts.Mode == "check_only" {
		return checkErr
	}
	var updateErr error
	if len(selected) > 0 {
		updateErr = operations.update(ctx, selected, containers, opts)
	}
	return errors.Join(append(runErrs, checkErr, updateErr)...)
}

func runScheduledUpdateCheck(ctx context.Context, cli *client.Client, containers []container.Summary) error {
	targets := make([]containerImageUpdateTarget, 0, len(containers))
	for _, summary := range containers {
		targets = append(targets, containerImageUpdateTarget{
			ContainerID:   summary.ID,
			ContainerName: primaryContainerName(summary),
			ImageID:       summary.ImageID,
			ImageRef:      summary.Image,
		})
	}
	statuses, result, err := checkContainerImageUpdates(ctx, cli, targets, time.Now())
	if err != nil {
		return err
	}
	if err := mergeUpdateStatuses(ctx, statuses); err != nil {
		return err
	}
	slog.Info("scheduled Docker image check complete",
		"component", "docker",
		"subsystem", "update",
		"checked", result.Checked,
		"updates", result.Updates,
		"uncheckable", result.Uncheckable,
		"errors", result.Errors)
	return nil
}

func runScheduledUpdates(ctx context.Context, cli *client.Client, summaries, all []container.Summary, opts apischema.DockerContainerAutoUpdateOptions) error {
	state := newScheduledUpdateState(ctx, cli)
	state.allContainers = all
	state.updateStopped = opts.UpdateStopped
	state.reviveStopped = opts.ReviveStopped
	for _, summary := range summaries {
		if err := state.prepare(summary); err != nil {
			return err
		}
	}
	if err := state.applyComposeGroups(); err != nil {
		return err
	}
	if opts.Cleanup {
		if err := cleanupUnusedUpdateImages(ctx, cli, state.oldImageIDs); err != nil {
			state.errs = append(state.errs, err)
		}
	}
	return errors.Join(state.errs...)
}

func composeScheduleKey(target composeProjectTarget) string {
	return target.Name + "\x00" + target.WorkingDir + "\x00" + strings.Join(target.ConfigFiles, "\x00")
}

func scheduledUpdateError(ctx context.Context, inspect container.InspectResponse, err error) error {
	status := imageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckState:    apischema.DockerUpdateCheckStateError,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
		Err:           err.Error(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	if mergeErr := mergeUpdateStatuses(ctx, []imageUpdateStatus{status}); mergeErr != nil {
		return errors.Join(err, mergeErr)
	}
	return err
}

type scheduledImageCleanupClient interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ImageRemove(context.Context, string, client.ImageRemoveOptions) (client.ImageRemoveResult, error)
}

func cleanupUnusedUpdateImages(ctx context.Context, cli scheduledImageCleanupClient, imageIDs []string) error {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers before old-image cleanup: %w", err)
	}
	inUse := make(map[string]struct{}, len(containers.Items))
	for _, summary := range containers.Items {
		inUse[summary.ImageID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(imageIDs))
	var cleanupErrs []error
	for _, imageID := range imageIDs {
		if imageID == "" {
			continue
		}
		if _, ok := seen[imageID]; ok {
			continue
		}
		seen[imageID] = struct{}{}
		if _, ok := inUse[imageID]; ok {
			continue
		}
		if _, err := cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{PruneChildren: true}); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove unused old image %q: %w", imageID, err))
		}
	}
	return errors.Join(cleanupErrs...)
}
