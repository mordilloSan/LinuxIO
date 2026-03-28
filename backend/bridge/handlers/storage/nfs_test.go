package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedNFSMountRegistryRoundTrip(t *testing.T) {
	originalPath := managedNFSMountsPath
	managedNFSMountsPath = filepath.Join(t.TempDir(), "nfs-mounts.json")
	t.Cleanup(func() {
		managedNFSMountsPath = originalPath
	})

	err := upsertManagedNFSMount(
		"192.168.1.249:/mnt/user/appdata",
		"/home/miguelmariz/docker2",
		"nfs4",
		[]string{"rw", "relatime"},
	)
	if err != nil {
		t.Fatalf("upsertManagedNFSMount() error = %v", err)
	}

	entries, err := loadManagedNFSMountEntries()
	if err != nil {
		t.Fatalf("loadManagedNFSMountEntries() error = %v", err)
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
	originalPath := managedNFSMountsPath
	managedNFSMountsPath = filepath.Join(t.TempDir(), "nfs-mounts.json")
	t.Cleanup(func() {
		managedNFSMountsPath = originalPath
	})

	if err := upsertManagedNFSMount(
		"192.168.1.249:/mnt/user/appdata",
		"/home/miguelmariz/docker2",
		"nfs",
		[]string{"rw"},
	); err != nil {
		t.Fatalf("upsertManagedNFSMount() error = %v", err)
	}

	if err := removeManagedNFSMount("/home/miguelmariz/docker2"); err != nil {
		t.Fatalf("removeManagedNFSMount() error = %v", err)
	}

	if _, err := os.Stat(managedNFSMountsPath); !os.IsNotExist(err) {
		t.Fatalf("expected registry file to be removed, stat err = %v", err)
	}
}
