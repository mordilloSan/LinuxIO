package docker

import (
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
)

func TestDockerVolumesFromSDKPreservesListFieldsAndSorts(t *testing.T) {
	volumes := dockerVolumesFromSDK([]volume.Volume{
		{
			ClusterVolume: &volume.ClusterVolume{ID: "cluster-volume"},
			CreatedAt:     "2026-07-30T10:00:00Z",
			Driver:        "local",
			Labels:        map[string]string{"purpose": "cache"},
			Mountpoint:    "/var/lib/docker/volumes/zeta/_data",
			Name:          "zeta",
			Options:       map[string]string{"type": "none"},
			Scope:         "local",
			Status:        map[string]any{"state": "ready", "nested": map[string]any{"ok": true}},
			UsageData:     &volume.UsageData{RefCount: 0, Size: -1},
		},
		{Name: "alpha", Driver: "local"},
	})

	if got, want := []string{volumes[0].Name, volumes[1].Name}, []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("volume order = %v, want %v", got, want)
	}
	got := volumes[1]
	if got.CreatedAt == nil || *got.CreatedAt != "2026-07-30T10:00:00Z" {
		t.Fatalf("CreatedAt = %v, want input value", got.CreatedAt)
	}
	if got.Scope == nil || *got.Scope != "local" {
		t.Fatalf("Scope = %v, want local", got.Scope)
	}
	if got.UsageData == nil || got.UsageData.RefCount != 0 || got.UsageData.Size != -1 {
		t.Fatalf("UsageData = %#v, want zero reference count and unavailable size", got.UsageData)
	}
	if !reflect.DeepEqual(got.Status, map[string]any{"state": "ready", "nested": map[string]any{"ok": true}}) {
		t.Fatalf("Status = %#v", got.Status)
	}
	if got.ClusterVolume == nil || got.ClusterVolume["ID"] != "cluster-volume" {
		t.Fatalf("ClusterVolume = %#v, want opaque SDK projection", got.ClusterVolume)
	}
}

func TestDockerVolumesFromSDKReturnsEmptySlice(t *testing.T) {
	for _, input := range [][]volume.Volume{nil, {}} {
		got := dockerVolumesFromSDK(input)
		if got == nil || len(got) != 0 {
			t.Fatalf("dockerVolumesFromSDK(%#v) = %#v, want non-nil empty slice", input, got)
		}
	}
}

func TestDockerNetworkFromSDKPreservesFieldsAndHandlesOptionalAddresses(t *testing.T) {
	created := time.Date(2026, time.July, 30, 10, 15, 0, 123, time.UTC)
	summary := network.Summary{Network: network.Network{
		Attachable: true,
		ConfigOnly: true,
		Created:    created,
		Driver:     "bridge",
		EnableIPv4: false,
		EnableIPv6: true,
		ID:         "network-id",
		Ingress:    true,
		Internal:   false,
		IPAM: network.IPAM{
			Driver:  "default",
			Options: map[string]string{"driver-option": "value"},
			Config: []network.IPAMConfig{{
				Subnet:     netip.MustParsePrefix("172.20.0.0/16"),
				IPRange:    netip.MustParsePrefix("172.20.1.0/24"),
				Gateway:    netip.MustParseAddr("172.20.0.1"),
				AuxAddress: map[string]netip.Addr{"host": netip.MustParseAddr("172.20.0.2")},
			}},
		},
		Labels:  map[string]string{"managed": "true"},
		Name:    "network-a",
		Options: map[string]string{"com.docker.network.bridge.name": "br-a"},
		Scope:   "local",
	}}
	inspect := network.Inspect{Containers: map[string]network.EndpointResource{
		"container-id": {
			EndpointID:  "endpoint-id",
			IPv4Address: netip.MustParsePrefix("172.20.0.2/16"),
			MacAddress:  network.HardwareAddr{0x02, 0x42, 0xac, 0x14, 0x00, 0x02},
			Name:        "container-a",
		},
		"empty-container": {Name: "container-b"},
	}}

	got := dockerNetworkFromSDK(summary, inspect)
	if got.Created == nil || *got.Created != created.Format(time.RFC3339Nano) {
		t.Fatalf("Created = %v, want %s", got.Created, created.Format(time.RFC3339Nano))
	}
	if !got.Attachable || !got.ConfigOnly || !got.Ingress || got.EnableIPv4 == nil || *got.EnableIPv4 || got.EnableIPv6 == nil || !*got.EnableIPv6 || got.Internal == nil || *got.Internal {
		t.Fatalf("network flags were not preserved: %#v", got)
	}
	if got.IPAM == nil || got.IPAM.Driver != "default" || got.IPAM.Options["driver-option"] != "value" || len(got.IPAM.Config) != 1 {
		t.Fatalf("IPAM = %#v, want complete SDK projection", got.IPAM)
	}
	config := got.IPAM.Config[0]
	if config.Subnet != "172.20.0.0/16" || config.IPRange != "172.20.1.0/24" || config.Gateway != "172.20.0.1" || config.AuxiliaryAddresses["host"] != "172.20.0.2" {
		t.Fatalf("IPAM config = %#v", config)
	}
	container := got.Containers["container-id"]
	if container.EndpointID != "endpoint-id" || container.IPv4Address == nil || *container.IPv4Address != "172.20.0.2/16" || container.IPv6Address != nil || container.MACAddress == nil || *container.MACAddress != "02:42:ac:14:00:02" {
		t.Fatalf("container = %#v, want endpoint data and omitted zero IPv6", container)
	}
	if empty := got.Containers["empty-container"]; empty.MACAddress != nil || empty.IPv4Address != nil || empty.IPv6Address != nil {
		t.Fatalf("empty endpoint optional addresses = %#v, want omitted", empty)
	}
}

func TestDockerNetworkFromSDKOmitsEmptyOptionalData(t *testing.T) {
	got := dockerNetworkFromSDK(network.Summary{Network: network.Network{Name: "empty"}}, network.Inspect{})
	if got.Created != nil || got.IPAM != nil || got.Containers != nil {
		t.Fatalf("optional network values = %#v, want omitted", got)
	}
	if got.EnableIPv4 == nil || got.EnableIPv6 == nil || got.Internal == nil {
		t.Fatalf("boolean values must retain false instead of becoming absent: %#v", got)
	}
}
