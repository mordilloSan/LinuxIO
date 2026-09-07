package docker

import "testing"

func TestDockerMajorVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected uint64
	}{
		{name: "standard semver", version: "25.0.1", expected: 25},
		{name: "prerelease semver", version: "26.0.0-beta.1", expected: 26},
		{name: "build metadata semver", version: "27.0.0+build.1", expected: 27},
		{name: "missing minor version", version: "25", expected: 0},
		{name: "missing patch version", version: "25.0", expected: 0},
		{name: "invalid major version", version: "v25.0.1", expected: 0},
		{name: "invalid minor version", version: "25.x.1", expected: 0},
		{name: "invalid patch version", version: "25.0.x", expected: 0},
		{name: "empty version", version: "", expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dockerMajorVersion(tt.version); got != tt.expected {
				t.Fatalf("dockerMajorVersion(%q) = %d, want %d", tt.version, got, tt.expected)
			}
		})
	}
}
