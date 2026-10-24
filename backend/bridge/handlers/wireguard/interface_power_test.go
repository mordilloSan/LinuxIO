package wireguard

import (
	"context"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestUpAndDownInterfaceRunWGQuick(t *testing.T) {
	env := newWireGuardTestEnv(t)

	up, err := UpInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("UpInterface returned error: %v", err)
	}
	upResult, ok := up.(map[string]any)
	if !ok || upResult["status"] != "on" {
		t.Fatalf("UpInterface result = %#v, want status on", up)
	}

	down, err := DownInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("DownInterface returned error: %v", err)
	}
	downResult, ok := down.(map[string]any)
	if !ok || downResult["status"] != "off" {
		t.Fatalf("DownInterface result = %#v, want status off", down)
	}

	want := []wgQuickCall{{action: "up", name: "wgtest"}, {action: "down", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, want) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, want)
	}
}
