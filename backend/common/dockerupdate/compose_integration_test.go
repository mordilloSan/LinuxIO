package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
)

// TestDockerUpdateComposeIntegration exercises the real Compose-managed replacement
// path. It is opt-in because it pulls public images and requires a Docker
// daemon with the Compose plugin.
func TestDockerUpdateComposeIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)
	withTempUpdateStatusPath(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	project := fmt.Sprintf("linuxio-it-%d", time.Now().UnixNano())
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	projectEnvPath := filepath.Join(dir, ".env")
	serviceEnvPath := filepath.Join(dir, "service.env")
	const service = "app"
	const firstImage = "alpine:3.20"
	const secondImage = "alpine:3.21"
	writeDockerUpdateComposeEnvironmentFile(t, projectEnvPath, "LINUXIO_IT_IMAGE", firstImage)
	writeDockerUpdateComposeEnvironmentFile(t, serviceEnvPath, "LINUXIO_SERVICE_VALUE", "from-service-env-file")
	writeDockerUpdateComposeFileWithEnvironment(t, composePath, service, "${LINUXIO_IT_IMAGE}", "./service.env")
	registerDockerUpdateComposeCleanup(t, project, composePath, dir)

	if err := composeUp(ctx, project, composePath, dir, true, nil); err != nil {
		t.Fatalf("compose up with %s: %v", firstImage, err)
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer cli.Close()
	initial := mustInspectComposeService(t, ctx, cli, project, service)
	assertInitialComposeContainer(t, initial, project, service)
	resolved, resolvedService, managed, err := composeTargetForContainer(ctx, cli, initial)
	if err != nil {
		t.Fatalf("resolve Compose target: %v", err)
	}
	if !managed || resolvedService != service || resolved.Name != project || len(resolved.ConfigFiles) != 1 || resolved.ConfigFiles[0] != composePath || resolved.WorkingDir != dir {
		t.Fatalf("resolved target = %#v, service=%q, managed=%v; want project/config/working dir from labels", resolved, resolvedService, managed)
	}

	writeDockerUpdateComposeEnvironmentFile(t, projectEnvPath, "LINUXIO_IT_IMAGE", secondImage)
	updateResult, _, err := newContainerUpdateResult(initial)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	updatedResult, err := updateComposeContainerWithProgress(ctx, cli, initial, resolved, service, updateResult, nil)
	if err != nil {
		t.Fatalf("production Compose update to %s: %v", secondImage, err)
	}
	if !updatedResult.Updated || updatedResult.NewImageID == initial.Image {
		t.Fatalf("production Compose update result = %#v, want replacement with a new image", updatedResult)
	}
	updated := mustInspectComposeService(t, ctx, cli, project, service)
	assertUpdatedComposeContainer(t, initial, updated, secondImage)
	if updated.Config == nil || !slices.Contains(updated.Config.Env, "LINUXIO_SERVICE_VALUE=from-service-env-file") {
		t.Fatalf("updated container environment = %#v, want value from service env_file", updated.Config)
	}
}

// TestDockerUpdateComposeRollbackIntegration moves a service's tag to an image
// the service exits on, as a pull does, and checks that the rollback puts the
// tag back on the old image and brings the service up on it again.
func TestDockerUpdateComposeRollbackIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)
	withTempUpdateStatusPath(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cli, clientErr := client.New(client.FromEnv)
	if clientErr != nil {
		t.Fatalf("create Docker client: %v", clientErr)
	}
	defer cli.Close()

	project := fmt.Sprintf("linuxio-it-%d", time.Now().UnixNano())
	const service = "app"
	const oldImage, newImage = "alpine:3.20", "alpine:3.21"
	tag := project + "-app:latest"
	for _, ref := range []string{oldImage, newImage} {
		if _, err := PullAndInspectImage(ctx, cli, ref); err != nil {
			t.Fatalf("pull %s: %v", ref, err)
		}
	}
	if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: oldImage, Target: tag}); err != nil {
		t.Fatalf("tag %s as %s: %v", oldImage, tag, err)
	}
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	// The service runs on alpine 3.20 and exits on any other release.
	content := fmt.Sprintf("services:\n  %s:\n    image: %s\n    command: [\"sh\", \"-c\", \"grep -q '^3.20' /etc/alpine-release && exec sleep infinity\"]\n", service, tag)
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write Compose file: %v", err)
	}
	// Registered before the project's cleanup, so it runs after it: the tag
	// is removed once the project is down.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := cli.ImageRemove(cleanupCtx, tag, client.ImageRemoveOptions{}); err != nil {
			t.Logf("best-effort tag cleanup failed: %v", err)
		}
	})
	registerDockerUpdateComposeCleanup(t, project, composePath, dir)

	if err := composeUp(ctx, project, composePath, dir, true, nil); err != nil {
		t.Fatalf("compose up on %s: %v", oldImage, err)
	}
	initial := mustInspectComposeService(t, ctx, cli, project, service)
	assertInitialComposeContainer(t, initial, project, service)
	target, resolvedService, managed, err := composeTargetForContainer(ctx, cli, initial)
	if err != nil || !managed || resolvedService != service {
		t.Fatalf("resolve Compose target: %#v, %q, %v, %v", target, resolvedService, managed, err)
	}
	result, _, err := newContainerUpdateResult(initial)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	// The update: the tag moves to the new image, as a pull moves it, and
	// the service is brought up on it.
	pullAndUp := func(ctx context.Context, target ComposeTarget, service string, emitter ComposeLineEmitter) error {
		if _, tagErr := cli.ImageTag(ctx, client.ImageTagOptions{Source: newImage, Target: tag}); tagErr != nil {
			return tagErr
		}
		return RunComposeProject(ctx, target, emitter, "up", "-d", "--no-deps", service)
	}

	_, err = updateComposeContainerWithRunner(ctx, cli, newComposeRollback(cli), initial, target, service, result, pullAndUp, nil)
	if !errors.As(err, new(rolledBackError)) {
		t.Fatalf("update error = %v, want a rolled back update", err)
	}
	tagged, err := cli.ImageInspect(ctx, tag)
	if err != nil || tagged.ID != initial.Image {
		t.Fatalf("tag %s names %s (%v), want the old image %s", tag, tagged.ID, err, initial.Image)
	}
	restored := mustInspectComposeService(t, ctx, cli, project, service)
	if restored.ID == initial.ID || restored.Image != initial.Image || restored.State == nil || !restored.State.Running {
		t.Fatalf("restored container %s on %s (state %#v), want a new container running %s", restored.ID, restored.Image, restored.State, initial.Image)
	}
}

