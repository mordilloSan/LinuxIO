package docker

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestFindContainerIDForProxyTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		containers []container.Summary
		wantID     string
		wantErr    string
	}{
		{
			name:   "matches exact container name",
			target: "linuxio-grafana",
			containers: []container.Summary{
				{ID: "abc123", Names: []string{"/linuxio-grafana"}},
			},
			wantID: "abc123",
		},
		{
			name:   "matches compose service name",
			target: "grafana",
			containers: []container.Summary{
				{
					ID:     "def456",
					Names:  []string{"/linuxio-grafana"},
					Labels: map[string]string{"com.docker.compose.service": "grafana"},
				},
			},
			wantID: "def456",
		},
		{
			name:   "prefers exact container name over compose service match",
			target: "grafana",
			containers: []container.Summary{
				{
					ID:     "service456",
					Names:  []string{"/linuxio-grafana"},
					Labels: map[string]string{"com.docker.compose.service": "grafana"},
				},
				{
					ID:    "exact123",
					Names: []string{"/grafana"},
				},
			},
			wantID: "exact123",
		},
		{
			name:   "errors on ambiguous compose service match",
			target: "grafana",
			containers: []container.Summary{
				{
					ID:     "one",
					Names:  []string{"/linuxio-grafana-a"},
					Labels: map[string]string{"com.docker.compose.service": "grafana"},
				},
				{
					ID:     "two",
					Names:  []string{"/linuxio-grafana-b"},
					Labels: map[string]string{"com.docker.compose.service": "grafana"},
				},
			},
			wantErr: "multiple compose services",
		},
		{
			name:       "errors when not found",
			target:     "grafana",
			containers: []container.Summary{{ID: "abc123", Names: []string{"/prometheus"}}},
			wantErr:    "container not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotID, err := findContainerIDForProxyTarget(tt.target, tt.containers)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if gotID != tt.wantID {
				t.Fatalf("expected id %q, got %q", tt.wantID, gotID)
			}
		})
	}
}
