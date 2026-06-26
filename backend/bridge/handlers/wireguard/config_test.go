package wireguard

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestAddNATHooksAddsExpectedPostUpAndPostDown(t *testing.T) {
	cfg := WireGuardConfig{
		PostUp:   []string{"echo custom up"},
		PostDown: []string{"echo custom down"},
	}

	if !addNATHooks(&cfg, "eth0", "10.7.0.1/24") {
		t.Fatal("addNATHooks reported no change")
	}

	for _, hook := range natPostUpHooks("eth0", "10.7.0.1/24") {
		if !slices.Contains(cfg.PostUp, hook) {
			t.Fatalf("PostUp missing %q in %v", hook, cfg.PostUp)
		}
	}
	for _, hook := range natPostDownHooks("eth0", "10.7.0.1/24") {
		if !slices.Contains(cfg.PostDown, hook) {
			t.Fatalf("PostDown missing %q in %v", hook, cfg.PostDown)
		}
	}
	if !slices.Contains(cfg.PostUp, "echo custom up") || !slices.Contains(cfg.PostDown, "echo custom down") {
		t.Fatalf("custom hooks were not preserved: PostUp=%v PostDown=%v", cfg.PostUp, cfg.PostDown)
	}
	if addNATHooks(&cfg, "eth0", "10.7.0.1/24") {
		t.Fatal("addNATHooks reported change on second call")
	}
}

func TestWriteWireGuardConfigPersistsPostHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := WireGuardConfig{
		PrivateKey: "server-private-key",
		Address:    []string{"10.7.0.1/24"},
		ListenPort: 51820,
		PostUp:     natPostUpHooks("eth0", "10.7.0.1/24"),
		PostDown:   natPostDownHooks("eth0", "10.7.0.1/24"),
	}

	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}

	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if !slices.Equal(got.PostUp, cfg.PostUp) {
		t.Fatalf("PostUp = %v, want %v", got.PostUp, cfg.PostUp)
	}
	if !slices.Equal(got.PostDown, cfg.PostDown) {
		t.Fatalf("PostDown = %v, want %v", got.PostDown, cfg.PostDown)
	}
}
