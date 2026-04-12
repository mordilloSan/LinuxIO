package semver

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{name: "newer patch", latest: "v1.2.4", current: "v1.2.3", want: true},
		{name: "older patch", latest: "v1.2.2", current: "v1.2.3", want: false},
		{name: "release newer than dev", latest: "v1.2.3", current: "dev-v1.2.3", want: true},
		{name: "same release", latest: "v1.2.3", current: "v1.2.3", want: false},
		{name: "shorter version older", latest: "v1.2", current: "v1.2.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewer(tt.latest, tt.current); got != tt.want {
				t.Fatalf("IsNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}