// TestDockerUpdateComposeRebuildIntegration rebuilds a service built from a
// Dockerfile on the host: with nothing changed, which must leave the
// container alone (a fully cached build keeps the image ID, or every rebuild
// would recreate the container); with a changed Dockerfile, which replaces
// it; and with a Dockerfile whose container exits at once, which is rolled
// back to the image it ran. Each step starts from what the last left.
func TestDockerUpdateComposeRebuildIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)
	withTempUpdateStatusPath(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	f := newRebuildFixture(ctx, t)
	initial := f.inspect()
	assertInitialComposeContainer(t, initial, f.project, rebuildService)

	f.checkFindsBuilt(initial)

	// The image `compose up` built carries Docker's default attestations, so
	// the first rebuild may give a new image ID even with nothing changed.
	if first, _, err := f.rebuild(initial); err != nil {
		t.Fatalf("first rebuild: %v", err)
	} else {
		t.Logf("first rebuild of the image compose up built: updated %v, %s -> %s", first.Updated, shortImageID(first.PreviousImageID), shortImageID(first.NewImageID))
	}
	current := f.inspect()
	f.checkBaseRecorded(current)
	f.rebuildUnchanged(current)
	f.checkBuiltStatus(current, false)
	current = f.recordSurvivesProjectDown(current)
	f.writeDockerfile("echo two > /release", `["sleep", "infinity"]`)
	second := f.rebuildChanged(current)
	f.writeDockerfile("echo three > /release", `["false"]`)
	f.rebuildFailing(second)
}

