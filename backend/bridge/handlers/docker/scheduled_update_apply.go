package docker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type scheduledUpdateCandidate struct {
	inspect       container.InspectResponse
	result        apischema.DockerContainerUpdateResult
	normalizedRef string
	needsUpdate   bool
}

type scheduledUpdateState struct {
	ctx           context.Context
	cli           *client.Client
	composeGroups map[string]*scheduledComposeTarget
	oldImageIDs   []string
	errs          []error
}

func newScheduledUpdateState(ctx context.Context, cli *client.Client) *scheduledUpdateState {
	return &scheduledUpdateState{
		ctx:           ctx,
		cli:           cli,
		composeGroups: make(map[string]*scheduledComposeTarget),
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

	target, service, managedByCompose, err := composeTargetForContainer(s.ctx, s.cli, candidate.inspect)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordCandidateError(candidate.inspect, err)
		return nil
	}
	if managedByCompose {
		s.addComposeCandidate(target, service, candidate.inspect)
		return nil
	}

	updated, err := updateStandaloneContainer(s.ctx, s.cli, candidate.inspect, candidate.normalizedRef, candidate.result)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordCandidateError(candidate.inspect, err)
		return nil
	}
	if updated.Updated {
		s.oldImageIDs = append(s.oldImageIDs, updated.PreviousImageID)
		slog.Info("updated standalone Docker container",
			"component", "docker-update",
			"container", updated.ContainerName,
			"old_image", updated.PreviousImageID,
			"new_image", updated.NewImageID)
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
	candidate := scheduledUpdateCandidate{inspect: inspectResult.Container}
	result, imageRef, err := newContainerUpdateResult(candidate.inspect)
	if err != nil {
		return candidate, err
	}
	candidate.result = result
	normalizedRef, _, immutable, err := normalizeUpdateReference(imageRef)
	if err != nil {
		return candidate, err
	}
	if immutable {
		markContainerCurrent(ctx, candidate.inspect.ID, candidate.inspect)
		return candidate, nil
	}
	observation, err := inspectImageUpdate(ctx, cli, candidate.inspect.Image, normalizedRef)
	if err != nil {
		return candidate, err
	}
	if observation.err != nil {
		return candidate, observation.err
	}
	if !observation.updateAvailable {
		markContainerCurrent(ctx, candidate.inspect.ID, candidate.inspect)
		return candidate, nil
	}
	candidate.normalizedRef = normalizedRef
	candidate.needsUpdate = true
	return candidate, nil
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
	collector := &composeMessageCollector{}
	if err := composePullAndUpServices(s.ctx, group.target, group.services, collector.Emit); err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		groupErr := fmt.Errorf("update Compose project %q: %w: %s", group.target.Name, err, collector.String())
		for _, inspect := range group.before {
			_ = scheduledUpdateError(s.ctx, inspect, groupErr)
		}
		s.errs = append(s.errs, groupErr)
		return nil
	}
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
		s.oldImageIDs = append(s.oldImageIDs, before.Image)
		slog.Info("updated Compose Docker container",
			"component", "docker-update",
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
	afterResult, err := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("inspect updated Compose container %q: %w", name, err)
	}
	after, err := waitForContainerReady(ctx, cli, afterResult.Container.ID)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("verify updated Compose container %q: %w", name, err)
	}
	if after.Image == before.Image {
		return container.InspectResponse{}, fmt.Errorf("Compose container %q did not activate the pulled image", name)
	}
	return after, nil
}
