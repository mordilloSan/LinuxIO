package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFetchBaseboardInfoReturnsEmptyWhenDMIUnavailable(t *testing.T) {
	for name, basePath := range map[string]string{
		"empty directory":   t.TempDir(),
		"missing directory": filepath.Join(t.TempDir(), "missing"),
	} {
		t.Run(name, func(t *testing.T) {
			info, err := fetchBaseboardInfo(
				context.Background(),
				basePath,
			)
			if err != nil {
				t.Fatalf("fetchBaseboardInfo() error = %v, want nil", err)
			}
			if info.Baseboard.Manufacturer != "" || info.Baseboard.Model != "" ||
				info.BIOS.Vendor != "" || info.BIOS.Version != "" {
				t.Fatalf("fetchBaseboardInfo() = %+v, want empty motherboard info", info)
			}
		})
	}
}

func TestFetchBaseboardInfoPreservesOperationalReadError(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "dmi")
	if err := os.WriteFile(basePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := fetchBaseboardInfo(context.Background(), basePath)
	if !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("fetchBaseboardInfo() error = %v, want ENOTDIR", err)
	}
	if info.Baseboard.Manufacturer != "" || info.Baseboard.Model != "" ||
		info.BIOS.Vendor != "" || info.BIOS.Version != "" {
		t.Fatalf("fetchBaseboardInfo() = %+v, want empty info on read failure", info)
	}
}

func TestFetchBaseboardInfoPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchBaseboardInfo(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchBaseboardInfo() error = %v, want context.Canceled", err)
	}
}

func TestFetchBaseboardInfoKeepsPartialIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "board_name"), []byte("Board 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := fetchBaseboardInfo(context.Background(), dir)
	if err != nil {
		t.Fatalf("fetchBaseboardInfo() error = %v", err)
	}
	if info.Baseboard.Model != "Board 1" || info.Baseboard.Manufacturer != "" {
		t.Fatalf("fetchBaseboardInfo() = %+v, want partial identity", info)
	}
}
