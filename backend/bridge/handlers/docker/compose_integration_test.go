package docker

import (
	"context"
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
	cli, err := getClient()
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer releaseClient(cli)
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
	target := composeProjectTarget{
		Name:               project,
		ConfigFiles:        []string{composePath},
		EnvironmentFiles:   []string{environmentPath},
		IsolateEnvironment: true,
		WorkingDir:         dir,
	}
	registerDockerUpdateComposeTargetCleanup(t, target)
	if err := runComposeProject(ctx, target, nil, "up", "-d"); err != nil {
		t.Fatalf("compose up with explicit environment file: %v", err)
	}

	cli, err := getClient()
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer releaseClient(cli)
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
	target := composeProjectTarget{Name: project, ConfigFiles: []string{composePath}, WorkingDir: dir}
	registerDockerUpdateComposeEnvironmentCleanup(t, target, "LINUXIO_IT_IMAGE", image)
	if err := runComposeProjectWithEnvironment(ctx, target, "LINUXIO_IT_IMAGE", image, "up", "-d"); err != nil {
		t.Fatalf("compose up with interpolated image: %v", err)
	}

	cli, err := getClient()
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer releaseClient(cli)
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
	target      composeProjectTarget
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
	cli, err := getClient()
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { releaseClient(cli) })
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

func registerDockerUpdateComposeTargetCleanup(t *testing.T, target composeProjectTarget) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := runComposeProject(cleanupCtx, target, nil, "down", "--remove-orphans", "--volumes"); err != nil {
			t.Logf("best-effort Compose cleanup failed: %v", err)
		}
	})
}

func registerDockerUpdateComposeEnvironmentCleanup(t *testing.T, target composeProjectTarget, name, value string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := runComposeProjectWithEnvironment(cleanupCtx, target, name, value, "down", "--remove-orphans", "--volumes"); err != nil {
			t.Logf("best-effort Compose cleanup failed: %v", err)
		}
	})
}

func runComposeProjectWithEnvironment(ctx context.Context, target composeProjectTarget, name, value string, args ...string) error {
	commandArgs, err := composeCommandArgs(target, args...)
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
