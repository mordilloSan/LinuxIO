package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type scheduledUpdateCandidate = containerUpdateCandidate

type scheduledUpdateState struct {
	ctx           context.Context
	cli           *client.Client
	allContainers []container.Summary
	dependencyCli nativeContainerUpdateClient
	dependencies  *standaloneDependencyIndex
	dependencyErr error
	dependencySet bool
	composeGroups map[string]*scheduledComposeTarget
	oldImageIDs   []string
	errs          []error
	updateStopped bool
	reviveStopped bool
	composeUpdate func(context.Context, composeProjectTarget, []string, composeLineEmitter) error
}

func newScheduledUpdateState(ctx context.Context, cli *client.Client) *scheduledUpdateState {
	return &scheduledUpdateState{
		ctx:           ctx,
		cli:           cli,
		dependencyCli: cli,
		composeGroups: make(map[string]*scheduledComposeTarget),
		composeUpdate: composePullAndUpServices,
	}
}

func (s *scheduledUpdateState) prepare(summary container.Summary) error {
	candidate, err := inspectScheduledUpdateCandidate(s.ctx, s.cli, summary)
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
	if err := validateContainerUpdateStateForPolicy(inspect, s.updateStopped); err != nil {
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
	slog.Info("skipped automatic update for stopped Docker container",
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
	updated apischema.DockerContainerUpdateResult,
	updateErr error,
) error {
	if updated.Updated {
		s.oldImageIDs = append(s.oldImageIDs, updated.PreviousImageID)
		slog.Info("updated standalone Docker container",
			"component", "docker",
			"subsystem", "update",
			"container", updated.ContainerName,
			"old_image", updated.PreviousImageID,
			"new_image", updated.NewImageID)
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
	summary container.Summary,
) (scheduledUpdateCandidate, error) {
	inspectResult, err := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return scheduledUpdateCandidate{}, fmt.Errorf("inspect scheduled container %q: %w", primaryContainerName(summary), err)
	}
	return inspectContainerUpdateCandidate(ctx, cli, inspectResult.Container)
}

func (s *scheduledUpdateState) recordCandidateError(inspect container.InspectResponse, err error) {
	if inspect.ID != "" {
		err = scheduledUpdateError(s.ctx, inspect, err)
	}
	s.errs = append(s.errs, err)
}

func (s *scheduledUpdateState) addComposeCandidate(target composeProjectTarget, service string, inspect container.InspectResponse) {
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
	collector := &composeMessageCollector{}
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
	for _, inspect := range group.before {
		_ = scheduledUpdateError(s.ctx, inspect, err)
	}
	s.errs = append(s.errs, err)
}

func (s *scheduledUpdateState) verifyComposeGroup(group *scheduledComposeTarget) error {
	for _, before := range group.before {
		after, err := verifyScheduledComposeContainer(s.ctx, s.cli, before)
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
		s.oldImageIDs = append(s.oldImageIDs, before.Image)
		slog.Info("updated Compose Docker container",
			"component", "docker",
			"subsystem", "update",
			"project", group.target.Name,
			"container", strings.TrimPrefix(before.Name, "/"),
			"old_image", before.Image,
			"new_image", after.Image)
	}
	return nil
}

func verifyScheduledComposeContainer(
	ctx context.Context,
	cli *client.Client,
	before container.InspectResponse,
) (container.InspectResponse, error) {
	name := strings.TrimPrefix(before.Name, "/")
	after, err := inspectAndWaitForComposeContainer(ctx, cli, name)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("verify updated Compose container %q: %w", name, err)
	}
	return after, nil
}
