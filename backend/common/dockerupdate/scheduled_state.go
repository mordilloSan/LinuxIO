package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

type scheduledUpdateState struct {
	ctx           context.Context
	cli           *client.Client
	registry      imageUpdateCheckClient
	classify      builtClassifier // tells the containers a Compose service builds
	allContainers []container.Summary
	dependencyCli ContainerClient
	readiness     containerReadinessClient // inspects a replaced Compose container
	rollback      composeRollback          // of a Compose service that is not ready
	dependencies  *standaloneDependencyIndex
	dependencyErr error
	dependencySet bool
	composeGroups map[string]*scheduledComposeTarget
	oldImageIDs   []string
	errs          []error
	updateStopped bool
	reviveStopped bool
	composeUpdate func(context.Context, ComposeTarget, []string, ComposeLineEmitter) error
	announce      func(name string)
	alertReport   string // the Docker updates' alerts.ReportFor
	// outcomes is each container this pass replaced or failed to, in the
	// order it happened, for the pass report.
	outcomes []ContainerOutcome
	// imageVersion reads the version label of an image by its ID, empty when
	// it has none; nil where no engine is at hand.
	imageVersion func(imageID string) string
}

func newScheduledUpdateState(ctx context.Context, cli *client.Client) *scheduledUpdateState {
	state := &scheduledUpdateState{
		ctx:           ctx,
		cli:           cli,
		registry:      cli,
		classify:      classifyComposeBuilt(cli),
		dependencyCli: cli,
		readiness:     cli,
		rollback:      newComposeRollback(cli),
		composeGroups: make(map[string]*scheduledComposeTarget),
		composeUpdate: composePullAndUpServices,
	}
	if cli != nil {
		state.imageVersion = func(imageID string) string { return imageVersionOf(ctx, cli, imageID) }
	}
	return state
}

type imageInspector interface {
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
}

// imageVersionOf reads the version label of an image; empty when it has none
// or cannot be read, which only shortens what the pass reports.
func imageVersionOf(ctx context.Context, cli imageInspector, imageID string) string {
	if imageID == "" {
		return ""
	}
	result, err := cli.ImageInspect(ctx, imageID)
	if err != nil {
		slog.Debug("failed to read the version of a Docker image",
			"component", "docker",
			"subsystem", "update",
			"image", imageID,
			"error", err)
		return ""
	}
	if result.Config == nil {
		return ""
	}
	return result.Config.Labels[imageVersionLabel]
}

// replaced is the outcome of a container the pass replaced: its images and,
// while the old one is still on the host (the cleanup runs after the last
// update), their versions.
func (s *scheduledUpdateState) replaced(before container.InspectResponse, newImageID string) ContainerOutcome {
	outcome := outcomeBefore(before)
	outcome.NewImageID = newImageID
	if s.imageVersion != nil {
		outcome.OldVersion, outcome.NewVersion = s.imageVersion(outcome.OldImageID), s.imageVersion(newImageID)
	}
	return outcome
}

func (s *scheduledUpdateState) prepare(summary container.Summary) error {
	candidate, err := inspectScheduledUpdateCandidate(s.ctx, s.cli, s.registry, s.classify, summary)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordCandidateError(candidate.inspect, err)
		return nil
	}
	if !candidate.needsUpdate {
		return nil
	}
	stopped, eligible := s.prepareCandidateState(candidate.inspect)
	if !eligible {
		return nil
	}

	target, service, managedByCompose, err := composeTargetForContainer(s.ctx, s.cli, candidate.inspect)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordCandidateError(candidate.inspect, err)
		return nil
	}
	if managedByCompose {
		if stopped {
			s.recordCandidateError(candidate.inspect, errors.New("stopped Compose services cannot be updated safely without changing their lifecycle state"))
			return nil
		}
		if scopeErr := validateComposeServiceScope(s.ctx, s.cli, target.Name, service); scopeErr != nil {
			s.recordCandidateError(candidate.inspect, scopeErr)
			return nil
		}
		s.addComposeCandidate(target, service, candidate.inspect)
		return nil
	}

	dependencies, err := s.standaloneDependencies()
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordCandidateError(candidate.inspect, err)
		return nil
	}
	s.announceUpdate(strings.TrimPrefix(candidate.inspect.Name, "/"))
	updated, err := updateStandaloneContainer(
		s.ctx,
		s.cli,
		candidate.inspect,
		candidate.normalizedRef,
		candidate.result,
		&defaultStandaloneUpdateJournal,
		dependencies,
		stoppedContainerUpdatePolicy{Allow: s.updateStopped, Revive: s.reviveStopped},
		nil,
	)
	return s.recordStandaloneUpdateOutcome(candidate.inspect, updated, err)
}

