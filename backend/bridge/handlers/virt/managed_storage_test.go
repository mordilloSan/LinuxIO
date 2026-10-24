package virt

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestManagedVolumeGuardsPropagateLookupErrors(t *testing.T) {
	pool := libvirt.StoragePool{Name: defaultPoolName}

	t.Run("iso volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "volume lookup failed")

		_, _, err := createManagedVolume(fake, pool, "linuxio-iso.qcow2", 8)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedVolume error = %v, want lookup failure", err)
		}
	})

	t.Run("image volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "image lookup failed")

		_, _, err := createManagedImageVolume(context.Background(), fake, pool, "linuxio-image.qcow2", apischema.VMCreateRequest{
			Name:          "image",
			VCPUs:         1,
			MemoryMB:      1024,
			DiskGB:        32,
			SourceType:    vmSourceTypeImagePreset,
			ImagePresetID: vmImagePresetHomeOS,
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedImageVolume error = %v, want lookup failure", err)
		}
	})

	t.Run("seed volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "seed lookup failed")
		preset, err := imagePreset(vmImagePresetDebian)
		if err != nil {
			t.Fatalf("imagePreset: %v", err)
		}

		_, err = createManagedCloudInitSeed(context.Background(), fake, pool, "linuxio-seed-seed.img", apischema.VMCreateRequest{
			Name:              "seed",
			CloudInitUsername: "linuxio",
			CloudInitPassword: "secret",
		}, preset)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedCloudInitSeed error = %v, want lookup failure", err)
		}
	})
}

func TestEnsureManagedStorageDirectoriesChmodsManagedDirs(t *testing.T) {
	var mkdirs []string
	var chmods []string
	oldMkdirAll := mkdirAll
	oldChmodFile := chmodFile
	mkdirAll = func(path string, mode os.FileMode) error {
		mkdirs = append(mkdirs, path)
		if mode != 0o755 {
			t.Fatalf("mkdir mode for %s = %o, want 755", path, mode)
		}
		return nil
	}
	chmodFile = func(path string, mode os.FileMode) error {
		chmods = append(chmods, path)
		if mode != 0o755 {
			t.Fatalf("chmod mode for %s = %o, want 755", path, mode)
		}
		return nil
	}
	t.Cleanup(func() {
		mkdirAll = oldMkdirAll
		chmodFile = oldChmodFile
	})

	if err := ensureManagedStorageDirectories(); err != nil {
		t.Fatalf("ensureManagedStorageDirectories: %v", err)
	}
	want := []string{managedRootPath, managedISOPath, managedCloudPath}
	if !slices.Equal(mkdirs, want) {
		t.Fatalf("mkdirs = %#v, want %#v", mkdirs, want)
	}
	if !slices.Equal(chmods, want) {
		t.Fatalf("chmods = %#v, want %#v", chmods, want)
	}
}
