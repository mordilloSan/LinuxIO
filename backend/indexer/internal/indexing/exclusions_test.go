package indexing

import (
	"runtime"
	"testing"
)

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name         string
		isDir        bool
		fullCombined string
		shouldSkip   bool
	}{
		{
			name:         "root directory never skipped",
			isDir:        true,
			fullCombined: "/",
			shouldSkip:   false,
		},
		{
			name:         "hidden file included",
			isDir:        false,
			fullCombined: "/.hidden",
			shouldSkip:   false,
		},
		{
			name:         "regular file not skipped",
			isDir:        false,
			fullCombined: "/file.txt",
			shouldSkip:   false,
		},
		{
			name:         "regular directory not skipped",
			isDir:        true,
			fullCombined: "/documents",
			shouldSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := Initialize("/tmp/test")
			result := idx.shouldSkip(tt.isDir, tt.fullCombined)

			if result != tt.shouldSkip {
				t.Errorf("Expected shouldSkip=%v, got %v", tt.shouldSkip, result)
			}
		})
	}
}

func TestShouldSkipExcludedPaths(t *testing.T) {
	idx := Initialize("/srv", WithExcludePaths([]string{"/srv/cache"}))

	for _, path := range []string{"/cache", "/cache/nested"} {
		if !idx.shouldSkip(true, path) {
			t.Fatalf("expected %s to be excluded", path)
		}
	}
	if idx.shouldSkip(true, "/cache-old") {
		t.Fatal("path boundary excluded /cache-old")
	}
}

func TestShouldSkipDockerOverlayMergedWhenIndexingRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	idx := Initialize("/")

	if !idx.shouldSkip(true, "/var/lib/docker/overlay2/layer123/merged") {
		t.Fatal("expected docker overlay merged directory to be skipped")
	}
	if idx.shouldSkip(true, "/var/lib/docker/overlay2/layer123/diff") {
		t.Fatal("expected docker overlay diff directory not to be skipped")
	}
}

func TestShouldSkipDockerOverlayMergedWhenIndexingDockerRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	idx := Initialize("/var/lib/docker")

	if !idx.shouldSkip(true, "/overlay2/layer123/merged") {
		t.Fatal("expected docker overlay merged directory to be skipped")
	}
	if idx.shouldSkip(true, "/overlay2/layer123/diff") {
		t.Fatal("expected docker overlay diff directory not to be skipped")
	}
}

func TestShouldSkipExternalMountRespectsNetworkMountOption(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	restore := loadMountPointsFn
	loadMountPointsFn = func() map[string]string {
		return map[string]string{"/mnt/share": "nfs"}
	}
	defer func() { loadMountPointsFn = restore }()

	defaultIdx := Initialize("/mnt/share")
	if !defaultIdx.shouldSkip(true, "/nested") {
		t.Fatal("expected network mount contents to be skipped by default")
	}

	includeIdx := Initialize("/mnt/share", WithNetworkMounts(true))
	if includeIdx.shouldSkip(true, "/nested") {
		t.Fatal("expected network mount contents to be indexed when option is enabled")
	}
	if !IsPathExcludedFromIndex("/", nil, false, "/mnt/share/file") {
		t.Fatal("expected direct mutation below network mount to be excluded")
	}
	if IsPathExcludedFromIndex("/", nil, true, "/mnt/share/file") {
		t.Fatal("expected direct mutation below enabled network mount to be included")
	}
}

