package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"
)

const imageUpdateObservationConcurrency = 6

// RefreshImageUpdates checks all running containers against their configured
// registry references and replaces the persistent status snapshot. The base
// images recorded for images no longer on the host are dropped with it.
func RefreshImageUpdates(ctx context.Context, cli *client.Client) (DockerUpdateCheckResult, error) {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return DockerUpdateCheckResult{}, fmt.Errorf("failed to list containers: %w", err)
	}
	targets := make([]containerImageUpdateTarget, 0, len(containers.Items))
	for _, ctr := range containers.Items {
		targets = append(targets, containerImageUpdateTarget{
			ContainerID:   ctr.ID,
			ContainerName: PrimaryContainerName(ctr),
			ImageID:       ctr.ImageID,
			ImageRef:      ctr.Image,
			Labels:        ctr.Labels,
		})
	}

	statuses, result, err := checkContainerImageUpdates(ctx, cli, classifyComposeBuilt(cli), targets, time.Now())
	if err != nil {
		return DockerUpdateCheckResult{}, err
	}
	images, err := listHostImages(ctx, cli)
	if err != nil {
		return DockerUpdateCheckResult{}, err
	}
	if err := writeUpdateStatuses(ctx, statuses, &images); err != nil {
		return DockerUpdateCheckResult{}, err
	}
	return result, nil
}

// listHostImages is the ID of every image the daemon holds, the same ID an
// image inspection gives (seen with the containerd image store), and when the
// list was asked for.
func listHostImages(ctx context.Context, cli *client.Client) (hostImages, error) {
	images := hostImages{ids: make(map[string]bool), listedAt: time.Now()}
	list, err := cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return hostImages{}, fmt.Errorf("list images: %w", err)
	}
	for _, item := range list.Items {
		images.ids[item.ID] = true
	}
	return images, nil
}

// RefreshContainerUpdate checks one container against its configured registry
// reference and refreshes only that container's status entry.
func RefreshContainerUpdate(ctx context.Context, cli *client.Client, containerID string) (DockerUpdateCheckResult, error) {
	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return DockerUpdateCheckResult{}, fmt.Errorf("inspect container: %w", err)
	}
	inspect := inspectResult.Container
	name := strings.TrimPrefix(inspect.Name, "/")
	if name == "" {
		return DockerUpdateCheckResult{}, fmt.Errorf("container %s has no name", inspect.ID)
	}

	imageRef := ""
	var labels map[string]string
	if inspect.Config != nil {
		imageRef, labels = inspect.Config.Image, inspect.Config.Labels
	}
	statuses, result, err := checkContainerImageUpdates(
		ctx,
		cli,
		classifyComposeBuilt(cli),
		[]containerImageUpdateTarget{{
			ContainerID:   inspect.ID,
			ContainerName: name,
			ImageID:       inspect.Image,
			ImageRef:      imageRef,
			Labels:        labels,
		}},
		time.Now(),
	)
	if err != nil {
		return DockerUpdateCheckResult{}, err
	}
	if err := mergeUpdateStatuses(ctx, statuses); err != nil {
		return DockerUpdateCheckResult{}, err
	}
	return result, nil
}