func (s *scheduledUpdateState) announceUpdate(name string) {
	if s.announce != nil {
		s.announce(name)
	}
}

func (s *scheduledUpdateState) prepareCandidateState(inspect container.InspectResponse) (bool, bool) {
	stopped := isStoppedContainer(inspect)
	if stopped && !s.updateStopped {
		skipped, err := skipStoppedScheduledContainer(s.ctx, inspect)
		if err != nil {
			s.recordCandidateError(inspect, err)
		}
		if skipped {
			return stopped, false
		}
	}
	if err := validateContainerUpdateState(inspect, s.updateStopped); err != nil {
		s.recordCandidateError(inspect, err)
		return stopped, false
	}
	return stopped, true
}

func isStoppedContainer(inspect container.InspectResponse) bool {
	return inspect.State != nil &&
		!inspect.State.Running &&
		!inspect.State.Paused &&
		!inspect.State.Restarting &&
		strings.EqualFold(string(inspect.State.Status), string(container.StateExited))
}

func skipStoppedScheduledContainer(ctx context.Context, inspect container.InspectResponse) (bool, error) {
	if inspect.State == nil || inspect.State.Running || inspect.State.Restarting {
		return false, nil
	}
	reason := "Automatic update skipped because the container is stopped; start it or use check-only scheduling."
	if err := markContainerUpdateDeferred(ctx, inspect, reason); err != nil {
		return true, err
	}
	name := strings.TrimPrefix(inspect.Name, "/")
	slog.Debug("skipped automatic update for stopped Docker container "+name,
		"component", "docker",
		"subsystem", "update",
		"container", name)
	return true, nil
}

func (s *scheduledUpdateState) standaloneDependencies() (*standaloneDependencyIndex, error) {
	if !s.dependencySet {
		s.dependencies, s.dependencyErr = buildStandaloneDependencyIndex(s.ctx, s.dependencyCli, s.allContainers)
		s.dependencySet = true
	}
	return s.dependencies, s.dependencyErr
}

func (s *scheduledUpdateState) recordStandaloneUpdateOutcome(
	before container.InspectResponse,
	updated DockerContainerUpdateResult,
	updateErr error,
) error {
	if updated.Updated {
		outcome := s.replaced(before, updated.NewImageID)
		s.outcomes = append(s.outcomes, outcome)
		s.oldImageIDs = append(s.oldImageIDs, updated.PreviousImageID)
		slog.Info("updated standalone Docker container "+updated.ContainerName,
			"component", "docker",
			"subsystem", "update",
			"container", updated.ContainerName,
			"image", outcome.Image,
			"old_image", updated.PreviousImageID,
			"new_image", updated.NewImageID,
			"old_version", outcome.OldVersion,
			"new_version", outcome.NewVersion)
		raiseDockerUpdatedAlert(s.ctx, s.alertReport, outcome)
	}
	if updateErr != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if updated.Updated {
			s.errs = append(s.errs, updateErr)
			slog.Warn("failed to remove the rollback container of updated standalone Docker container "+updated.ContainerName,
				"component", "docker",
				"subsystem", "update",
				"container", updated.ContainerName,
				"error", updateErr)
			return nil
		}
		s.recordCandidateError(before, updateErr)
		return nil
	}
	return nil
}

func inspectScheduledUpdateCandidate(
	ctx context.Context,
	cli *client.Client,
	registry imageUpdateCheckClient,
	classify builtClassifier,
	summary container.Summary,
) (containerUpdateCandidate, error) {
	inspectResult, err := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return containerUpdateCandidate{}, fmt.Errorf("inspect scheduled container %q: %w", PrimaryContainerName(summary), err)
	}
	return inspectContainerUpdateCandidate(ctx, registry, classify, inspectResult.Container)
}

func (s *scheduledUpdateState) recordCandidateError(inspect container.InspectResponse, err error) {
	if inspect.ID != "" {
		s.recordFailedOutcome(inspect, err)
		err = scheduledUpdateError(s.ctx, inspect, err)
	}
	s.errs = append(s.errs, err)
}

// recordFailedOutcome adds a container whose update failed, and whose alert
// reports it, to the pass report.
func (s *scheduledUpdateState) recordFailedOutcome(inspect container.InspectResponse, err error) {
	outcome := outcomeBefore(inspect)
	outcome.Err = err
	s.outcomes = append(s.outcomes, outcome)
}