// checkFindsBuilt: the check stores the container as built, and an update
// that is not a rebuild finds nothing to do and records no error.
func (f rebuildFixture) checkFindsBuilt(initial container.InspectResponse) {
	f.t.Helper()
	name := strings.TrimPrefix(initial.Name, "/")
	if _, err := RefreshContainerUpdate(f.ctx, f.cli, initial.ID); err != nil {
		f.t.Fatalf("check the container: %v", err)
	}
	// Built by `compose up`, not by LinuxIO: nothing records its bases.
	status, ok := ReadStatusSnapshot().ForContainerName(name)
	if !ok || status.CheckState != DockerUpdateCheckStateBuilt || status.Err != "" || status.UpdateAvailable ||
		status.CheckReason != builtReason(f.project, rebuildService)+builtWithoutRecord {
		f.t.Fatalf("stored status = %+v, %v; want built, with no record", status, ok)
	}

	var phases []string
	result, err := updateInspectedContainerWithProgress(f.ctx, f.cli, initial, func(phase, message string) {
		phases = append(phases, phase+": "+message)
	})
	if err != nil || result.Updated {
		f.t.Fatalf("update of a built container = %+v, %v; want nothing to do", result, err)
	}
	if status, _ := ReadStatusSnapshot().ForContainerName(name); status.CheckState != DockerUpdateCheckStateBuilt || status.Err != "" {
		f.t.Fatalf("status after an update = %+v, want it still built", status)
	}
	f.t.Logf("update of a built container reported %q", phases)
}

// checkBaseRecorded: a rebuild by LinuxIO records the base it resolved, with
// the digest the registry gives for it now, so the check finds no newer
// version; a recorded digest the registry no longer gives makes the
// container wait for a rebuild.
func (f rebuildFixture) checkBaseRecorded(current container.InspectResponse) {
	f.t.Helper()
	record, ok := readBuiltImageRecords()[current.Image]
	if !ok || len(record.Bases) != 1 || record.Bases[0].Ref != "docker.io/library/alpine:3.20" {
		f.t.Fatalf("record of %s = %+v, %v; want the base alpine:3.20", current.Image, record, ok)
	}
	base := record.Bases[0]
	remote, err := registryDigest(f.ctx, f.cli, base.Ref)
	if err != nil || remote.String() != base.Digest {
		f.t.Fatalf("registry digest of %s = %s (%v), recorded %s; want the same", base.Ref, remote, err, base.Digest)
	}
	f.t.Logf("recorded base %s@%s", base.Ref, base.Digest)
	f.checkBuiltStatus(current, false)

	record.Bases = []baseImage{{Ref: base.Ref, Digest: digest.FromString("an older alpine").String()}}
	if err := recordBuiltImage(f.ctx, record); err != nil {
		f.t.Fatalf("record an older base: %v", err)
	}
	f.checkBuiltStatus(current, true)
}

// recordSurvivesProjectDown: a check of every container while the project is
// down keeps the record of its image, which is still on the host (the image
// list gives the ID the record is kept by), so the container brought up on
// it again is still watched. It returns that container.
func (f rebuildFixture) recordSurvivesProjectDown(current container.InspectResponse) container.InspectResponse {
	f.t.Helper()
	composePath := filepath.Join(f.dir, "compose.yaml")
	if err := composeDown(f.ctx, f.project, composePath, f.dir, false, nil); err != nil {
		f.t.Fatalf("compose down: %v", err)
	}
	if _, err := RefreshImageUpdates(f.ctx, f.cli); err != nil {
		f.t.Fatalf("check every container: %v", err)
	}
	if _, ok := readBuiltImageRecords()[current.Image]; !ok {
		f.t.Fatalf("the record of image %s was dropped while its project was down", current.Image)
	}
	if err := runCompose(f.ctx, f.project, composePath, f.dir, nil, "up", "-d", "--no-build"); err != nil {
		f.t.Fatalf("compose up: %v", err)
	}
	again := f.inspect()
	if again.Image != current.Image {
		f.t.Fatalf("container came back on %s, want %s", again.Image, current.Image)
	}
	f.checkBuiltStatus(again, false)
	return again
}

// checkBuiltStatus checks current, and wants it built, and waiting for a
// rebuild when baseMoved.
func (f rebuildFixture) checkBuiltStatus(current container.InspectResponse, baseMoved bool) {
	f.t.Helper()
	if _, err := RefreshContainerUpdate(f.ctx, f.cli, current.ID); err != nil {
		f.t.Fatalf("check the container: %v", err)
	}
	status, ok := ReadStatusSnapshot().ForContainerName(strings.TrimPrefix(current.Name, "/"))
	if !ok || status.CheckState != DockerUpdateCheckStateBuilt || status.Err != "" || status.UpdateAvailable != baseMoved {
		f.t.Fatalf("status = %+v, %v; want built, waiting for a rebuild: %v", status, ok, baseMoved)
	}
	f.t.Logf("check of the built container: %s", status.CheckReason)
}

