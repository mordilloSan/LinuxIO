package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	const service = "app"
	const firstImage = "alpine:3.20"
	const secondImage = "alpine:3.21"
	writeDockerUpdateComposeFile(t, composePath, service, firstImage)
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

	writeDockerUpdateComposeFile(t, composePath, service, secondImage)
	updateResult, _, err := newContainerUpdateResult(initial)
	if err != nil {
		t.Fatalf("build update result: %v", err)
	}
	updatedResult, err := updateComposeContainer(ctx, cli, initial, resolved, service, updateResult)
	if err != nil {
		t.Fatalf("production Compose update to %s: %v", secondImage, err)
	}
	if !updatedResult.Updated || updatedResult.NewImageID == initial.Image {
		t.Fatalf("production Compose update result = %#v, want replacement with a new image", updatedResult)
	}
	updated := mustInspectComposeService(t, ctx, cli, project, service)
	assertUpdatedComposeContainer(t, initial, updated, secondImage)
}

func writeDockerUpdateComposeFile(t *testing.T, composePath, service, image string) {
	t.Helper()
	content := fmt.Sprintf("services:\n  %s:\n    image: %s\n    command: [\"sleep\", \"infinity\"]\n", service, image)
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write Compose file: %v", err)
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
	filters := client.Filters{}.
		Add("label", "com.docker.compose.project="+project).
		Add("label", "com.docker.compose.service="+service)
	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return container.InspectResponse{}, err
	}
	if len(list.Items) != 1 {
		return container.InspectResponse{}, fmt.Errorf("found %d containers for Compose service", len(list.Items))
	}
	result, err := cli.ContainerInspect(ctx, list.Items[0].ID, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return result.Container, nil
}