func (s *scheduledUpdateState) addComposeCandidate(target ComposeTarget, service string, inspect container.InspectResponse) {
	key := composeScheduleKey(target)
	group := s.composeGroups[key]
	if group == nil {
		group = &scheduledComposeTarget{target: target}
		s.composeGroups[key] = group
	}
	group.services = append(group.services, service)
	group.before = append(group.before, inspect)
}

func (s *scheduledUpdateState) applyComposeGroups() error {
	for _, group := range s.composeGroups {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if err := s.applyComposeGroup(group); err != nil {
			return err
		}
	}
	return nil
}

func (s *scheduledUpdateState) applyComposeGroup(group *scheduledComposeTarget) error {
	valid, err := s.validateComposeGroup(group)
	if err != nil {
		return err
	}
	if !valid {
		return nil
	}
	updated, err := s.runComposeUpdate(group)
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	return s.verifyComposeGroup(group)
}

func (s *scheduledUpdateState) validateComposeGroup(group *scheduledComposeTarget) (bool, error) {
	if s.cli == nil {
		return true, nil
	}
	if err := validateComposeServiceScopes(s.ctx, s.cli, group.target.Name, group.services); err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		s.recordComposeGroupError(group, err)
		return false, nil
	}
	return true, nil
}

func (s *scheduledUpdateState) runComposeUpdate(group *scheduledComposeTarget) (bool, error) {
	s.announceUpdate(group.target.Name)
	collector := &ComposeMessageCollector{}
	if err := s.composeUpdate(s.ctx, group.target, group.services, collector.Emit); err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		groupErr := fmt.Errorf("update Compose project %q: %w: %s", group.target.Name, err, collector.String())
		s.recordComposeGroupError(group, groupErr)
		return false, nil
	}
	return true, nil
}

func (s *scheduledUpdateState) recordComposeGroupError(group *scheduledComposeTarget, err error) {
	// Each container's alert reports the group's failure; what else went
	// wrong while recording it, such as a status file that cannot be written,
	// stays unmarked beside it.
	for _, inspect := range group.before {
		s.recordFailedOutcome(inspect, err)
		if recorded := scheduledUpdateError(s.ctx, inspect, err); !scheduledrun.AllReported(recorded) {
			s.errs = append(s.errs, fmt.Errorf("record the failed update of %s: %w", strings.TrimPrefix(inspect.Name, "/"), scheduledrun.Unreported(recorded)))
		}
	}
	if len(group.before) > 0 {
		err = scheduledrun.Reported(err)
	}
	s.errs = append(s.errs, err)
}

// verifyComposeGroup waits for each updated container of group to be ready,
// rolls back the images of those that are not, and records each outcome. A
// pass stopped while it waits records the containers verified so far and
// rolls back nothing.
func (s *scheduledUpdateState) verifyComposeGroup(group *scheduledComposeTarget) error {
	afters := make([]container.InspectResponse, 0, len(group.before))
	failures := make([]error, 0, len(group.before))
	var stopped error
	for _, before := range group.before {
		after, err := verifyScheduledComposeContainer(s.ctx, s.readiness, before)
		if err != nil && s.ctx.Err() != nil {
			stopped = s.ctx.Err()
			break
		}
		afters = append(afters, after)
		failures = append(failures, err)
	}
	if stopped == nil {
		s.rollBackComposeFailures(group, failures)
	}
	for i, failure := range failures {
		before, after := group.before[i], afters[i]
		if failure != nil {
			s.recordCandidateError(before, failure)
			continue
		}
		markContainerCurrent(s.ctx, before.ID, after)
		if after.Image == before.Image {
			continue
		}
		outcome := s.replaced(before, after.Image)
		s.outcomes = append(s.outcomes, outcome)
		s.oldImageIDs = append(s.oldImageIDs, before.Image)
		slog.Info("updated Compose Docker container "+outcome.Name,
			"component", "docker",
			"subsystem", "update",
			"project", group.target.Name,
			"container", outcome.Name,
			"image", outcome.Image,
			"old_image", before.Image,
			"new_image", after.Image,
			"old_version", outcome.OldVersion,
			"new_version", outcome.NewVersion)
		raiseDockerUpdatedAlert(s.ctx, s.alertReport, outcome)
	}
	return stopped
}