const rebuildService = "app"

// rebuildFixture is a Compose project whose service rebuildService is built
// from a Dockerfile in dir.
type rebuildFixture struct {
	ctx     context.Context
	t       *testing.T
	cli     *client.Client
	project string
	dir     string
}

// newRebuildFixture brings up a project whose service builds FROM
// alpine:3.20, and removes it and every image it built when the test ends.
func newRebuildFixture(ctx context.Context, t *testing.T) rebuildFixture {
	t.Helper()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	logDockerImageStore(ctx, t, cli)
	f := rebuildFixture{ctx: ctx, t: t, cli: cli, project: fmt.Sprintf("linuxio-it-%d", time.Now().UnixNano()), dir: t.TempDir()}

	// The base image is the registry's now, as the rebuild's --pull gets it.
	if _, err := PullAndInspectImage(ctx, cli, "alpine:3.20"); err != nil {
		t.Fatalf("pull alpine:3.20: %v", err)
	}
	composePath := filepath.Join(f.dir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("services:\n  "+rebuildService+":\n    build: .\n"), 0o600); err != nil {
		t.Fatalf("write Compose file: %v", err)
	}
	// Registered before the project's cleanup, so it runs after it.
	registerLabelledImageCleanup(t, cli, "linuxio.integration="+f.project)
	registerDockerUpdateComposeCleanup(t, f.project, composePath, f.dir)
	f.writeDockerfile("echo one > /release", `["sleep", "infinity"]`)
	if err := composeUp(ctx, f.project, composePath, f.dir, true, nil); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	return f
}

// writeDockerfile writes the service's Dockerfile. Every image built from it
// carries the project as a label, so the ones a rollback leaves untagged are
// removed too.
func (f rebuildFixture) writeDockerfile(run, cmd string) {
	f.t.Helper()
	content := fmt.Sprintf("FROM alpine:3.20\nLABEL linuxio.integration=%s\nRUN %s\nCMD %s\n", f.project, run, cmd)
	if err := os.WriteFile(filepath.Join(f.dir, "Dockerfile"), []byte(content), 0o600); err != nil {
		f.t.Fatalf("write Dockerfile: %v", err)
	}
}

func (f rebuildFixture) inspect() container.InspectResponse {
	f.t.Helper()
	return mustInspectComposeService(f.t, f.ctx, f.cli, f.project, rebuildService)
}

// rebuild rebuilds the service that before runs, and returns the lines of
// the build's output it reported. The tag that held the image before ran is
// gone afterwards, whatever the outcome.
func (f rebuildFixture) rebuild(before container.InspectResponse) (DockerContainerUpdateResult, []string, error) {
	f.t.Helper()
	var lines []string
	result, err := rebuildInspectedContainer(f.ctx, f.cli, before, func(phase, message string) {
		if phase == ProgressPhaseBuildOutput {
			lines = append(lines, message)
		}
	})
	hold := strings.TrimSuffix(normalizedReference(before.Config.Image), ":latest") + ":" + rebuildPreviousTag
	if held, inspectErr := f.cli.ImageInspect(f.ctx, hold); inspectErr == nil {
		f.t.Fatalf("tag %s still names image %s after the rebuild", hold, held.ID)
	}
	return result, lines, err
}

// rebuildUnchanged: the cached build keeps the image ID, and the container is
// left as it is.
func (f rebuildFixture) rebuildUnchanged(initial container.InspectResponse) {
	f.t.Helper()
	unchanged, lines, err := f.rebuild(initial)
	if err != nil {
		f.t.Fatalf("rebuild with nothing changed: %v", err)
	}
	f.t.Logf("cached rebuild reported %d lines of build output, the last: %q", len(lines), lines[max(0, len(lines)-3):])
	if unchanged.Updated || unchanged.NewImageID != initial.Image {
		f.t.Fatalf("rebuild with nothing changed = %+v; a fully cached build must keep the image ID %s", unchanged, initial.Image)
	}
	if same := f.inspect(); same.ID != initial.ID {
		f.t.Fatalf("rebuild with nothing changed replaced container %s with %s", initial.ID, same.ID)
	}
	if len(lines) < 2 {
		f.t.Fatalf("building lines = %q, want the build's output reported", lines)
	}
}

