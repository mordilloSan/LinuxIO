package docker

import "testing"

func TestVolumePruneFilters(t *testing.T) {
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
			args := volumePruneFilters(tt.apiVersion)
			if args.Contains("all") != tt.expectAll {
				t.Fatalf("all filter presence = %v, want %v", args.Contains("all"), tt.expectAll)
			}

			if tt.expectAll {
				values := args.Get("all")
				if len(values) != 1 || values[0] != "true" {
					t.Fatalf("all filter values = %v, want [true]", values)
				}
			}
		})
	}
}
