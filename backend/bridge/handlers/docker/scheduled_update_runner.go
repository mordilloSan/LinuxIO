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
)

type scheduledComposeTarget struct {
	target   composeProjectTarget
	services []string
	before   []container.InspectResponse
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
	if len(opts.ContainerNames) == 0 {
		slog.Info("Docker update schedule has no selected containers", "component", "docker-update")
		return nil
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

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list scheduled Docker update targets: %w", err)
	}
	targetByName := make(map[string]container.Summary, len(containers.Items))
	for _, summary := range containers.Items {
		if name := primaryContainerName(summary); name != "" {
			targetByName[name] = summary
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

	if opts.Mode == "check_only" {
		checkErr := runScheduledUpdateCheck(ctx, cli, selected)
		return errors.Join(append(runErrs, checkErr)...)
	}
	updateErr := runScheduledUpdates(ctx, cli, selected, opts.Cleanup)
	return errors.Join(append(runErrs, updateErr)...)
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
		"component", "docker-update",
		"checked", result.Checked,
		"updates", result.Updates,
		"errors", result.Errors)
	return nil
}

func runScheduledUpdates(ctx context.Context, cli *client.Client, summaries []container.Summary, cleanup bool) error {
	state := newScheduledUpdateState(ctx, cli)
	for _, summary := range summaries {
		if err := state.prepare(summary); err != nil {
			return err
		}
	}
	if err := state.applyComposeGroups(); err != nil {
		return err
	}
	if cleanup {
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

func cleanupUnusedUpdateImages(ctx context.Context, cli *client.Client, imageIDs []string) error {
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