// rebuildChanged: a changed Dockerfile gives a new image, and the service is
// replaced on it.
func (f rebuildFixture) rebuildChanged(initial container.InspectResponse) container.InspectResponse {
	f.t.Helper()
	updated, _, err := f.rebuild(initial)
	if err != nil {
		f.t.Fatalf("rebuild with a changed Dockerfile: %v", err)
	}
	second := f.inspect()
	if !updated.Updated || updated.NewImageID == initial.Image || second.ID == initial.ID || second.Image != updated.NewImageID ||
		second.State == nil || !second.State.Running {
		f.t.Fatalf("rebuild with a changed Dockerfile = %+v, container %s on %s (%#v); want a new running container on a new image", updated, second.ID, second.Image, second.State)
	}
	status, ok := ReadStatusSnapshot().ForContainerName(strings.TrimPrefix(second.Name, "/"))
	if !ok || status.CheckState != DockerUpdateCheckStateBuilt || status.ImageID != second.Image {
		f.t.Fatalf("status = %+v, %v; want the rebuilt container built", status, ok)
	}
	return second
}

// rebuildFailing: a new container that exits at once is rolled back to the
// image the service ran.
func (f rebuildFixture) rebuildFailing(second container.InspectResponse) {
	f.t.Helper()
	if _, _, err := f.rebuild(second); !errors.As(err, new(rolledBackError)) {
		f.t.Fatalf("rebuild to a failing image: error %v, want a rolled back rebuild", err)
	}
	tagged, err := f.cli.ImageInspect(f.ctx, second.Config.Image)
	if err != nil || tagged.ID != second.Image {
		f.t.Fatalf("tag %s names %s (%v), want the image the service ran, %s", second.Config.Image, tagged.ID, err, second.Image)
	}
	restored := f.inspect()
	if restored.ID == second.ID || restored.Image != second.Image || restored.State == nil || !restored.State.Running {
		f.t.Fatalf("restored container %s on %s (state %#v), want a new container running %s", restored.ID, restored.Image, restored.State, second.Image)
	}
}

// registerLabelledImageCleanup removes, when the test ends, every image that
// carries label (name=value), tagged or not.
func registerLabelledImageCleanup(t *testing.T, cli *client.Client, label string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		images, err := cli.ImageList(cleanupCtx, client.ImageListOptions{All: true, Filters: client.Filters{}.Add("label", label)})
		if err != nil {
			t.Logf("best-effort image cleanup failed: %v", err)
			return
		}
		for _, item := range images.Items {
			if _, err := cli.ImageRemove(cleanupCtx, item.ID, client.ImageRemoveOptions{Force: true}); err != nil {
				t.Logf("best-effort image cleanup of %s failed: %v", item.ID, err)
			}
		}
	})
}

// logDockerImageStore logs the daemon's version and image store: whether a
// fully cached build keeps its image ID depends on them.
func logDockerImageStore(ctx context.Context, t *testing.T, cli *client.Client) {
	t.Helper()
	info, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		t.Logf("docker info: %v", err)
		return
	}
	t.Logf("Docker %s, storage driver %s %v", info.Info.ServerVersion, info.Info.Driver, info.Info.DriverStatus)
}

