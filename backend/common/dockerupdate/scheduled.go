package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

type scheduledComposeTarget struct {
	target   ComposeTarget
	services []string
	before   []container.InspectResponse
}

type scheduledPassOperations struct {
	list func(context.Context) ([]container.Summary, error)
	// check returns what it found: how many containers it checked, how many
	// have a newer image and how many it could not check.
	check func(context.Context, []container.Summary) (DockerUpdateCheckResult, error)
	// update calls announce with the name of each container or Compose
	// project right before it updates it, and returns how many containers it
	// replaced.
	update func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(name string)) (int, error)
}

// RunScheduled executes one configured check or update pass. It is called by
// the short-lived Docker update worker; no updater daemon remains resident
// between timer activations. report, when not nil, is told what the pass is
// doing.
func RunScheduled(ctx context.Context, configPath string, report func(message string)) error {
	store := defaultContainerAutoUpdateStore
	if strings.TrimSpace(configPath) != "" {
		store.configPath = configPath
	}
	opts, err := store.readOptions()
	if err != nil {
		return err
	}

	release, err := AcquireLock(ctx)
	if err != nil {
		return err
	}
	defer release()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()
	if err := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); err != nil {
		return fmt.Errorf("recover previous standalone Docker update: %w", err)
	}

	// Read once, so the whole pass follows one setting.
	notify := alerts.NotificationsOrDefaults()
	alertReport := notify.ReportFor(alerts.KindDockerUpdates)
	registry := newRegistryLookupCache(cli)
	return runScheduledPass(ctx, opts, notify, scheduledPassOperations{
		list: func(ctx context.Context) ([]container.Summary, error) {
			containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
			return containers.Items, err
		},
		check: func(ctx context.Context, summaries []container.Summary) (DockerUpdateCheckResult, error) {
			return runScheduledUpdateCheck(ctx, registry, summaries, notify.DockerUpdatesAvailable)
		},
		update: func(ctx context.Context, summaries, all []container.Summary, opts DockerContainerAutoUpdateOptions, announce func(string)) (int, error) {
			return runScheduledUpdates(ctx, cli, registry, summaries, all, opts, alertReport, announce)
		},
	}, report)
}

// registryLookupCache keeps successful registry lookups for one scheduled run,
// so the update pass reuses what the check pass fetched instead of asking the
// registry again. Failed lookups are not kept: the update pass tries them again.
type registryLookupCache struct {
	imageUpdateCheckClient
	mu      sync.Mutex
	results map[string]client.DistributionInspectResult
}

func newRegistryLookupCache(cli imageUpdateCheckClient) *registryLookupCache {
	return &registryLookupCache{imageUpdateCheckClient: cli, results: make(map[string]client.DistributionInspectResult)}
}

func (c *registryLookupCache) DistributionInspect(
	ctx context.Context,
	imageRef string,
	options client.DistributionInspectOptions,
) (client.DistributionInspectResult, error) {
	c.mu.Lock()
	result, ok := c.results[imageRef]
	c.mu.Unlock()
	if ok {
		return result, nil
	}
	result, err := c.imageUpdateCheckClient.DistributionInspect(ctx, imageRef, options)
	if err != nil {
		return result, err
	}
	c.mu.Lock()
	c.results[imageRef] = result
	c.mu.Unlock()
	return result, nil
}

// runScheduledPass runs one pass with operations under notify, the
// notification settings read for this pass, and raises the pass notice that
// passNotice decides on.
func runScheduledPass(ctx context.Context, opts DockerContainerAutoUpdateOptions, notify alerts.NotificationSettings, operations scheduledPassOperations, report func(message string)) error {
	if report == nil {
		report = func(string) {}
	}
	containers, err := operations.list(ctx)
	if err != nil {
		return fmt.Errorf("list scheduled Docker update targets: %w", err)
	}
	targetByName := make(map[string]container.Summary, len(containers))
	for _, summary := range containers {
		if name := PrimaryContainerName(summary); name != "" {
			targetByName[name] = summary
		}
	}
	running := make([]container.Summary, 0, len(containers))
	for _, summary := range containers {
		if summary.State == container.StateRunning {
			running = append(running, summary)
		}
	}

	selected, runErrs := scheduledUpdateSelection(opts, containers, targetByName)

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
	report("Checking for updates")
	checked, checkErr := operations.check(ctx, checkTargets)
	if opts.Mode == "check_only" {
		raisePassNotice(ctx, notify, checkErr, checked, 0)
		return checkErr
	}
	updated := 0
	var updateErr error
	if len(selected) > 0 {
		announce := func(name string) { report("Updating " + name) }
		updated, updateErr = operations.update(ctx, selected, containers, opts, announce)
	}
	err = errors.Join(append(runErrs, checkErr, updateErr)...)
	raisePassNotice(ctx, notify, err, checked, updated)
	return err
}

// raisePassNotice raises the pass notice when passNotice decides on one.
func raisePassNotice(ctx context.Context, notify alerts.NotificationSettings, passErr error, checked DockerUpdateCheckResult, updated int) {
	if waiting, ok := passNotice(notify, passErr, checked, updated); ok {
		raiseDockerPassAlert(ctx, time.Now(), waiting)
	}
}

