package watchtower

import (
	"reflect"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []Result
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "no containers matched filter",
			output: "no containers matched filter\n",
			want:   nil,
		},
		{
			name:   "fresh container",
			output: "nginx (docker.io/library/nginx:latest): Fresh\n",
			want: []Result{
				{Name: "nginx", Image: "docker.io/library/nginx:latest", State: StateFresh},
			},
		},
		{
			name:   "stale container",
			output: "homeassistant (ghcr.io/home-assistant/home-assistant:stable): Stale\n",
			want: []Result{
				{Name: "homeassistant", Image: "ghcr.io/home-assistant/home-assistant:stable", State: StateStale},
			},
		},
		{
			name:   "failed with error message",
			output: "broken (registry.example.com/app:1.0): Failed Error: manifest unknown\n",
			want: []Result{
				{Name: "broken", Image: "registry.example.com/app:1.0", State: StateFailed, Err: "manifest unknown"},
			},
		},
		{
			name: "mixed states with blank and stray lines",
			output: "nginx (nginx:latest): Updated\n" +
				"\n" +
				"time=\"2026-06-12\" level=info msg=\"stray log line\"\n" +
				"redis (redis:7): Fresh\n",
			want: []Result{
				{Name: "nginx", Image: "nginx:latest", State: StateUpdated},
				{Name: "redis", Image: "redis:7", State: StateFresh},
			},
		},
		{
			name:   "leading slash in name is trimmed",
			output: "/legacy (alpine:3): Skipped Error: cooldown active\n",
			want: []Result{
				{Name: "legacy", Image: "alpine:3", State: StateSkipped, Err: "cooldown active"},
			},
		},
		{
			name:   "name with dot",
			output: "app.service (example.com/app:2): Restarted\n",
			want: []Result{
				{Name: "app.service", Image: "example.com/app:2", State: StateRestarted},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePorcelain(tc.output)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParsePorcelain = %#v, want %#v", got, tc.want)
			}
		})
	}
}
