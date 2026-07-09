package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestValidateStackDirectoryPreservesExistingContents(t *testing.T) {
	dirPath := t.TempDir()
	markerPath := filepath.Join(dirPath, ".linuxio-write-test")
	markerContent := []byte("existing user data")
	if err := os.WriteFile(markerPath, markerContent, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	setDirectoryModTime(t, dirPath)
	before, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat directory before validation: %v", err)
	}

	resultAny, err := ValidateStackDirectory(context.Background(), dirPath)
	result := requireDirectoryValidationResult(t, resultAny, err)
	if !result.Exists || !result.IsDirectory || !result.CanWrite || !result.Valid {
		t.Fatalf("ValidateStackDirectory() result = %#v, want writable existing directory", result)
	}

	gotContent, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker after validation: %v", err)
	}
	if string(gotContent) != string(markerContent) {
		t.Fatalf("marker content after validation = %q, want %q", gotContent, markerContent)
	}
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("stat marker after validation: %v", err)
	}
	if got := markerInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("marker mode after validation = %o, want 600", got)
	}

	after, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat directory after validation: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("directory mtime changed during validation: before %v, after %v", before.ModTime(), after.ModTime())
	}
}

func TestValidateStackDirectoryDoesNotCreateMissingTarget(t *testing.T) {
	parentPath := t.TempDir()
	targetPath := filepath.Join(parentPath, "new-stack")

	setDirectoryModTime(t, parentPath)
	before, err := os.Stat(parentPath)
	if err != nil {
		t.Fatalf("stat parent before validation: %v", err)
	}

	resultAny, err := ValidateStackDirectory(context.Background(), targetPath)
	result := requireDirectoryValidationResult(t, resultAny, err)
	if result.Exists || !result.CanCreate || !result.CanWrite || !result.Valid {
		t.Fatalf("ValidateStackDirectory() result = %#v, want creatable missing directory", result)
	}
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("missing target after validation: Lstat error = %v, want not exist", err)
	}

	after, err := os.Stat(parentPath)
	if err != nil {
		t.Fatalf("stat parent after validation: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("parent mtime changed during validation: before %v, after %v", before.ModTime(), after.ModTime())
	}
}

func TestValidateStackDirectoryRejectsDanglingSymlink(t *testing.T) {
	parentPath := t.TempDir()
	targetPath := filepath.Join(parentPath, "stack-link")
	if err := os.Symlink(filepath.Join(parentPath, "missing"), targetPath); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}

	resultAny, err := ValidateStackDirectory(context.Background(), targetPath)
	result := requireDirectoryValidationResult(t, resultAny, err)
	if !result.Exists || result.Valid {
		t.Fatalf("ValidateStackDirectory() result = %#v, want existing invalid path", result)
	}
	if _, err := os.Lstat(targetPath); err != nil {
		t.Fatalf("dangling symlink was modified during validation: %v", err)
	}
}

func requireDirectoryValidationResult(
	t *testing.T,
	resultAny any,
	err error,
) apischema.DirectoryValidationResult {
	t.Helper()
	if err != nil {
		t.Fatalf("ValidateStackDirectory() error = %v", err)
	}
	result, ok := resultAny.(apischema.DirectoryValidationResult)
	if !ok {
		t.Fatalf("ValidateStackDirectory() type = %T, want DirectoryValidationResult", resultAny)
	}
	return result
}

func setDirectoryModTime(t *testing.T, dirPath string) {
	t.Helper()
	fixedTime := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(dirPath, fixedTime, fixedTime); err != nil {
		t.Fatalf("set directory timestamps: %v", err)
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
