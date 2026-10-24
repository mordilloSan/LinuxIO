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

	restore := loadExternalMountPointsFn
	loadExternalMountPointsFn = func() map[string]string {
		return map[string]string{"/mnt/share": "nfs"}
	}
	defer func() { loadExternalMountPointsFn = restore }()

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