type imageUpdateCheckClient interface {
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
	DistributionInspect(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

type containerImageUpdateTarget struct {
	ContainerID   string
	ContainerName string
	ImageID       string
	ImageRef      string
	Labels        map[string]string // the container's, which name its Compose project
}

// builtClassifier tells which of targets, containers whose image may be
// built on this host (imageUpdateObservation.mayBeBuilt), run an image their
// Compose service builds: the reason for each, by container ID. A target it
// leaves out keeps what the check found. Tests replace it; nil classifies
// none.
type builtClassifier func(context.Context, []containerImageUpdateTarget) map[string]string

type imageUpdateObservation struct {
	localDigest       string
	remoteDigest      string
	uncheckableReason string
	updateAvailable   bool
	// registryRefused is a lookup the registry refused (no such repository
	// or tag, or no access to it), not a throttled or unreachable registry.
	registryRefused bool
	// builtBy is the Compose project and service the image's own labels say
	// built it, when they do.
	builtBy composeImageBuilder
	err     error
}

// composeImageBuilder is the Compose project and service an image's own
// labels name: Compose labels every image it builds so.
type composeImageBuilder struct {
	project string
	service string
}

func composeImageBuilderOf(labels map[string]string) composeImageBuilder {
	return composeImageBuilder{project: labels[composeProjectLabel], service: labels[composeServiceLabel]}
}

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
)

// mayBeBuilt tells whether the image observed may be one the Compose service
// of a container with labels builds on this host. With Docker's classic image
// store such an image has no repository digest, so it cannot be checked.
// With the containerd image store it has one, its own (`<name>@<image ID>`),
// which the registry refuses to look up; a refusal counts only when the
// image's own labels name the container's project and service, since a
// pulled image whose registry login expired or tag was deleted is refused
// too.
func (o imageUpdateObservation) mayBeBuilt(labels map[string]string) bool {
	if o.uncheckableReason != "" {
		return true
	}
	return o.registryRefused && o.builtBy.project != "" &&
		o.builtBy == composeImageBuilderOf(labels)
}

type imageUpdateObservationRequest struct {
	imageID     string
	imageRef    string
	observation imageUpdateObservation
	inspect     bool
}

func (o imageUpdateObservation) checkState() DockerUpdateCheckState {
	switch {
	case o.err != nil:
		return DockerUpdateCheckStateError
	case o.uncheckableReason != "":
		return DockerUpdateCheckStateUncheckable
	case o.updateAvailable:
		return DockerUpdateCheckStateAvailable
	default:
		return DockerUpdateCheckStateCurrent
	}
}

func checkContainerImageUpdates(
	ctx context.Context,
	cli imageUpdateCheckClient,
	classify builtClassifier,
	targets []containerImageUpdateTarget,
	checkedAt time.Time,
) ([]ImageUpdateStatus, DockerUpdateCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, DockerUpdateCheckResult{}, err
	}

	requests := make([]imageUpdateObservationRequest, 0, len(targets))
	requestByKey := make(map[string]int, len(targets))
	targetRequestIndices := make([]int, len(targets))
	for targetIndex, target := range targets {
		normalizedRef, pinnedDigest, immutable, normalizeErr := NormalizeImageReference(target.ImageRef)
		cacheKey := target.ImageID + "\x00" + normalizedRef
		if normalizeErr != nil {
			cacheKey = target.ImageID + "\x00" + target.ImageRef
		}

		requestIndex, exists := requestByKey[cacheKey]
		if !exists {
			requestIndex = len(requests)
			requestByKey[cacheKey] = requestIndex
			request := imageUpdateObservationRequest{imageID: target.ImageID, imageRef: normalizedRef}
			switch {
			case normalizeErr != nil:
				request.observation.err = normalizeErr
			case immutable:
				request.observation.localDigest = pinnedDigest
			default:
				request.inspect = true
			}
			requests = append(requests, request)
		}
		targetRequestIndices[targetIndex] = requestIndex
	}
	if err := ctx.Err(); err != nil {
		return nil, DockerUpdateCheckResult{}, err
	}
	// Records are kept by image ID, so the base images of the built images
	// are asked for with the images, and one backoff bounds the check.
	records := readBuiltImageRecords()
	baseLookups, err := inspectImageUpdateRequests(ctx, cli, requests, recordedBaseRefs(records, targets))
	if err != nil {
		return nil, DockerUpdateCheckResult{}, err
	}
	built, err := classifyMaybeBuilt(ctx, classify, targets, func(targetIndex int) imageUpdateObservation {
		return requests[targetRequestIndices[targetIndex]].observation
	})
	if err != nil {
		return nil, DockerUpdateCheckResult{}, err
	}
	builtChecks := compareBuiltContainers(targets, built, records, baseLookups)

	statuses := make([]ImageUpdateStatus, 0, len(targets))
	var result DockerUpdateCheckResult
	for targetIndex, target := range targets {
		status := checkedStatus(target, requests[targetRequestIndices[targetIndex]].observation, builtChecks[target.ContainerID], checkedAt)
		statuses = append(statuses, status)
		result.count(status)
	}

	return statuses, result, nil
}

// count adds what the check found for one container to the result.
func (r *DockerUpdateCheckResult) count(status ImageUpdateStatus) {
	r.Checked++
	if status.Err != "" {
		r.Errors++
	}
	switch status.CheckState {
	case DockerUpdateCheckStateUncheckable:
		r.Uncheckable++
	case DockerUpdateCheckStateBuilt:
		r.Built++
	default:
	}
	if status.UpdateAvailable {
		r.Updates++
		if status.CheckState == DockerUpdateCheckStateBuilt {
			r.Rebuilds++
		}
	}
}

