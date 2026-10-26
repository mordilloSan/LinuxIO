package docker

import (
	"errors"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestVolumePruneOptions(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		expectAll  bool
	}{
		{name: "unknown API version", apiVersion: "", expectAll: false},
		{name: "API 1.41", apiVersion: "1.41", expectAll: false},
		{name: "API 1.42", apiVersion: "1.42", expectAll: true},
		{name: "API 1.51", apiVersion: "1.51", expectAll: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := volumePruneOptions(tt.apiVersion)
			if opts.All != tt.expectAll {
				t.Fatalf("all option = %v, want %v", opts.All, tt.expectAll)
			}
		})
	}
}

func TestWithPrunedNamesEarlierSteps(t *testing.T) {
	failed := errors.New("volume prune failed")
	if got := withPruned(failed, &apischema.DockerSystemPruneResponse{}); got.Error() != failed.Error() {
		t.Fatalf("withPruned with nothing pruned = %v, want the original error", got)
	}

	got := withPruned(failed, &apischema.DockerSystemPruneResponse{ContainersDeleted: []string{"a", "b"}, SpaceReclaimed: 42})
	if !errors.Is(got, failed) || !strings.Contains(got.Error(), "already pruned: 2 containers, 0 images, 0 networks, 0 volumes, 42 bytes reclaimed") {
		t.Fatalf("withPruned = %v, want wrapped error naming 2 containers and 42 bytes", got)
	}
}
