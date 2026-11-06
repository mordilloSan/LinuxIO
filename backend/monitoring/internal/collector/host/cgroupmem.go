package host

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// shortcut: cgroup v2 only; on a cgroup v1 host Docker and VM memory read as
// zero, add the v1 memory controller paths if such a host needs the split.
const cgroupRoot = "/sys/fs/cgroup"

// heldStatKeys are the memory.stat entries a cgroup cannot give back without
// swapping: process memory, shared memory and unreclaimable kernel memory.
// Host "used" (total minus available) counts the same pages, so the Docker
// and VM figures are parts of it and the page cache stays out of both.
var heldStatKeys = []string{"anon", "shmem", "kernel_stack", "pagetables", "percpu", "slab_unreclaimable"}

// cgroupHeldBytes returns the memory Docker containers and libvirt VMs hold.
// Docker's systemd cgroup driver puts each container in its own
// system.slice/docker-<id>.scope, its cgroupfs driver puts them all under
// docker/; libvirt puts every VM under machine.slice.
func cgroupHeldBytes(root string) (docker, vms uint64) {
	scopes, _ := filepath.Glob(filepath.Join(root, "system.slice", "docker-*.scope", "memory.stat"))
	for _, path := range append(scopes, filepath.Join(root, "docker", "memory.stat")) {
		docker += heldBytes(path)
	}
	return docker, heldBytes(filepath.Join(root, "machine.slice", "memory.stat"))
}

// heldBytes sums the held entries of one memory.stat file. A missing file is
// zero: the host has no such cgroup, or the container stopped mid-read.
func heldBytes(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var total uint64
	for line := range strings.Lines(string(data)) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !slices.Contains(heldStatKeys, key) {
			continue
		}
		if n, err := strconv.ParseUint(value, 10, 64); err == nil {
			total += n
		}
	}
	return total
}