// checkedStatus is the status of target that observation found: built by a
// Compose service of the host, for built's reason and with no error, when
// built has one, available when a base image of its image has a newer
// version.
func checkedStatus(target containerImageUpdateTarget, observation imageUpdateObservation, built builtCheck, checkedAt time.Time) ImageUpdateStatus {
	status := ImageUpdateStatus{
		ContainerID:     target.ContainerID,
		ContainerName:   target.ContainerName,
		CheckReason:     observation.uncheckableReason,
		CheckState:      observation.checkState(),
		ImageID:         target.ImageID,
		ImageRef:        target.ImageRef,
		LocalDigest:     observation.localDigest,
		RemoteDigest:    observation.remoteDigest,
		UpdateAvailable: observation.updateAvailable,
		CheckedAt:       checkedAt,
	}
	if observation.err != nil {
		status.Err = observation.err.Error()
	}
	if built.reason != "" {
		status.CheckState, status.CheckReason, status.Err = DockerUpdateCheckStateBuilt, built.reason, ""
		status.UpdateAvailable, status.RemoteDigest = built.baseMoved, built.remoteDigest
	}
	return status
}

// classifyMaybeBuilt runs classify on the targets whose observation (by
// target index) says their Compose service may build their image
// (mayBeBuilt), and returns its reasons for those targets by container ID.
func classifyMaybeBuilt(
	ctx context.Context,
	classify builtClassifier,
	targets []containerImageUpdateTarget,
	observation func(targetIndex int) imageUpdateObservation,
) (map[string]string, error) {
	if classify == nil {
		return nil, nil
	}
	var candidates []containerImageUpdateTarget
	for targetIndex, target := range targets {
		if observation(targetIndex).mayBeBuilt(target.Labels) {
			candidates = append(candidates, target)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	reasons := classify(ctx, candidates)
	built := make(map[string]string, len(reasons))
	for _, target := range candidates {
		if reason, ok := reasons[target.ContainerID]; ok {
			built[target.ContainerID] = reason
		}
	}
	return built, ctx.Err()
}

// containerBuiltReason is why the container inspect, whose image observation
// found, runs an image its Compose service builds on this host (classify);
// empty when it does not. It reaches the verdict a check does.
func containerBuiltReason(ctx context.Context, classify builtClassifier, inspect container.InspectResponse, observation imageUpdateObservation) string {
	if classify == nil || inspect.Config == nil || !observation.mayBeBuilt(inspect.Config.Labels) {
		return ""
	}
	return classify(ctx, []containerImageUpdateTarget{inspectedTarget(inspect)})[inspect.ID]
}

// inspectedTarget is the check target of an inspected container that has a
// configuration.
func inspectedTarget(inspect container.InspectResponse) containerImageUpdateTarget {
	return containerImageUpdateTarget{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		ImageID:       inspect.Image,
		ImageRef:      inspect.Config.Image,
		Labels:        inspect.Config.Labels,
	}
}

// builtReason is the check reason of a container that runs the image service
// of project builds on this host.
func builtReason(project, service string) string {
	return fmt.Sprintf("built on this host by Compose service %q of project %q", service, project)
}

// classifyComposeBuilt is the engine's builtClassifier: a target is built when
// Compose manages it and its service has a build section. Each project's
// configuration is read once (`docker compose config`), for all its targets;
// a target whose project cannot be resolved or read stays uncheckable.
func classifyComposeBuilt(cli *client.Client) builtClassifier {
	return func(ctx context.Context, targets []containerImageUpdateTarget) map[string]string {
		reasons := make(map[string]string)
		for _, project := range composeProjectsOf(ctx, cli, targets) {
			settings := composeProjectSettings(ctx, project)
			for _, service := range project.services {
				if !settings[service].Build {
					continue
				}
				for _, id := range project.members[service] {
					reasons[id] = builtReason(project.target.Name, service)
				}
			}
		}
		return reasons
	}
}

// composeProjectSettings reads the settings of the services of project in
// one `config`, or, when that read fails, of each service alone: one
// service no longer in the files fails the read of all, and must not hide
// the others. A service whose settings cannot be read is left out.
func composeProjectSettings(ctx context.Context, project *composeTargetProject) map[string]composeServiceSettings {
	settings, err := composeServiceSettingsOf(ctx, project.target, project.services)
	if err == nil {
		return settings
	}
	if len(project.services) == 1 || ctx.Err() != nil {
		slog.Debug("failed to read the Compose configuration of a project with images that may be built",
			"component", "docker",
			"subsystem", "update",
			"project", project.target.Name,
			"error", err)
		return nil
	}
	settings = make(map[string]composeServiceSettings, len(project.services))
	for _, service := range project.services {
		one, oneErr := composeServiceSettingsOf(ctx, project.target, []string{service})
		if oneErr != nil {
			slog.Debug("failed to read the Compose configuration of a service with an image that may be built",
				"component", "docker",
				"subsystem", "update",
				"project", project.target.Name,
				"service", service,
				"error", oneErr)
			continue
		}
		maps.Copy(settings, one)
	}
	return settings
}

// composeTargetProject is a Compose project and the targets of a check it
// manages.
type composeTargetProject struct {
	target   ComposeTarget
	services []string
	members  map[string][]string // container IDs by service
}

// composeProjectsOf groups the targets Compose manages by project, in the
// order the targets name them; a target whose project cannot be resolved is
// left out.
func composeProjectsOf(ctx context.Context, cli *client.Client, targets []containerImageUpdateTarget) []*composeTargetProject {
	var projects []*composeTargetProject
	byKey := make(map[string]*composeTargetProject)
	for _, target := range targets {
		composeTarget, service, managed, err := composeTargetForLabels(ctx, cli, target.ContainerName, target.Labels)
		if !managed {
			continue
		}
		if err != nil {
			slog.Debug("failed to resolve the Compose project of Docker container "+target.ContainerName,
				"component", "docker",
				"subsystem", "update",
				"container", target.ContainerName,
				"error", err)
			continue
		}
		key := composeScheduleKey(composeTarget)
		project := byKey[key]
		if project == nil {
			project = &composeTargetProject{target: composeTarget, members: make(map[string][]string)}
			byKey[key] = project
			projects = append(projects, project)
		}
		if _, seen := project.members[service]; !seen {
			project.services = append(project.services, service)
		}
		project.members[service] = append(project.members[service], target.ContainerID)
	}
	return projects
}

// inspectImageUpdateRequests looks up the image of each request that asks for
// it and the digest of each of baseRefs (lookupBaseDigest), all in one pool,
// so a registry that throttles is waited on once whatever it is asked for.
// It returns the base lookups by reference, and an error only when ctx ends.
func inspectImageUpdateRequests(
	ctx context.Context,
	cli imageUpdateCheckClient,
	requests []imageUpdateObservationRequest,
	baseRefs []string,
) (map[string]baseLookup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(imageUpdateObservationConcurrency)
	for requestIndex := range requests {
		if !requests[requestIndex].inspect {
			continue
		}
		group.Go(func() error {
			observation, err := inspectImageUpdate(
				groupCtx,
				cli,
				requests[requestIndex].imageID,
				requests[requestIndex].imageRef,
			)
			if err != nil {
				return err
			}
			requests[requestIndex].observation = observation
			return nil
		})
	}
	bases := make([]baseLookup, len(baseRefs))
	for baseIndex, ref := range baseRefs {
		group.Go(func() error {
			found, err := lookupBaseDigest(groupCtx, cli, ref)
			bases[baseIndex] = found
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	lookups := make(map[string]baseLookup, len(baseRefs))
	for baseIndex, ref := range baseRefs {
		lookups[ref] = bases[baseIndex]
	}
	return lookups, nil
}

func inspectImageUpdate(
	ctx context.Context,
	cli imageUpdateCheckClient,
	imageID string,
	imageRef string,
) (imageUpdateObservation, error) {
	var observation imageUpdateObservation

	local, err := cli.ImageInspect(ctx, imageID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		observation.err = fmt.Errorf("inspect local image %q: %w", imageID, err)
		return observation, nil
	}

	if local.Config != nil {
		observation.builtBy = composeImageBuilderOf(local.Config.Labels)
	}
	localDigests := repositoryDigests(local.RepoDigests)
	if len(localDigests) == 0 {
		observation.uncheckableReason = fmt.Sprintf("local image %q has no repository digest; cannot compare it with %q", imageID, imageRef)
		return observation, nil
	}
	observation.localDigest = localDigests[0].String()

	remote, err := registryDigest(ctx, cli, imageRef)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		observation.err = fmt.Errorf("inspect registry distribution %q: %w", imageRef, err)
		observation.registryRefused = registryRefusal(err)
		return observation, nil
	}
	if err := remote.Validate(); err != nil {
		observation.err = fmt.Errorf("registry returned an invalid manifest digest for %q: %w", imageRef, err)
		return observation, nil
	}

	observation.remoteDigest = remote.String()
	observation.updateAvailable = !slices.Contains(localDigests, remote)
	return observation, nil
}

// registryDigest is the digest the registry gives for imageRef now
// (DistributionInspect). A registry that throttles or does not answer is
// asked again (registryRetries); one that refuses is not. The error is the
// daemon's own, which registryRefusal reads.
func registryDigest(ctx context.Context, cli imageUpdateCheckClient, imageRef string) (digest.Digest, error) {
	remote, err := cli.DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{})
	for retry := 0; err != nil && retry < registryRetries; retry++ {
		// Retry only the registry read. Give throttled, slow or briefly
		// unreachable registries a few minutes to recover without retrying
		// authentication or missing-image errors.
		if !retryableRegistryInspectError(err) {
			break
		}
		if waitErr := waitRegistryBackoff(ctx, retry); waitErr != nil {
			return "", waitErr
		}
		remote, err = cli.DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{})
	}
	if err != nil {
		return "", err
	}
	return remote.Descriptor.Digest, nil
}

// registryRetries is how many times a throttled or unreachable registry call
// is retried; waitRegistryBackoff spreads them over 5+10+20+40+80 seconds.
const registryRetries = 5

func waitRegistryBackoff(ctx context.Context, retry int) error {
	timer := time.NewTimer(5 * time.Second << retry)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	return ctx.Err()
}

func retryableRegistryInspectError(err error) bool {
	var networkError net.Error
	if errdefs.IsResourceExhausted(err) || errdefs.IsDeadlineExceeded(err) ||
		(errors.As(err, &networkError) && networkError.Timeout()) {
		return true
	}
	// Errors from the daemon's registry client lose their type across HTTP.
	return retryableRegistryMessage(err.Error())
}

// registryRefusal tells a registry that refused a lookup (no such repository
// or tag, or no access to it) from one that throttled, timed out or could not
// be reached.
func registryRefusal(err error) bool {
	if retryableRegistryInspectError(err) {
		return false
	}
	if errdefs.IsNotFound(err) || errdefs.IsUnauthorized(err) || errdefs.IsPermissionDenied(err) {
		return true
	}
	// Errors from the daemon's registry client lose their type across HTTP.
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "denied") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "manifest unknown") ||
		strings.Contains(message, "not found")
}