func TestDockerUpdateComposeExplicitEnvironmentFileIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)
	withTempUpdateStatusPath(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	project := fmt.Sprintf("linuxio-it-env-file-%d", time.Now().UnixNano())
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	environmentPath := filepath.Join(dir, "project.env")
	const service = "app"
	const firstImage = "alpine:3.20"
	const secondImage = "alpine:3.21"
	writeDockerUpdateComposeFile(t, composePath, service, "${LINUXIO_IT_IMAGE}")
	writeDockerUpdateComposeEnvironmentFile(t, environmentPath, "LINUXIO_IT_IMAGE", firstImage)
	target := ComposeTarget{
		Name:               project,
		ConfigFiles:        []string{composePath},
		EnvironmentFiles:   []string{environmentPath},
		IsolateEnvironment: true,
		WorkingDir:         dir,
	}
	registerDockerUpdateComposeTargetCleanup(t, target)
	if err := RunComposeProject(ctx, target, nil, "up", "-d"); err != nil {
		t.Fatalf("compose up with explicit environment file: %v", err)
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer cli.Close()
	initial := mustInspectComposeService(t, ctx, cli, project, service)
	resolved, resolvedService, managed, err := composeTargetForContainer(ctx, cli, initial)
	if err != nil {
		t.Fatalf("resolve Compose target: %v", err)
	}
	if !managed || resolvedService != service || !slices.Equal(resolved.EnvironmentFiles, []string{environmentPath}) {
		t.Fatalf("resolved target = %#v, service=%q, managed=%v; want explicit environment file", resolved, resolvedService, managed)
	}

	writeDockerUpdateComposeEnvironmentFile(t, environmentPath, "LINUXIO_IT_IMAGE", secondImage)
	result, _, err := newContainerUpdateResult(initial)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	updatedResult, err := updateComposeContainerWithProgress(ctx, cli, initial, resolved, service, result, nil)
	if err != nil {
		t.Fatalf("update Compose service with explicit environment file: %v", err)
	}
	if !updatedResult.Updated {
		t.Fatalf("update result = %#v, want replacement", updatedResult)
	}
	updated := mustInspectComposeService(t, ctx, cli, project, service)
	assertUpdatedComposeContainer(t, initial, updated, secondImage)
}

// TestDockerUpdateComposeShellOnlyInterpolationRefusesMutation verifies that
// update execution does not inherit unrelated worker environment variables.
func TestDockerUpdateComposeShellOnlyInterpolationRefusesMutation(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	project := fmt.Sprintf("linuxio-it-interpolation-%d", time.Now().UnixNano())
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	const service = "app"
	const image = "alpine:3.20"
	if err := os.WriteFile(composePath, []byte("services:\n  app:\n    image: ${LINUXIO_IT_IMAGE:?required}\n    command: [\"sleep\", \"infinity\"]\n"), 0o600); err != nil {
		t.Fatalf("write interpolated Compose file: %v", err)
	}
	target := ComposeTarget{Name: project, ConfigFiles: []string{composePath}, WorkingDir: dir}
	registerDockerUpdateComposeEnvironmentCleanup(t, target, "LINUXIO_IT_IMAGE", image)
	if err := runComposeProjectWithEnvironment(ctx, target, "LINUXIO_IT_IMAGE", image, "up", "-d"); err != nil {
		t.Fatalf("compose up with interpolated image: %v", err)
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer cli.Close()
	initial := mustInspectComposeService(t, ctx, cli, project, service)
	resolved, resolvedService, managed, err := composeTargetForContainer(ctx, cli, initial)
	if err != nil {
		t.Fatalf("resolve Compose target: %v", err)
	}
	if !managed || resolvedService != service {
		t.Fatalf("resolved target = %#v, service=%q, managed=%v; want Compose-managed app", resolved, resolvedService, managed)
	}
	result, _, err := newContainerUpdateResult(initial)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	if _, err := updateComposeContainerWithProgress(ctx, cli, initial, resolved, service, result, nil); err == nil || !strings.Contains(err.Error(), "validate Compose project") {
		t.Fatalf("shell-only interpolation update error = %v, want validation refusal", err)
	}

	after := mustInspectComposeService(t, ctx, cli, project, service)
	if after.ID != initial.ID || after.Image != initial.Image {
		t.Fatalf("interpolated Compose refusal changed container: before=%s/%s after=%s/%s", initial.ID, initial.Image, after.ID, after.Image)
	}
}

// TestDockerUpdateComposeScaledServiceRefusesMutation verifies that selecting
// one container from a scaled Compose service is rejected before pull/up. Both
// replicas must remain running with their original image and IDs.
func TestDockerUpdateComposeScaledServiceRefusesMutation(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_DOCKER_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_DOCKER_INTEGRATION=1 to run the Docker integration test")
	}
	requireDockerComposeIntegration(t)

	fixture := setupScaledComposeFixture(t)
	const service = "app"
	const secondImage = "alpine:3.21"
	writeDockerUpdateComposeFile(t, fixture.composePath, service, secondImage)
	selected := fixture.initial[0]
	result, _, err := newContainerUpdateResult(selected)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	if _, err := updateComposeContainerWithProgress(fixture.ctx, fixture.cli, selected, fixture.target, service, result, nil); err == nil || !strings.Contains(err.Error(), "replicas") {
		t.Fatalf("scaled Compose update error = %v, want replica-safety refusal", err)
	}

	after := mustInspectComposeServiceContainers(t, fixture.ctx, fixture.cli, fixture.target.Name, service)
	assertScaledComposeRefusal(t, fixture.initial, after)
}

type scaledComposeFixture struct {
	ctx         context.Context
	cli         *client.Client
	composePath string
	target      ComposeTarget
	initial     []container.InspectResponse
}

func setupScaledComposeFixture(t *testing.T) scaledComposeFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	project := fmt.Sprintf("linuxio-it-scaled-%d", time.Now().UnixNano())
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	const service = "app"
	const image = "alpine:3.20"
	writeDockerUpdateComposeFile(t, composePath, service, image)
	registerDockerUpdateComposeCleanup(t, project, composePath, dir)
	if err := composeUp(ctx, project, composePath, dir, true, nil); err != nil {
		t.Fatalf("compose up with %s: %v", image, err)
	}
	if err := runCompose(ctx, project, composePath, dir, nil, "up", "-d", "--scale", service+"=2", service); err != nil {
		t.Fatalf("scale Compose service: %v", err)
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	initial := mustInspectComposeServiceContainers(t, ctx, cli, project, service)
	if len(initial) != 2 {
		t.Fatalf("scaled Compose service has %d containers, want 2", len(initial))
	}
	assertScaledComposeContainersRunning(t, initial)
	target, resolvedService, managed, err := composeTargetForContainer(ctx, cli, initial[0])
	if err != nil {
		t.Fatalf("resolve Compose target: %v", err)
	}
	if !managed || resolvedService != service {
		t.Fatalf("resolved target = %#v, service=%q, managed=%v; want Compose-managed app", target, resolvedService, managed)
	}
	return scaledComposeFixture{ctx: ctx, cli: cli, composePath: composePath, target: target, initial: initial}
}

func assertScaledComposeContainersRunning(t *testing.T, containers []container.InspectResponse) {
	t.Helper()
	for _, inspect := range containers {
		if inspect.State == nil || !inspect.State.Running {
			t.Fatalf("scaled Compose container %s is not running", inspect.ID)
		}
	}
}

func assertScaledComposeRefusal(t *testing.T, initial, after []container.InspectResponse) {
	t.Helper()
	if len(after) != len(initial) {
		t.Fatalf("scaled Compose refusal changed replica count from %d to %d", len(initial), len(after))
	}
	initialByID := make(map[string]container.InspectResponse, len(initial))
	for _, inspect := range initial {
		initialByID[inspect.ID] = inspect
	}
	for _, inspect := range after {
		before, ok := initialByID[inspect.ID]
		if !ok {
			t.Fatalf("scaled Compose refusal replaced replica %s", inspect.ID)
		}
		if inspect.Image != before.Image || inspect.Config == nil || before.Config == nil || inspect.Config.Image != before.Config.Image {
			t.Fatalf("scaled Compose refusal changed replica %s image: before=%s/%s after=%s/%s", inspect.ID, before.Image, before.Config.Image, inspect.Image, inspect.Config.Image)
		}
	}
}

func writeDockerUpdateComposeFile(t *testing.T, composePath, service, image string) {
	t.Helper()
	content := fmt.Sprintf("services:\n  %s:\n    image: %s\n    command: [\"sleep\", \"infinity\"]\n", service, image)
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write Compose file: %v", err)
	}
}

