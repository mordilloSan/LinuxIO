package host

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMemoryStat(t *testing.T, root, dir, content string) {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "memory.stat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupHeldBytesLeavesOutPageCache(t *testing.T) {
	root := t.TempDir()
	// file, active_file and slab_reclaimable are cache and must not count.
	writeMemoryStat(t, root, "machine.slice", "anon 1000\nfile 5000\nkernel_stack 10\npagetables 20\npercpu 1\nslab_reclaimable 300\nslab_unreclaimable 9\nshmem 60\nactive_file 4000\n")
	// Both Docker cgroup drivers on one host, plus a non-container unit that
	// must be ignored.
	writeMemoryStat(t, root, "system.slice/docker-abc.scope", "anon 100\nfile 900\n")
	writeMemoryStat(t, root, "system.slice/docker-def.scope", "anon 200\nshmem 5\n")
	writeMemoryStat(t, root, "system.slice/docker.service", "anon 7777\n")
	writeMemoryStat(t, root, "docker", "anon 40\nfile 1\n")

	docker, vms := cgroupHeldBytes(root)
	if vms != 1100 {
		t.Errorf("vms = %d, want 1100", vms)
	}
	if docker != 345 {
		t.Errorf("docker = %d, want 345", docker)
	}
}

func TestCgroupHeldBytesWithoutDockerOrVMs(t *testing.T) {
	docker, vms := cgroupHeldBytes(t.TempDir())
	if docker != 0 || vms != 0 {
		t.Fatalf("docker, vms = %d, %d, want 0, 0", docker, vms)
	}
}
