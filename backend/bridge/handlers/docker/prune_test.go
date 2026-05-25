package docker

import "testing"

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