func writeDockerUpdateComposeFileWithEnvironment(t *testing.T, composePath, service, image, environmentFile string) {
	t.Helper()
	content := fmt.Sprintf(
		"services:\n  %s:\n    image: %s\n    env_file:\n      - %s\n    command: [\"sleep\", \"infinity\"]\n",
		service,
		image,
		environmentFile,
	)
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write Compose file with environment: %v", err)
	}
}

func writeDockerUpdateComposeEnvironmentFile(t *testing.T, path, name, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(name+"="+value+"\n"), 0o600); err != nil {
		t.Fatalf("write Compose environment file: %v", err)
	}
}

func registerDockerUpdateComposeCleanup(t *testing.T, project, composePath, dir string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := composeDown(cleanupCtx, project, composePath, dir, true, nil); err != nil {
			t.Logf("best-effort Compose cleanup failed: %v", err)
		}
	})
}

func registerDockerUpdateComposeTargetCleanup(t *testing.T, target ComposeTarget) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := RunComposeProject(cleanupCtx, target, nil, "down", "--remove-orphans", "--volumes"); err != nil {
			t.Logf("best-effort Compose cleanup failed: %v", err)
		}
	})
}

func registerDockerUpdateComposeEnvironmentCleanup(t *testing.T, target ComposeTarget, name, value string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := runComposeProjectWithEnvironment(cleanupCtx, target, name, value, "down", "--remove-orphans", "--volumes"); err != nil {
			t.Logf("best-effort Compose cleanup failed: %v", err)
		}
	})
}