func retryableRegistryMessage(message string) bool {
	message = strings.ToLower(message)
	// GHCR throttles with "error from registry: retry-after: …" and no 429 text.
	return strings.Contains(message, "retry-after:") ||
		strings.Contains(message, "toomanyrequests") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(message, "tls handshake timeout") ||
		strings.Contains(message, "i/o timeout") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connection reset")
}

// NormalizeImageReference returns the canonical tagged reference for imageRef,
// plus the pinned digest when the reference is immutable.
func NormalizeImageReference(imageRef string) (normalized string, pinnedDigest string, immutable bool, err error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", "", false, fmt.Errorf("container image reference is empty")
	}

	if imageDigest, parseErr := digest.Parse(imageRef); parseErr == nil {
		return imageRef, imageDigest.String(), true, nil
	}

	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", "", false, fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}
	if digested, ok := named.(reference.Digested); ok {
		return named.String(), digested.Digest().String(), true, nil
	}

	tagged := reference.TagNameOnly(named)
	return tagged.String(), "", false, nil
}

func repositoryDigests(values []string) []digest.Digest {
	digests := make([]digest.Digest, 0, len(values))
	seen := make(map[digest.Digest]struct{}, len(values))
	for _, value := range values {
		parsed, err := reference.ParseAnyReference(value)
		if err != nil {
			continue
		}
		digested, ok := parsed.(reference.Digested)
		if !ok {
			continue
		}
		valueDigest := digested.Digest()
		if err := valueDigest.Validate(); err != nil {
			continue
		}
		if _, ok := seen[valueDigest]; ok {
			continue
		}
		seen[valueDigest] = struct{}{}
		digests = append(digests, valueDigest)
	}
	return digests
}
