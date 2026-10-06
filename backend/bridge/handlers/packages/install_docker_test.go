package packages

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// withDockerInstall serves script as the Docker convenience script, reports
// whether docker is already on PATH, and records service and detect steps.
func withDockerInstall(t *testing.T, dockerInstalled bool, handler http.HandlerFunc) *[]string {
	t.Helper()
	server := httptest.NewServer(handler)
	originalURL := dockerInstallScriptURL
	originalLookPath := capabilityCommandLookPath
	originalFamily := capabilityDistroFamily
	originalEnable := capabilityEnableService
	originalStart := capabilityStartService
	originalWait := capabilityWaitServiceActive
	originalDetect := capabilityDetectWithRetry
	t.Cleanup(func() {
		server.Close()
		dockerInstallScriptURL = originalURL
		capabilityCommandLookPath = originalLookPath
		capabilityDistroFamily = originalFamily
		capabilityEnableService = originalEnable
		capabilityStartService = originalStart
		capabilityWaitServiceActive = originalWait
		capabilityDetectWithRetry = originalDetect
	})

	var order []string
	dockerInstallScriptURL = server.URL
	capabilityCommandLookPath = func(name string) (string, error) {
		if name == "docker" {
			if dockerInstalled {
				return "/usr/bin/docker", nil
			}
			return "", exec.ErrNotFound
		}
		order = append(order, "run:"+name)
		return exec.LookPath(name)
	}
	capabilityDistroFamily = func() string { return "debian" }
	capabilityEnableService = func(_ context.Context, service string) error {
		order = append(order, "enable:"+service)
		return nil
	}
	capabilityStartService = func(_ context.Context, service string) error {
		order = append(order, "start:"+service)
		return nil
	}
	capabilityWaitServiceActive = func(_ context.Context, service string, _ time.Duration) error {
		order = append(order, "wait:"+service)
		return nil
	}
	capabilityDetectWithRetry = func(_ context.Context, spec system.CapabilitySpec, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}
	return &order
}

func serveScript(script string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(script))
	}
}

func TestInstallCapabilityRunsDockerScriptBeforeServiceActions(t *testing.T) {
	order := withDockerInstall(t, false, serveScript("echo \"script=$0\"\necho installing docker\n"))

	registry := bridgetask.NewTaskService()
	task, err := registry.Create("system.install_capability", nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	result, err := installCapability(context.Background(), task, "docker")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if !result.Available {
		t.Fatal("result.Available = false, want true")
	}
	want := []string{"run:sh", "enable:docker.service", "start:docker.service", "wait:docker.service", "detect:docker"}
	if !slices.Equal(*order, want) {
		t.Fatalf("operation order = %v, want %v", *order, want)
	}

	_, replay, unsubscribe := task.SubscribeWithReplay(64)
	defer unsubscribe()
	var stdout, status strings.Builder
	for _, event := range replay {
		progress, ok := event.Progress.(bridgetask.TaskProgress)
		if !ok {
			continue
		}
		detail, ok := progress.Detail.(InstallCapabilityProgress)
		if !ok || detail.Output == nil {
			continue
		}
		switch detail.Output.Stream {
		case "stdout":
			stdout.WriteString(detail.Output.Text)
		case "status":
			status.WriteString(detail.Output.Text)
			status.WriteByte('\n')
		}
	}
	if !strings.Contains(stdout.String(), "installing docker\n") {
		t.Fatalf("stdout replay = %q, want script output", stdout.String())
	}
	if !strings.Contains(status.String(), "sha256 ") {
		t.Fatalf("status replay = %q, want the script checksum", status.String())
	}
	_, scriptLine, _ := strings.Cut(stdout.String(), "script=")
	scriptPath, _, _ := strings.Cut(scriptLine, "\n")
	if _, err := os.Stat(scriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script %q still present after install: %v", scriptPath, err)
	}
}

func TestInstallCapabilitySkipsDockerScriptWhenDockerIsInstalled(t *testing.T) {
	order := withDockerInstall(t, true, func(http.ResponseWriter, *http.Request) {
		t.Error("install script downloaded although docker is installed")
	})

	if _, err := installCapability(context.Background(), nil, "docker"); err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	want := []string{"enable:docker.service", "start:docker.service", "wait:docker.service", "detect:docker"}
	if !slices.Equal(*order, want) {
		t.Fatalf("operation order = %v, want %v", *order, want)
	}
}

func TestInstallCapabilityStopsWhenDockerScriptFails(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "download",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			want: "503",
		},
		{
			name:    "empty",
			handler: serveScript(""),
			want:    "script is empty",
		},
		{
			name:    "script",
			handler: serveScript("echo unsupported distro >&2\nexit 1\n"),
			want:    "unsupported distro",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := withDockerInstall(t, false, test.handler)

			_, err := installCapability(context.Background(), nil, "docker")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
			if slices.ContainsFunc(*order, func(step string) bool { return !strings.HasPrefix(step, "run:") }) {
				t.Fatalf("operation order = %v, want no service actions after failure", *order)
			}
		})
	}
}
