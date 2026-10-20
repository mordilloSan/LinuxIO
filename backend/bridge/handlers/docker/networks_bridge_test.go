package docker

import (
	"testing"

	"github.com/moby/moby/api/types/network"
)

func TestBridgeInterfaceName(t *testing.T) {
	cases := []struct {
		name string
		nw   network.Summary
		want string
	}{
		{"non-bridge driver", network.Summary{Driver: "overlay", ID: "0123456789abcdef"}, ""},
		{"custom name option", network.Summary{Driver: "bridge", ID: "0123456789abcdef", Options: map[string]string{"com.docker.network.bridge.name": "br-immich"}}, "br-immich"},
		{"default network", network.Summary{Driver: "bridge", Name: "bridge", ID: "0123456789abcdef"}, "docker0"},
		{"generated name", network.Summary{Driver: "bridge", Name: "app_default", ID: "0123456789abcdef"}, "br-0123456789ab"},
		{"short id", network.Summary{Driver: "bridge", ID: "abc"}, ""},
	}
	for _, tc := range cases {
		if got := bridgeInterfaceName(tc.nw); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
