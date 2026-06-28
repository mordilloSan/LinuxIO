package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedNFSMountRegistryRoundTrip(t *testing.T) {
	originalPath := nfsMountStore.path
	nfsMountStore.path = filepath.Join(t.TempDir(), "nfs-mounts.json")
	t.Cleanup(func() {
		nfsMountStore.path = originalPath
	})

	err := nfsMountStore.upsert(managedMountEntry{
		Source:     "192.168.1.249:/mnt/user/appdata",
		Mountpoint: "/home/miguelmariz/docker2",
		FSType:     "nfs4",
		Options:    []string{"rw", "relatime"},
	})
	if err != nil {
		t.Fatalf("upsert() error = %v", err)
	}

	entries, err := nfsMountStore.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}

	entry, ok := entries["/home/miguelmariz/docker2"]
	if !ok {
		t.Fatalf("expected mountpoint to be present in managed registry")
	}
	if entry.Source != "192.168.1.249:/mnt/user/appdata" {
		t.Fatalf("unexpected source = %q", entry.Source)
	}
	if entry.FSType != "nfs4" {
		t.Fatalf("unexpected fsType = %q", entry.FSType)
	}
	if len(entry.Options) != 2 || entry.Options[0] != "rw" || entry.Options[1] != "relatime" {
		t.Fatalf("unexpected options = %#v", entry.Options)
	}
}

func TestRemoveManagedNFSMountDeletesRegistryFileWhenEmpty(t *testing.T) {
	originalPath := nfsMountStore.path
	nfsMountStore.path = filepath.Join(t.TempDir(), "nfs-mounts.json")
	t.Cleanup(func() {
		nfsMountStore.path = originalPath
	})

	if err := nfsMountStore.upsert(managedMountEntry{
		Source:     "192.168.1.249:/mnt/user/appdata",
		Mountpoint: "/home/miguelmariz/docker2",
		FSType:     "nfs",
		Options:    []string{"rw"},
	}); err != nil {
		t.Fatalf("upsert() error = %v", err)
	}

	if err := nfsMountStore.remove("/home/miguelmariz/docker2"); err != nil {
		t.Fatalf("remove() error = %v", err)
	}

	if _, err := os.Stat(nfsMountStore.path); !os.IsNotExist(err) {
		t.Fatalf("expected registry file to be removed, stat err = %v", err)
	}
}