func TestContainerOverlayMounts(t *testing.T) {
	containers := []struct{ mountPoint, options string }{
		// Docker with the containerd image store.
		{"/var/lib/docker/rootfs/overlayfs/abc123", "rw,lowerdir=/var/lib/docker/containerd/daemon/io.containerd.snapshotter.v1.overlayfs/snapshots/3/fs,upperdir=/var/lib/docker/containerd/daemon/io.containerd.snapshotter.v1.overlayfs/snapshots/12/fs,workdir=/var/lib/docker/containerd/daemon/io.containerd.snapshotter.v1.overlayfs/snapshots/12/work"},
		// Docker's overlay2 driver, also with its data moved elsewhere.
		{"/var/lib/docker/overlay2/abc123/merged", "rw,lowerdir=/var/lib/docker/overlay2/l/XYZ,upperdir=/var/lib/docker/overlay2/abc123/diff,workdir=/var/lib/docker/overlay2/abc123/work"},
		{"/mnt/data/docker/overlay2/abc123/merged", "rw,lowerdir=/mnt/data/docker/overlay2/l/XYZ,upperdir=/mnt/data/docker/overlay2/abc123/diff,workdir=/mnt/data/docker/overlay2/abc123/work"},
		// Podman, and containerd on its own.
		{"/var/lib/containers/storage/overlay/abc123/merged", "rw,lowerdir=/var/lib/containers/storage/overlay/l/XYZ,upperdir=/var/lib/containers/storage/overlay/abc123/diff,workdir=/var/lib/containers/storage/overlay/abc123/work"},
		{"/run/containerd/io.containerd.runtime.v2.task/k8s.io/abc123/rootfs", "rw,lowerdir=/srv/k/io.containerd.snapshotter.v1.overlayfs/snapshots/1/fs,upperdir=/srv/k/io.containerd.snapshotter.v1.overlayfs/snapshots/5/fs,workdir=/srv/k/io.containerd.snapshotter.v1.overlayfs/snapshots/5/work"},
	}
	for _, mount := range containers {
		if !isContainerOverlay(mount.mountPoint, mount.options) {
			t.Errorf("the overlay at %s is not taken for a container's", mount.mountPoint)
		}
	}
	// Overlays that are part of the host: a system extension over /usr, and
	// one an administrator put over a data folder.
	hosts := []struct{ mountPoint, options string }{
		{"/usr", "ro,lowerdir=/run/systemd/sysext/meta/usr:/usr"},
		{"/srv/media", "rw,lowerdir=/srv/base,upperdir=/srv/changes,workdir=/srv/work"},
		{"/", "rw,lowerdir=/sysroot,upperdir=/run/upper,workdir=/run/work"},
	}
	for _, mount := range hosts {
		if isContainerOverlay(mount.mountPoint, mount.options) {
			t.Errorf("the overlay at %s is taken for a container's and left out", mount.mountPoint)
		}
	}
}

func TestParseMountInfoReadsTheMountOptions(t *testing.T) {
	line := `412 29 0:61 / /var/lib/docker/rootfs/overlayfs/abc123 rw,relatime shared:230 - overlay overlay rw,lowerdir=/a,upperdir=/b,workdir=/c`
	mountPoint, fsType, source, options, ok := parseMountInfo(line)
	if !ok || mountPoint != "/var/lib/docker/rootfs/overlayfs/abc123" || fsType != "overlay" || source != "overlay" || options != "rw,lowerdir=/a,upperdir=/b,workdir=/c" {
		t.Fatalf("parsed %q %q %q %q ok=%v", mountPoint, fsType, source, options, ok)
	}
}

func TestShouldSkipOverlayMountsWhateverTheNetworkSetting(t *testing.T) {
	restore := loadMountPointsFn
	loadMountPointsFn = func() map[string]string {
		return map[string]string{
			"/": "overlay",
			"/var/lib/docker/rootfs/overlayfs/abc123": "overlay",
		}
	}
	defer func() { loadMountPointsFn = restore }()

	for _, includeNetwork := range []bool{false, true} {
		idx := Initialize("/", WithNetworkMounts(includeNetwork))
		if !idx.shouldSkip(true, "/var/lib/docker/rootfs/overlayfs/abc123") {
			t.Errorf("network mounts %v: a container's overlay mount is walked", includeNetwork)
		}
		if !idx.shouldSkip(true, "/var/lib/docker/rootfs/overlayfs/abc123/usr/lib") {
			t.Errorf("network mounts %v: a folder below a container's overlay mount is walked", includeNetwork)
		}
		// A host whose own root is an overlay is still indexed.
		if idx.shouldSkip(true, "/usr") {
			t.Errorf("network mounts %v: a root filesystem on overlay is left out", includeNetwork)
		}
		if !IsPathExcludedFromIndex("/", nil, includeNetwork, "/var/lib/docker/rootfs/overlayfs/abc123/etc/passwd") {
			t.Errorf("network mounts %v: a file below a container's overlay mount is added", includeNetwork)
		}
	}
}