// rollBackComposeFailures rolls back each tag a service of group failed on
// (exited, turned unhealthy or kept restarting). The services of the group
// under that tag, however each spells it, go back together, the ready ones
// included: one left on the new image while the tag names the old one would
// be moved back by its next `up`. What each tag names is read before any
// rollback moves one. When the services under a tag ran different images,
// one tag cannot take them all back, and none is rolled back; neither is a
// service that was still coming up when its wait ended (errStillComingUp).
// failures holds the verify error of each service of group, nil for one that
// became ready; each failed service, and each ready one a rollback moved,
// gets its outcome. A pass stopped meanwhile starts no further rollback.
func (s *scheduledUpdateState) rollBackComposeFailures(group *scheduledComposeTarget, failures []error) {
	tags, members := composeTags(group)
	type tagged struct {
		first   int // the first service that failed on the tag
		imageID string
		err     error
	}
	failedTags := make(map[string]tagged)
	for _, ref := range tags {
		k := slices.IndexFunc(members[ref], func(i int) bool { return errors.As(failures[i], new(containerFailedError)) })
		if k >= 0 {
			imageID, err := s.rollback.taggedImage(s.ctx, ref)
			failedTags[ref] = tagged{first: members[ref][k], imageID: imageID, err: err}
		}
	}
	for _, ref := range tags {
		tag, failed := failedTags[ref]
		switch {
		case !failed:
			for _, i := range members[ref] {
				if failures[i] != nil {
					failures[i] = composeUpdateFailure(failures[i], "", rollbackRefusal(failures[i]))
				}
			}
		case s.ctx.Err() == nil:
			s.rollBackTag(group, failures, ref, members[ref], tag.first, tag.imageID, tag.err)
		}
	}
}

// composeTags groups the services of group by the tag each runs, normalised,
// in the group's order.
func composeTags(group *scheduledComposeTarget) ([]string, map[string][]int) {
	var tags []string
	members := make(map[string][]int)
	for i, before := range group.before {
		ref := normalizedReference(configImage(before))
		if _, seen := members[ref]; !seen {
			tags = append(tags, ref)
		}
		members[ref] = append(members[ref], i)
	}
	return tags, members
}

// rollBackTag rolls back the services of group under the tag ref (members),
// of which first failed, and gives each its outcome in failures.
// pulledImageID is what the tag named before any rollback, or pulledErr why
// it could not be read.
func (s *scheduledUpdateState) rollBackTag(group *scheduledComposeTarget, failures []error, ref string, members []int, first int, pulledImageID string, pulledErr error) {
	oldImageID, firstFailure := group.before[first].Image, failures[first]
	var images []string
	var services []composeRollbackService
	blocker := -1 // a service under the tag that did not fail itself
	for _, i := range members {
		before := group.before[i]
		if !slices.Contains(images, shortImageID(before.Image)) {
			images = append(images, shortImageID(before.Image))
		}
		services = append(services, composeRollbackService{Service: group.services[i], Container: strings.TrimPrefix(before.Name, "/")})
		if blocker < 0 && failures[i] != nil && rollbackRefusal(failures[i]) != nil {
			blocker = i
		}
	}
	changed, rollbackErr := false, pulledErr
	switch {
	case rollbackErr != nil:
	case blocker >= 0:
		// It may be changing its data, or how it runs is unknown: the old
		// image must not be started on it.
		rollbackErr = fmt.Errorf("%w: %s under the same tag had not failed when its wait ended, and may be changing its data", errNotRolledBack, strings.TrimPrefix(group.before[blocker].Name, "/"))
	case len(images) > 1:
		rollbackErr = fmt.Errorf("%w: the services under tag %q ran different images (%s)", errNotRolledBack, ref, strings.Join(images, ", "))
	default:
		changed, rollbackErr = s.rollback.run(s.ctx, group.target, ref, oldImageID, pulledImageID, services)
	}
	for _, i := range members {
		cause := failures[i]
		if cause == nil {
			if !changed {
				continue // still on the new image, where it was ready
			}
			cause = fmt.Errorf("%s runs the same image: %w", strings.TrimPrefix(group.before[first].Name, "/"), firstFailure)
		}
		reason := rollbackErr
		if i == blocker {
			reason = rollbackRefusal(cause)
		}
		failures[i] = composeUpdateFailure(cause, oldImageID, reason)
	}
}

func verifyScheduledComposeContainer(
	ctx context.Context,
	cli containerReadinessClient,
	before container.InspectResponse,
) (container.InspectResponse, error) {
	name := strings.TrimPrefix(before.Name, "/")
	after, err := inspectAndWaitForComposeContainer(ctx, cli, name)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("verify updated Compose container %q: %w", name, err)
	}
	return after, nil
}
