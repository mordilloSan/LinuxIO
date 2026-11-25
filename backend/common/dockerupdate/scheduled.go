package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
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
	// project right before it updates it, and returns the containers it
	// replaced or failed to.
	update func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(name string)) ([]ContainerOutcome, error)
	// freeSpace refuses the updates when Docker's disk has less than the
	// given GiB free (checkDockerFreeSpace). It is asked only when the
	// settings set a minimum and the pass has containers to update.
	freeSpace func(ctx context.Context, minGiB int) error
}

// RunScheduled executes one configured check or update pass. It is called by
// the short-lived Docker updater; no updater daemon remains resident
// between timer activations. report, when not nil, is told what the pass is
// doing. The returned report says what the pass did, also when it failed. A
// failure of one container raised that container's alert and is marked
// scheduledrun.Reported in the error; a pass that found another update
// running did no work and returns a skipped report and no error.
func RunScheduled(ctx context.Context, configPath string, report func(message string)) (PassReport, error) {
	store := defaultContainerAutoUpdateStore
	if strings.TrimSpace(configPath) != "" {
		store.configPath = configPath
	}
	opts, err := store.readOptions()
	if err != nil {
		return PassReport{}, err
	}

	release, busy, err := acquirePassLock(ctx)
	if busy {
		return PassReport{Skipped: passSkippedBusy}, nil
	}
	if err != nil {
		return PassReport{}, err
	}
	defer release()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return PassReport{}, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()
	if err := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); err != nil {
		return PassReport{}, fmt.Errorf("recover previous standalone Docker update: %w", err)
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
			return runScheduledUpdateCheck(ctx, registry, classifyComposeBuilt(cli), summaries, notify.DockerUpdatesAvailable)
		},
		update: func(ctx context.Context, summaries, all []container.Summary, opts DockerContainerAutoUpdateOptions, announce func(string)) ([]ContainerOutcome, error) {
			return runScheduledUpdates(ctx, cli, registry, summaries, all, opts, alertReport, announce)
		},
		freeSpace: func(ctx context.Context, minGiB int) error {
			return checkDockerFreeSpace(ctx, cli, minGiB)
		},
	}, report)
}

// containerdRoot is where the containerd that Docker talks to keeps its data
// unless its own configuration moves it. With the containerd image store,
// the default of a new Docker 29 install, the images' layers are kept there
// and not under Docker's root folder (Docker 29.1.3 on Ubuntu has its
// snapshotters there and none under /var/lib/docker).
const containerdRoot = "/var/lib/containerd"

type dockerInfoClient interface {
	Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error)
}

// freeBytesAt is the room the filesystem of path has for a write; swapped by
// tests.
var freeBytesAt = func(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// checkDockerFreeSpace returns an error naming the folder when the filesystem
// of Docker's root folder, or of containerdRoot, has less than minGiB GiB
// free, before a pass pulls anything onto it. A containerd folder that does
// not exist holds nothing and is not asked.
func checkDockerFreeSpace(ctx context.Context, cli dockerInfoClient, minGiB int) error {
	info, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return fmt.Errorf("read Docker's root folder: %w", err)
	}
	minimum := uint64(minGiB) << 30
	for _, folder := range []string{info.Info.DockerRootDir, containerdRoot} {
		if folder == "" {
			continue
		}
		free, err := freeBytesAt(folder)
		if folder == containerdRoot && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read the free space of %s: %w", folder, err)
		}
		if free < minimum {
			return fmt.Errorf("no container was updated: the filesystem of %s has %.1f GiB free, less than the %d GiB the settings ask for",
				folder, float64(free)/(1<<30), minGiB)
		}
	}
	return nil
}

// acquirePassLock takes the update lock for a scheduled pass. busy is true
// for a pass that finds another update holding it: it did no work and failed
// nothing.
func acquirePassLock(ctx context.Context) (release func(), busy bool, err error) {
	release, err = acquireLock(ctx)
	if errors.Is(err, errUpdateInProgress) {
		return nil, true, nil
	}
	return release, false, err
}

