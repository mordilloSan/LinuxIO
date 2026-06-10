package docker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type imageUpdateStatus struct {
	ContainerID     string
	ContainerName   string
	ImageID         string
	ImageRef        string
	LocalDigest     string
	RemoteDigest    string
	UpdateAvailable bool
	CheckedAt       time.Time
	Err             string
}

var imageUpdateCache = struct {
	sync.RWMutex
	byContainerID   map[string]imageUpdateStatus
	byContainerName map[string]imageUpdateStatus
	byImageID       map[string]imageUpdateStatus
}{
	byContainerID:   map[string]imageUpdateStatus{},
	byContainerName: map[string]imageUpdateStatus{},
	byImageID:       map[string]imageUpdateStatus{},
}

func RefreshDockerImageUpdates(ctx context.Context) (apischema.DockerUpdateCheckResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("failed to list containers: %w", err)
	}

	statuses := make([]imageUpdateStatus, 0, len(containers.Items))
	result := apischema.DockerUpdateCheckResult{}
	for _, ctr := range containers.Items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		status := checkContainerImageUpdate(ctx, cli, ctr)
		statuses = append(statuses, status)
		result.Checked++
		if status.Err != "" {
			result.Errors++
		}
		if status.UpdateAvailable {
			result.Updates++
		}
	}
	replaceImageUpdateCache(statuses)

	return result, nil
}

func checkContainerImageUpdate(ctx context.Context, cli *client.Client, ctr container.Summary) imageUpdateStatus {
	status := imageUpdateStatus{
		ContainerID:   ctr.ID,
		ContainerName: primaryContainerName(ctr),
		ImageID:       ctr.ImageID,
		ImageRef:      ctr.Image,
		CheckedAt:     time.Now(),
	}

	normalizedRef, repoName, err := normalizeTaggedImageRef(ctr.Image)
	if err != nil {
		status.Err = err.Error()
		return status
	}
	status.ImageRef = normalizedRef

	localInspectRef := ctr.ImageID
	if strings.TrimSpace(localInspectRef) == "" {
		localInspectRef = ctr.Image
	}
	localImage, err := cli.ImageInspect(ctx, localInspectRef)
	if err != nil {
		status.Err = fmt.Sprintf("inspect local image: %v", err)
		return status
	}
	localDigests := matchingRepoDigests(localImage.RepoDigests, repoName)
	if len(localDigests) == 0 {
		status.Err = "local image has no matching registry digest"
		return status
	}
	status.LocalDigest = localDigests[0]

	auth, err := resolveRegistryAuth(ctx, normalizedRef)
	if err != nil {
		status.Err = fmt.Sprintf("load registry auth: %v", err)
		return status
	}
	remote, err := cli.DistributionInspect(ctx, normalizedRef, client.DistributionInspectOptions{EncodedRegistryAuth: auth})
	if err != nil {
		status.Err = fmt.Sprintf("inspect remote image: %v", err)
		return status
	}
	remoteDigest := remote.Descriptor.Digest.String()
	if remoteDigest == "" {
		status.Err = "remote image digest is empty"
		return status
	}
	status.RemoteDigest = remoteDigest
	status.UpdateAvailable = !slices.Contains(localDigests, remoteDigest)
	return status
}

func replaceImageUpdateCache(statuses []imageUpdateStatus) {
	byContainerID := make(map[string]imageUpdateStatus, len(statuses))
	byContainerName := make(map[string]imageUpdateStatus, len(statuses))
	byImageID := make(map[string]imageUpdateStatus, len(statuses))
	for _, status := range statuses {
		if status.ContainerID != "" {
			byContainerID[status.ContainerID] = status
		}
		if status.ContainerName != "" {
			byContainerName[status.ContainerName] = status
		}
		if status.ImageID != "" {
			existing, ok := byImageID[status.ImageID]
			if !ok || status.UpdateAvailable || (!existing.UpdateAvailable && existing.Err != "") {
				byImageID[status.ImageID] = status
			}
		}
	}

	imageUpdateCache.Lock()
	imageUpdateCache.byContainerID = byContainerID
	imageUpdateCache.byContainerName = byContainerName
	imageUpdateCache.byImageID = byImageID
	imageUpdateCache.Unlock()
}

func imageUpdateStatusForContainer(containerID string) (imageUpdateStatus, bool) {
	imageUpdateCache.RLock()
	defer imageUpdateCache.RUnlock()
	status, ok := imageUpdateCache.byContainerID[containerID]
	return status, ok
}

func imageUpdateStatusForImage(img image.Summary) (imageUpdateStatus, bool) {
	imageUpdateCache.RLock()
	defer imageUpdateCache.RUnlock()
	if status, ok := imageUpdateCache.byImageID[img.ID]; ok {
		return status, true
	}
	for _, digest := range img.RepoDigests {
		for _, status := range imageUpdateCache.byImageID {
			if status.LocalDigest == digest {
				return status, true
			}
		}
	}
	return imageUpdateStatus{}, false
}

func normalizeTaggedImageRef(imageRef string) (string, string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", "", fmt.Errorf("image reference is empty")
	}
	if strings.HasPrefix(imageRef, "sha256:") {
		return "", "", fmt.Errorf("image is pinned by digest")
	}
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", "", fmt.Errorf("parse image reference: %w", err)
	}
	if _, ok := named.(reference.Digested); ok {
		return "", "", fmt.Errorf("image is pinned by digest")
	}
	tagged := reference.TagNameOnly(named)
	repoName := reference.TrimNamed(tagged).Name()
	return reference.FamiliarString(tagged), repoName, nil
}

func matchingRepoDigests(repoDigests []string, repoName string) []string {
	matches := make([]string, 0, len(repoDigests))
	for _, repoDigest := range repoDigests {
		named, err := reference.ParseNormalizedNamed(repoDigest)
		if err != nil {
			continue
		}
		digested, ok := named.(reference.Digested)
		if !ok {
			continue
		}
		if reference.TrimNamed(named).Name() == repoName {
			matches = append(matches, digested.Digest().String())
		}
	}
	return matches
}

func primaryContainerName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(ctr.Names[0], "/")
}