// passNotice decides whether a pass raises the every_run pass notice, and how
// many waiting updates it names (none is "nothing to update"). passErr is
// what the pass returns, checked what its check found, and updated how many
// containers it replaced (none in check-only mode). A failed pass raised its
// alerts and a replaced container its own notice; a check that could not
// check every container (an unreachable registry) cannot say what is
// waiting. Any other pass names what waits, an updating one that replaced
// nothing as a check-only one does, since updates can wait for containers it
// is not set to update; when the "updates are waiting" alerts report them it
// raises nothing more.
func passNotice(notify alerts.NotificationSettings, passErr error, checked DockerUpdateCheckResult, updated int) (waiting int, raise bool) {
	switch {
	case passErr != nil, checked.Errors > 0, notify.ReportFor(alerts.KindDockerUpdates) != alerts.ReportEveryRun:
		return 0, false
	case updated > 0:
		return 0, false
	case checked.Updates > 0 && notify.DockerUpdatesAvailable:
		return 0, false
	}
	return checked.Updates, true
}

func scheduledUpdateSelection(opts DockerContainerAutoUpdateOptions, containers []container.Summary, targetByName map[string]container.Summary) ([]container.Summary, []error) {
	selected := make([]container.Summary, 0, len(opts.ContainerNames))
	if opts.UpdateAll {
		// Containers that cannot be updated safely are skipped, not failed.
		replicas := composeReplicaCounts(containers)
		for _, summary := range containers {
			if eligible, _ := containerMutationEligibility(summary, replicas); eligible &&
				(summary.State == container.StateRunning || opts.UpdateStopped) {
				selected = append(selected, summary)
			}
		}
		return selected, nil
	}
	var errs []error
	for _, name := range opts.ContainerNames {
		summary, ok := targetByName[name]
		if !ok {
			errs = append(errs, fmt.Errorf("scheduled container %q no longer exists", name))
			continue
		}
		selected = append(selected, summary)
	}
	return selected, errs
}

// runScheduledUpdateCheck checks containers and returns what it found.
func runScheduledUpdateCheck(ctx context.Context, cli imageUpdateCheckClient, containers []container.Summary, reportAvailable bool) (DockerUpdateCheckResult, error) {
	targets := make([]containerImageUpdateTarget, 0, len(containers))
	for _, summary := range containers {
		targets = append(targets, containerImageUpdateTarget{
			ContainerID:   summary.ID,
			ContainerName: PrimaryContainerName(summary),
			ImageID:       summary.ImageID,
			ImageRef:      summary.Image,
		})
	}
	statuses, result, err := checkContainerImageUpdates(ctx, cli, targets, time.Now())
	if err != nil {
		return DockerUpdateCheckResult{}, err
	}
	if err := mergeUpdateStatuses(ctx, statuses); err != nil {
		return DockerUpdateCheckResult{}, err
	}
	syncAvailableAlerts(ctx, statuses, reportAvailable)
	slog.Debug("scheduled Docker image check complete",
		"component", "docker",
		"subsystem", "update",
		"checked", result.Checked,
		"updates", result.Updates,
		"uncheckable", result.Uncheckable,
		"errors", result.Errors)
	return result, nil
}

// runScheduledUpdates updates summaries and returns how many containers it
// replaced. alertReport is the Docker updates' alerts.ReportFor.
func runScheduledUpdates(ctx context.Context, cli *client.Client, registry imageUpdateCheckClient, summaries, all []container.Summary, opts DockerContainerAutoUpdateOptions, alertReport string, announce func(string)) (int, error) {
	state := newScheduledUpdateState(ctx, cli)
	state.announce = announce
	state.alertReport = alertReport
	state.registry = registry
	state.allContainers = all
	state.updateStopped = opts.UpdateStopped
	state.reviveStopped = opts.ReviveStopped
	for _, summary := range summaries {
		if err := state.prepare(summary); err != nil {
			return state.updated, err
		}
	}
	if err := state.applyComposeGroups(); err != nil {
		return state.updated, err
	}
	if opts.Cleanup {
		if err := cleanupUnusedUpdateImages(ctx, cli, state.oldImageIDs); err != nil {
			state.errs = append(state.errs, err)
		}
	}
	return state.updated, errors.Join(state.errs...)
}

func composeScheduleKey(target ComposeTarget) string {
	return target.Name + "\x00" + target.WorkingDir + "\x00" +
		strings.Join(target.ConfigFiles, "\x00") + "\x00" +
		strings.Join(target.EnvironmentFiles, "\x00")
}

func scheduledUpdateError(ctx context.Context, inspect container.InspectResponse, err error) error {
	status := ImageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckState:    DockerUpdateCheckStateError,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
		Err:           err.Error(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	raiseDockerUpdateAlert(ctx, inspect.Name, err)
	if mergeErr := mergeUpdateStatuses(ctx, []ImageUpdateStatus{status}); mergeErr != nil {
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
