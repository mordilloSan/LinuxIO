package btrfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubMkfs puts fake mkfs.ext4, mkfs.xfs and mkfs.btrfs where findCommand
// looks, so the tests need no real ones; stubExec keeps them from running.
func stubMkfs(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"mkfs.ext4", "mkfs.xfs", "mkfs.btrfs"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := searchDirs
	searchDirs = append(slices.Clone(old), bin)
	t.Cleanup(func() { searchDirs = old })
}

func TestFormatArgs(t *testing.T) {
	stubMkfs(t)
	device := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(device, make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		fsType, label string
		overwrite     bool
		want          string // <u> is the returned UUID, <d> the device
	}{
		{"ext4", "data", false, "mkfs.ext4 -U <u> -L data <d>"},
		{"ext4", "", true, "mkfs.ext4 -U <u> -F <d>"},
		{"xfs", "data", false, "mkfs.xfs -m uuid=<u> -L data <d>"},
		{"xfs", "", true, "mkfs.xfs -m uuid=<u> -f <d>"},
		{"btrfs", "", false, "mkfs.btrfs -U <u> <d>"},
		{"btrfs", "data", true, "mkfs.btrfs -U <u> -L data -f <d>"},
	}
	for _, tt := range tests {
		calls := stubExec(t)
		uuid, err := Format(context.Background(), device, tt.fsType, tt.label, tt.overwrite)
		if err != nil || len(*calls) != 1 {
			t.Fatalf("%s: uuid=%q err=%v calls=%v", tt.want, uuid, err, *calls)
		}
		got := strings.Join(append([]string{filepath.Base((*calls)[0][0])}, (*calls)[0][1:]...), " ")
		if want := strings.NewReplacer("<u>", uuid, "<d>", device).Replace(tt.want); len(uuid) != 36 || got != want {
			t.Errorf("ran %q, want %q", got, want)
		}
	}

	calls := stubExec(t)
	if _, err := Format(context.Background(), device, "vfat", "", true); err == nil || len(*calls) != 0 {
		t.Fatalf("vfat: err=%v calls=%v", err, *calls)
	}
}

func TestFormatRefusesDataWithoutOverwrite(t *testing.T) {
	stubMkfs(t)
	dir := t.TempDir()
	data, blank := filepath.Join(dir, "data"), filepath.Join(dir, "blank")
	// Both are smaller than the 1 MiB probe, so its read ends early.
	image := make([]byte, 64<<10)
	if err := os.WriteFile(blank, image, 0o600); err != nil {
		t.Fatal(err)
	}
	image[1080] = 0x53 // where ext4 keeps its magic
	if err := os.WriteFile(data, image, 0o600); err != nil {
		t.Fatal(err)
	}
	forced := func(argv []string) bool { return slices.Contains(argv, "-F") || slices.Contains(argv, "-f") }
	calls := stubExec(t)

	if _, err := Format(context.Background(), data, "ext4", "", false); !errors.Is(err, ErrDeviceHasData) || len(*calls) != 0 {
		t.Fatalf("data without overwrite: err=%v calls=%v", err, *calls)
	}
	if _, err := Format(context.Background(), data, "ext4", "", true); err != nil || len(*calls) != 1 || !forced((*calls)[0]) {
		t.Fatalf("data with overwrite: err=%v calls=%v", err, *calls)
	}
	if _, err := Format(context.Background(), blank, "ext4", "", false); err != nil || len(*calls) != 2 || forced((*calls)[1]) {
		t.Fatalf("blank without overwrite: err=%v calls=%v", err, *calls)
	}
}

func TestValidateFormat(t *testing.T) {
	for _, bad := range []struct{ fsType, label string }{
		{"vfat", ""},
		{"ext4", strings.Repeat("x", 17)},
		{"xfs", strings.Repeat("x", 13)},
		{"btrfs", strings.Repeat("x", 256)},
		{"ext4", "a/b"},
		{"xfs", "a\nb"},
	} {
		if err := ValidateFormat(bad.fsType, bad.label); err == nil {
			t.Errorf("%s %q accepted", bad.fsType, bad.label)
		}
	}
	for _, good := range []struct{ fsType, label string }{
		{"ext4", ""},
		{"xfs", ""},
		{"btrfs", ""},
		{"ext4", strings.Repeat("x", 16)},
		{"xfs", strings.Repeat("x", 12)},
		{"btrfs", strings.Repeat("x", 255)},
	} {
		if err := ValidateFormat(good.fsType, good.label); err != nil {
			t.Errorf("%s %q: %v", good.fsType, good.label, err)
		}
	}
}

func TestMinFormatSize(t *testing.T) {
	// The smallest sizes mkfs.xfs 6.18.0 and mkfs.btrfs 6.17.1 accept here.
	for fsType, want := range map[string]uint64{"ext4": 1 << 20, "xfs": 314_572_800, "btrfs": 114_294_784, "vfat": 0} {
		if got := MinFormatSize(fsType); got != want {
			t.Errorf("%s: got %d, want %d", fsType, got, want)
		}
	}
}
