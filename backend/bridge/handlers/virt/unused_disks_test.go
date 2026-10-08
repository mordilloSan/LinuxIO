package virt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestManagedDiskVMName(t *testing.T) {
	tests := map[string]string{
		"linuxio-web.qcow2":         "web",
		"linuxio-web-seed.img":      "web",
		"linuxio-web-seed.qcow2":    "web-seed",
		"linuxio-.qcow2":            "",
		"linuxio-web.vnc":           "",
		"other.qcow2":               "",
		"linuxio-../etc.qcow2":      "",
		"linuxio-web.qcow2.partial": "",
	}
	for fileName, want := range tests {
		got, ok := managedDiskVMName(fileName)
		if got != want || ok != (want != "") {
			t.Errorf("managedDiskVMName(%q) = %q, %v; want %q", fileName, got, ok, want)
		}
	}
}

// withUnusedDiskFixture points the managed disk folders at temp dirs holding
// one disk kept from a deleted VM, one owned by a defined VM, one another VM
// references by path, and one file LinuxIO did not create.
func withUnusedDiskFixture(t *testing.T) (*fakeConn, string, string) {
	t.Helper()
	pool, cloud := t.TempDir(), t.TempDir()
	oldDirs := managedDiskDirs
	managedDiskDirs = []string{pool, cloud}
	t.Cleanup(func() { managedDiskDirs = oldDirs })
	for _, path := range []string{
		filepath.Join(cloud, "linuxio-gone.qcow2"),
		filepath.Join(cloud, "linuxio-gone-seed.img"),
		filepath.Join(pool, "linuxio-live.qcow2"),
		filepath.Join(pool, "linuxio-shared.qcow2"),
		filepath.Join(pool, "other.qcow2"),
	} {
		if err := os.WriteFile(path, []byte("disk"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	fake := newFakeConn()
	fake.domains["live"] = testDomain("live")
	fake.domainXML["live"] = `<domain type="kvm"><name>live</name></domain>`
	fake.domains["borrower"] = testDomain("borrower")
	fake.domainXML["borrower"] = `<domain type="kvm"><name>borrower</name><devices><disk type="file" device="disk"><source file="/mnt/copy/linuxio-shared.qcow2"></source><target dev="vda"></target></disk></devices></domain>`
	withFakeLibvirt(t, fake)
	return fake, pool, cloud
}

func TestListUnusedDisksReturnsOnlyUnreferencedManagedDisks(t *testing.T) {
	_, _, cloud := withUnusedDiskFixture(t)

	got, err := ListUnusedDisks(context.Background())
	if err != nil {
		t.Fatalf("ListUnusedDisks: %v", err)
	}
	var paths []string
	for _, disk := range got {
		if disk.VMName != "gone" {
			t.Errorf("disk %s VMName = %q, want gone", disk.Name, disk.VMName)
		}
		paths = append(paths, disk.Path)
	}
	want := []string{filepath.Join(cloud, "linuxio-gone-seed.img"), filepath.Join(cloud, "linuxio-gone.qcow2")}
	if !slices.Equal(paths, want) {
		t.Fatalf("unused disk paths = %v, want %v", paths, want)
	}
}

func TestDeleteUnusedDiskRemovesUnreferencedDisks(t *testing.T) {
	t.Run("removes an unreferenced disk", func(t *testing.T) {
		_, _, cloud := withUnusedDiskFixture(t)
		path := filepath.Join(cloud, "linuxio-gone.qcow2")
		if err := DeleteUnusedDisk(context.Background(), path); err != nil {
			t.Fatalf("DeleteUnusedDisk: %v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disk still exists: %v", err)
		}
	})

	t.Run("deletes pool volumes through libvirt", func(t *testing.T) {
		fake, _, cloud := withUnusedDiskFixture(t)
		path := filepath.Join(cloud, "linuxio-gone.qcow2")
		fake.volumesByPath[path] = libvirt.StorageVol{Name: "linuxio-gone.qcow2", Key: path}
		if err := DeleteUnusedDisk(context.Background(), path); err != nil {
			t.Fatalf("DeleteUnusedDisk: %v", err)
		}
		if !slices.Equal(fake.deletedVolumes, []string{"linuxio-gone.qcow2"}) {
			t.Fatalf("deletedVolumes = %v, want the pool volume", fake.deletedVolumes)
		}
	})

}

func TestDeleteUnusedDiskRefusesDisksItMustKeep(t *testing.T) {
	for name, tc := range map[string]struct {
		file string
		code int
	}{
		"disk of a defined VM":        {file: "linuxio-live.qcow2", code: 409},
		"disk referenced by path":     {file: "linuxio-shared.qcow2", code: 409},
		"file LinuxIO did not create": {file: "other.qcow2", code: 400},
		"missing disk":                {file: "linuxio-absent.qcow2", code: 404},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, pool, _ := withUnusedDiskFixture(t)
			path := filepath.Join(pool, tc.file)
			err := DeleteUnusedDisk(context.Background(), path)
			if code := errorCode(err, 0); code != tc.code {
				t.Fatalf("DeleteUnusedDisk(%s) error = %v (code %d), want code %d", tc.file, err, code, tc.code)
			}
			if tc.code != 404 {
				if _, statErr := os.Stat(path); statErr != nil {
					t.Fatalf("refused disk was touched: %v", statErr)
				}
			}
		})
	}

}

func TestDeleteUnusedDiskRefusesPathsOutsideManagedFolders(t *testing.T) {
	withUnusedDiskFixture(t)
	for _, path := range []string{"/etc/linuxio-gone.qcow2", "relative/linuxio-gone.qcow2"} {
		var bridgeErr *bridgeipc.Error
		if err := DeleteUnusedDisk(context.Background(), path); !errors.As(err, &bridgeErr) || bridgeErr.Code != 400 {
			t.Fatalf("DeleteUnusedDisk(%s) error = %v, want 400", path, err)
		}
	}
}
