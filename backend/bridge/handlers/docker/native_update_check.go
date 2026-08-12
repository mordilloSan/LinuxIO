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

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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
	localDigest     string
	remoteDigest    string
	updateAvailable bool
	err             error
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

	statuses := make([]imageUpdateStatus, 0, len(targets))
	observations := make(map[string]imageUpdateObservation)
	var result apischema.DockerUpdateCheckResult

	for _, target := range targets {
		observation, err := cachedImageUpdateObservation(ctx, cli, observations, target)
		if err != nil {
			return nil, apischema.DockerUpdateCheckResult{}, err
		}

		status := imageUpdateStatus{
			ContainerID:     target.ContainerID,
			ContainerName:   target.ContainerName,
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
		if status.UpdateAvailable {
			result.Updates++
		}
	}

	return statuses, result, nil
}

func cachedImageUpdateObservation(
	ctx context.Context,
	cli imageUpdateCheckClient,
	cache map[string]imageUpdateObservation,
	target containerImageUpdateTarget,
) (imageUpdateObservation, error) {
	if err := ctx.Err(); err != nil {
		return imageUpdateObservation{}, err
	}

	normalizedRef, pinnedDigest, immutable, normalizeErr := normalizeUpdateReference(target.ImageRef)
	cacheKey := target.ImageID + "\x00" + normalizedRef
	if normalizeErr != nil {
		cacheKey = target.ImageID + "\x00" + target.ImageRef
	}
	if observation, ok := cache[cacheKey]; ok {
		return observation, nil
	}

	var observation imageUpdateObservation
	switch {
	case normalizeErr != nil:
		observation.err = normalizeErr
	case immutable:
		observation.localDigest = pinnedDigest
	default:
		var err error
		observation, err = inspectImageUpdate(ctx, cli, target.ImageID, normalizedRef)
		if err != nil {
			return imageUpdateObservation{}, err
		}
	}
	cache[cacheKey] = observation
	return observation, nil
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
		observation.err = fmt.Errorf("local image %q has no repository digest; cannot compare it with %q", imageID, imageRef)
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
