package btrfs

import (
	"context"
	"os/exec"
	"testing"
)

func stubFailingCommand(t *testing.T) {
	t.Helper()
	old := execCommand
	execCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo ' boom ' >&2; exit 1")
	}
	t.Cleanup(func() { execCommand = old })
}

func TestRunCommandWrapsStderr(t *testing.T) {
	stubFailingCommand(t)
	_, err := runCommand(context.Background(), "sh", "subvolume", "list", "/x")
	if err == nil || err.Error() != "sh subvolume list: boom" {
		t.Fatalf("err = %v, want %q", err, "sh subvolume list: boom")
	}
}

func TestRunUsesBtrfs(t *testing.T) {
	if _, err := findCommand("btrfs"); err != nil {
		t.Skip("btrfs not installed")
	}
	stubFailingCommand(t)
	_, err := run(context.Background(), "filesystem", "show")
	if err == nil || err.Error() != "btrfs filesystem show: boom" {
		t.Fatalf("err = %v, want %q", err, "btrfs filesystem show: boom")
	}
}
