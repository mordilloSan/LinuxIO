package docker

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestExtractHostPortsTreatsTCPAndUDPAsDistinct(t *testing.T) {
	svc := map[string]any{
		"ports": []any{
			"53:53/tcp",
			"53:53/udp",
			map[string]any{
				"target":    67,
				"published": "67",
				"protocol":  "udp",
			},
			map[string]any{
				"target":    80,
				"published": "8080",
			},
		},
	}

	got := extractHostPorts(svc)
	want := []string{"53/tcp", "53/udp", "67/udp", "8080/tcp"}

	if len(got) != len(want) {
		t.Fatalf("extractHostPorts() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractHostPorts()[%d] = %q, want %q (all: %#v)", i, got[i], want[i], got)
		}
	}
}

func TestValidateComposeFileAllowsPiHoleDNSProtocols(t *testing.T) {
	content := `
services:
  pihole:
    image: pihole/pihole:latest
    ports:
      - "53:53/tcp"
      - "53:53/udp"
`

	resultAny, err := ValidateComposeFile(context.Background(), content)
	if err != nil {
		t.Fatalf("ValidateComposeFile() error = %v", err)
	}
	result, ok := resultAny.(apischema.ValidateComposeResponse)
	if !ok {
		t.Fatalf("ValidateComposeFile() type = %T, want ValidationResult", resultAny)
	}
	if !result.Valid {
		t.Fatalf("ValidateComposeFile() valid = false, errors = %#v", result.Errors)
	}
}

func TestValidateComposeFileRejectsDuplicateHostPortProtocol(t *testing.T) {
	content := `
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80/tcp"
      - "8080:8080/tcp"
`

	resultAny, err := ValidateComposeFile(context.Background(), content)
	if err != nil {
		t.Fatalf("ValidateComposeFile() error = %v", err)
	}
	result, ok := resultAny.(apischema.ValidateComposeResponse)
	if !ok {
		t.Fatalf("ValidateComposeFile() type = %T, want ValidationResult", resultAny)
	}
	if result.Valid {
		t.Fatalf("ValidateComposeFile() valid = true, want duplicate port error")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("ValidateComposeFile() errors empty")
	}
}

func TestDiscoverComposeProjectsIncludesContainers(t *testing.T) {
	projects := discoverComposeProjectsFromContainers(
		context.Background(),
		nil,
		[]container.Summary{
			{
				ID:    "abc123",
				Image: "ghcr.io/immich-app/immich-server:release",
				Labels: map[string]string{
					"com.docker.compose.project": "immich",
					"com.docker.compose.service": "server",
				},
				Names:  []string{"/immich-server"},
				Ports:  []container.PortSummary{{PrivatePort: 2283, PublicPort: 2283, Type: "tcp"}},
				State:  container.StateRunning,
				Status: "Up 2 minutes",
			},
		},
	)

	project, ok := projects["immich"]
	if !ok {
		t.Fatalf("missing compose project in %#v", projects)
	}
	if len(project.Containers) != 1 {
		t.Fatalf("containers len = %d, want 1", len(project.Containers))
	}

	got := project.Containers[0]
	if got.ID != "abc123" {
		t.Fatalf("container ID = %q, want abc123", got.ID)
	}
	if got.Names[0] != "/immich-server" {
		t.Fatalf("container name = %q, want /immich-server", got.Names[0])
	}
	if got.Image != "ghcr.io/immich-app/immich-server:release" {
		t.Fatalf("container image = %q", got.Image)
	}
	if got.State != "running" {
		t.Fatalf("container state = %q, want running", got.State)
	}
	if len(got.Ports) != 1 || got.Ports[0].PrivatePort != 2283 {
		t.Fatalf("container ports = %#v", got.Ports)
	}
}
