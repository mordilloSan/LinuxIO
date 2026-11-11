package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

type scheduledUpdateCandidate = containerUpdateCandidate

type scheduledUpdateState struct {
	ctx           context.Context
	cli           *client.Client
	registry      imageUpdateCheckClient
	allContainers []container.Summary
	dependencyCli ContainerClient
	readiness     containerReadinessClient // inspects a replaced Compose container
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
	updated       int    // containers this pass replaced
}

func newScheduledUpdateState(ctx context.Context, cli *client.Client) *scheduledUpdateState {
	return &scheduledUpdateState{
		ctx:           ctx,
		cli:           cli,
		registry:      cli,
		dependencyCli: cli,
		readiness:     cli,
		composeGroups: make(map[string]*scheduledComposeTarget),
		composeUpdate: composePullAndUpServices,
	}
}

func (s *scheduledUpdateState) prepare(summary container.Summary) error {
	candidate, err := inspectScheduledUpdateCandidate(s.ctx, s.cli, s.registry, summary)
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
	updated, err := updateStandaloneContainerWithDependenciesAndPolicy(
		s.ctx,
		s.cli,
		candidate.inspect,
		candidate.normalizedRef,
		candidate.result,
		&defaultStandaloneUpdateJournal,
		dependencies,
		stoppedContainerUpdatePolicy{Allow: s.updateStopped, Revive: s.reviveStopped},
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
	slog.Debug("skipped automatic update for stopped Docker container",
		"component", "docker",
		"subsystem", "update",
		"container", strings.TrimPrefix(inspect.Name, "/"))
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
		s.updated++
		s.oldImageIDs = append(s.oldImageIDs, updated.PreviousImageID)
		slog.Info("updated standalone Docker container",
			"component", "docker",
			"subsystem", "update",
			"container", updated.ContainerName,
			"old_image", updated.PreviousImageID,
			"new_image", updated.NewImageID)
		raiseDockerUpdatedAlert(s.ctx, s.alertReport, updated.ContainerName, updated.PreviousImageID, updated.NewImageID)
	}
	if updateErr != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if updated.Updated {
			s.errs = append(s.errs, updateErr)
			slog.Warn("failed to remove standalone Docker rollback container after successful update",
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
	summary container.Summary,
) (scheduledUpdateCandidate, error) {
	inspectResult, err := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return scheduledUpdateCandidate{}, fmt.Errorf("inspect scheduled container %q: %w", PrimaryContainerName(summary), err)
	}
	return inspectContainerUpdateCandidate(ctx, registry, inspectResult.Container)
}

func (s *scheduledUpdateState) recordCandidateError(inspect container.InspectResponse, err error) {
	if inspect.ID != "" {
		err = scheduledUpdateError(s.ctx, inspect, err)
	}
	s.errs = append(s.errs, err)
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
		if recorded := scheduledUpdateError(s.ctx, inspect, err); !scheduledrun.AllReported(recorded) {
			s.errs = append(s.errs, fmt.Errorf("record the failed update of %s: %w", strings.TrimPrefix(inspect.Name, "/"), unreportedPart(recorded)))
		}
	}
	if len(group.before) > 0 {
		err = scheduledrun.Reported(err)
	}
	s.errs = append(s.errs, err)
}

func (s *scheduledUpdateState) verifyComposeGroup(group *scheduledComposeTarget) error {
	for _, before := range group.before {
		after, err := verifyScheduledComposeContainer(s.ctx, s.readiness, before)
		if err != nil {
			if ctxErr := s.ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			s.recordCandidateError(before, err)
			continue
		}
		markContainerCurrent(s.ctx, before.ID, after)
		if after.Image == before.Image {
			continue
		}
		s.updated++
		s.oldImageIDs = append(s.oldImageIDs, before.Image)
		slog.Info("updated Compose Docker container",
			"component", "docker",
			"subsystem", "update",
			"project", group.target.Name,
			"container", strings.TrimPrefix(before.Name, "/"),
			"old_image", before.Image,
			"new_image", after.Image)
		raiseDockerUpdatedAlert(s.ctx, s.alertReport, before.Name, before.Image, after.Image)
	}
	return nil
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

// unreportedPart is what joined err holds besides its reported failures.
func unreportedPart(err error) error {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return err
	}
	var rest []error
	for _, e := range joined.Unwrap() {
		if !scheduledrun.AllReported(e) {
			rest = append(rest, e)
		}
	}
	return errors.Join(rest...)
}
