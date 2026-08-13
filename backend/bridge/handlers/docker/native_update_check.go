package docker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const imageUpdateObservationConcurrency = 6

type imageUpdateCheckClient interface {
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
	DistributionInspect(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

type containerImageUpdateTarget struct {
	ContainerID   string
	ContainerName string
	ImageID       string
	ImageRef      string
}

type imageUpdateObservation struct {
	localDigest       string
	remoteDigest      string
	uncheckableReason string
	updateAvailable   bool
	err               error
}

type imageUpdateObservationRequest struct {
	imageID     string
	imageRef    string
	observation imageUpdateObservation
	inspect     bool
}

func (o imageUpdateObservation) checkState() apischema.DockerUpdateCheckState {
	switch {
	case o.err != nil:
		return apischema.DockerUpdateCheckStateError
	case o.uncheckableReason != "":
		return apischema.DockerUpdateCheckStateUncheckable
	case o.updateAvailable:
		return apischema.DockerUpdateCheckStateAvailable
	default:
		return apischema.DockerUpdateCheckStateCurrent
	}
}

func checkContainerImageUpdates(
	ctx context.Context,
	cli imageUpdateCheckClient,
	targets []containerImageUpdateTarget,
	checkedAt time.Time,
) ([]imageUpdateStatus, apischema.DockerUpdateCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, apischema.DockerUpdateCheckResult{}, err
	}

	requests := make([]imageUpdateObservationRequest, 0, len(targets))
	requestByKey := make(map[string]int, len(targets))
	targetRequestIndices := make([]int, len(targets))
	for targetIndex, target := range targets {
		normalizedRef, pinnedDigest, immutable, normalizeErr := normalizeUpdateReference(target.ImageRef)
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
		return nil, apischema.DockerUpdateCheckResult{}, err
	}
	if err := inspectImageUpdateRequests(ctx, cli, requests); err != nil {
		return nil, apischema.DockerUpdateCheckResult{}, err
	}

	statuses := make([]imageUpdateStatus, 0, len(targets))
	var result apischema.DockerUpdateCheckResult
	for targetIndex, target := range targets {
		observation := requests[targetRequestIndices[targetIndex]].observation

		status := imageUpdateStatus{
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
		statuses = append(statuses, status)

		result.Checked++
		if status.Err != "" {
			result.Errors++
		}
		if status.CheckState == apischema.DockerUpdateCheckStateUncheckable {
			result.Uncheckable++
		}
		if status.UpdateAvailable {
			result.Updates++
		}
	}

	return statuses, result, nil
}

func inspectImageUpdateRequests(
	ctx context.Context,
	cli imageUpdateCheckClient,
	requests []imageUpdateObservationRequest,
) error {
	if err := ctx.Err(); err != nil {
		return err
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
	return group.Wait()
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

	localDigests := repositoryDigests(local.RepoDigests)
	if len(localDigests) == 0 {
		observation.uncheckableReason = fmt.Sprintf("local image %q has no repository digest; cannot compare it with %q", imageID, imageRef)
		return observation, nil
	}
	observation.localDigest = localDigests[0].String()

	remote, err := cli.DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		observation.err = fmt.Errorf("inspect registry distribution %q: %w", imageRef, err)
		return observation, nil
	}
	if err := remote.Descriptor.Digest.Validate(); err != nil {
		observation.err = fmt.Errorf("registry returned an invalid manifest digest for %q: %w", imageRef, err)
		return observation, nil
	}

	observation.remoteDigest = remote.Descriptor.Digest.String()
	observation.updateAvailable = !slices.Contains(localDigests, remote.Descriptor.Digest)
	return observation, nil
}

func normalizeUpdateReference(imageRef string) (normalized string, pinnedDigest string, immutable bool, err error) {
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