func runComposeProjectWithEnvironment(ctx context.Context, target ComposeTarget, name, value string, args ...string) error {
	commandArgs, err := composeCommandArgs(target, "json", args...)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", commandArgs...)
	cmd.Dir = target.WorkingDir
	cmd.Env = append(composeCommandEnvironment(), name+"="+value)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run docker compose with test environment: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func mustInspectComposeService(t *testing.T, ctx context.Context, cli *client.Client, project, service string) container.InspectResponse {
	t.Helper()
	inspect, err := inspectComposeService(ctx, cli, project, service)
	if err != nil {
		t.Fatalf("inspect Compose container: %v", err)
	}
	return inspect
}

func assertInitialComposeContainer(t *testing.T, inspect container.InspectResponse, project, service string) {
	t.Helper()
	if inspect.State == nil || !inspect.State.Running {
		t.Fatalf("initial container state = %#v, want running", inspect.State)
	}
	if inspect.Config == nil {
		t.Fatal("initial container configuration is unavailable")
	}
	labels := inspect.Config.Labels
	if labels["com.docker.compose.project"] != project || labels["com.docker.compose.service"] != service {
		t.Fatalf("initial container labels = %#v, want project %q and service %q", labels, project, service)
	}
}

func assertUpdatedComposeContainer(t *testing.T, initial, updated container.InspectResponse, image string) {
	t.Helper()
	if updated.ID == initial.ID {
		t.Fatalf("Compose update reused container %s; want replacement", initial.ID)
	}
	if updated.State == nil || !updated.State.Running {
		t.Fatalf("updated container state = %#v, want running", updated.State)
	}
	if updated.Config == nil || updated.Config.Image != image {
		got := "<nil>"
		if updated.Config != nil {
			got = updated.Config.Image
		}
		t.Fatalf("updated image reference = %q, want %q", got, image)
	}
	if updated.Image == initial.Image {
		t.Fatalf("Compose update kept image ID %s; want %s to differ", updated.Image, initial.Image)
	}
}

func requireDockerComposeIntegration(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("Docker CLI is unavailable; install docker and enable the integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput(); err != nil {
		t.Fatalf("Docker daemon is unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.CommandContext(ctx, "docker", "compose", "version").CombinedOutput(); err != nil {
		t.Fatalf("Docker Compose plugin is unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

func inspectComposeService(ctx context.Context, cli *client.Client, project, service string) (container.InspectResponse, error) {
	containers, err := inspectComposeServiceContainers(ctx, cli, project, service)
	if err != nil {
		return container.InspectResponse{}, err
	}
	if len(containers) != 1 {
		return container.InspectResponse{}, fmt.Errorf("found %d containers for Compose service", len(containers))
	}
	return containers[0], nil
}

func inspectComposeServiceContainers(ctx context.Context, cli *client.Client, project, service string) ([]container.InspectResponse, error) {
	filters := client.Filters{}.
		Add("label", "com.docker.compose.project="+project).
		Add("label", "com.docker.compose.service="+service)
	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, err
	}
	results := make([]container.InspectResponse, 0, len(list.Items))
	for _, item := range list.Items {
		result, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		results = append(results, result.Container)
	}
	return results, nil
}

func mustInspectComposeServiceContainers(t *testing.T, ctx context.Context, cli *client.Client, project, service string) []container.InspectResponse {
	t.Helper()
	containers, err := inspectComposeServiceContainers(ctx, cli, project, service)
	if err != nil {
		t.Fatalf("inspect Compose containers: %v", err)
	}
	return containers
}

func composeUp(ctx context.Context, projectName, configFile, workingDir string, removeOrphans bool, emitter ComposeLineEmitter) error {
	args := []string{"up", "-d"}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, args...)
}

func composeDown(ctx context.Context, projectName, configFile, workingDir string, removeOrphans bool, emitter ComposeLineEmitter) error {
	args := []string{"down"}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, args...)
}

func runCompose(ctx context.Context, projectName, configFile, workingDir string, emitter ComposeLineEmitter, args ...string) error {
	return RunComposeProject(ctx, ComposeTarget{
		Name:        projectName,
		ConfigFiles: []string{configFile},
		WorkingDir:  workingDir,
	}, emitter, args...)
}
