package network

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenBackendPrefersNetplanOverLowerPriorityBackends(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, filepath.Join(env.NetplanDir, "01-eth0.yaml"), `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
`)
	mustWriteFile(t, filepath.Join(env.NMConnectionDir, "eth0.nmconnection"), `
[connection]
id=eth0
interface-name=eth0
type=802-3-ethernet
`)
	backend, err := OpenBackend(env, "eth0")
	if err != nil {
		t.Fatalf("OpenBackend: %v", err)
	}
	if backend.Name() != "netplan" {
		t.Fatalf("expected netplan backend, got %s", backend.Name())
	}
}

func TestOpenBackendReturnsAmbiguousNetplanError(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, filepath.Join(env.NetplanDir, "01-a.yaml"), `
network:
  version: 2
  ethernets:
    eth0: { dhcp4: true }
`)
	mustWriteFile(t, filepath.Join(env.NetplanDir, "02-b.yaml"), `
network:
  version: 2
  ethernets:
    eth0: { dhcp4: false }
`)
	_, err := OpenBackend(env, "eth0")
	if err == nil || !strings.Contains(err.Error(), "ambiguous netplan configuration") {
		t.Fatalf("expected ambiguous netplan error, got %v", err)
	}
}

func TestOpenBackendReturnsUnsupportedWhenNoBackendMatches(t *testing.T) {
	env, _, _ := testEnv(t)
	_, err := OpenBackend(env, "eth9")
	if err == nil || !strings.Contains(err.Error(), "unsupported network backend") {
		t.Fatalf("expected unsupported backend error, got %v", err)
	}
}