// acquireLock is swapped by tests.
var acquireLock = AcquireLock

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
// passNotice decides on. It returns what the pass did, also when it failed.
func runScheduledPass(ctx context.Context, opts DockerContainerAutoUpdateOptions, notify alerts.NotificationSettings, operations scheduledPassOperations, report func(message string)) (PassReport, error) {
	if report == nil {
		report = func(string) {}
	}
	containers, err := operations.list(ctx)
	if err != nil {
		return PassReport{}, fmt.Errorf("list scheduled Docker update targets: %w", err)
	}
	var pass PassReport
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
	if checkErr == nil {
		pass.Mode, pass.Checked = opts.Mode, checked
	}
	if opts.Mode == "check_only" {
		raisePassNotice(ctx, notify, checkErr, checked, 0)
		return pass, checkErr
	}
	var updateErr error
	pass.Containers, updateErr = updateSelected(ctx, opts, operations, selected, containers, report)
	updated, _, _ := pass.counts()
	err = errors.Join(append(runErrs, checkErr, updateErr)...)
	raisePassNotice(ctx, notify, err, checked, updated)
	return pass, err
}

// updateSelected is the update step of a pass in update mode: it updates
// selected, once the free space the settings ask for is there.
func updateSelected(ctx context.Context, opts DockerContainerAutoUpdateOptions, operations scheduledPassOperations, selected, all []container.Summary, report func(message string)) ([]ContainerOutcome, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	if opts.MinFreeGiB > 0 {
		// No container alert names this, so it fails the run.
		if err := operations.freeSpace(ctx, opts.MinFreeGiB); err != nil {
			return nil, err
		}
	}
	announce := func(name string) { report("Updating " + name) }
	return operations.update(ctx, selected, all, opts, announce)
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
func runScheduledUpdateCheck(ctx context.Context, cli imageUpdateCheckClient, classify builtClassifier, containers []container.Summary, reportAvailable bool) (DockerUpdateCheckResult, error) {
	targets := make([]containerImageUpdateTarget, 0, len(containers))
	for _, summary := range containers {
		targets = append(targets, containerImageUpdateTarget{
			ContainerID:   summary.ID,
			ContainerName: PrimaryContainerName(summary),
			ImageID:       summary.ImageID,
			ImageRef:      summary.Image,
			Labels:        summary.Labels,
		})
	}
	statuses, result, err := checkContainerImageUpdates(ctx, cli, classify, targets, time.Now())
	if err != nil {
		return DockerUpdateCheckResult{}, err
	}
	if err := mergeUpdateStatuses(ctx, statuses); err != nil {
		return DockerUpdateCheckResult{}, err
	}
	syncAvailableAlerts(ctx, statuses, reportAvailable)
	syncCheckAlerts(ctx, statuses)
	slog.Debug("scheduled Docker image check complete",
		"component", "docker",
		"subsystem", "update",
		"checked", result.Checked,
		"updates", result.Updates,
		"uncheckable", result.Uncheckable,
		"errors", result.Errors)
	return result, nil
}

// runScheduledUpdates updates summaries and returns the containers it
// replaced or failed to. alertReport is the Docker updates' alerts.ReportFor.
func runScheduledUpdates(ctx context.Context, cli *client.Client, registry imageUpdateCheckClient, summaries, all []container.Summary, opts DockerContainerAutoUpdateOptions, alertReport string, announce func(string)) ([]ContainerOutcome, error) {
	state := newScheduledUpdateState(ctx, cli)
	state.announce = announce
	state.alertReport = alertReport
	state.registry = registry
	state.allContainers = all
	state.updateStopped = opts.UpdateStopped
	state.reviveStopped = opts.ReviveStopped
	for _, summary := range summaries {
		if err := state.prepare(summary); err != nil {
			return state.outcomes, err
		}
	}
	if err := state.applyComposeGroups(); err != nil {
		return state.outcomes, err
	}
	if opts.Cleanup {
		if err := cleanupUnusedUpdateImages(ctx, cli, state.oldImageIDs); err != nil {
			state.errs = append(state.errs, err)
		}
	}
	return state.outcomes, errors.Join(state.errs...)
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
	raiseDockerUpdateAlert(ctx, inspect.Name, status.ImageRef, err)
	// The container's alert reports it; a status file that cannot be written
	// is the pass's own failure, joined unmarked.
	err = scheduledrun.Reported(err)
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
