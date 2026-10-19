package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func stubRollbackPinFiles(t *testing.T, fstab, cmdline string) {
	t.Helper()
	dir := t.TempDir()
	oldFstab, oldCmdline := fstabPath, procCmdlinePath
	t.Cleanup(func() { fstabPath, procCmdlinePath = oldFstab, oldCmdline })
	fstabPath, procCmdlinePath = filepath.Join(dir, "fstab"), filepath.Join(dir, "cmdline")
	if err := os.WriteFile(fstabPath, []byte(fstab), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(procCmdlinePath, []byte(cmdline), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRollbackPinnedRefusesFstabSubvolID(t *testing.T) {
	stubRollbackPinFiles(t, "UUID=x /home btrfs subvolid=257,noatime 0 0\n", "root=UUID=x rootflags=subvol=@\n")
	mounts := []btrfs.Mount{
		{Mountpoint: "/", Subvol: "@", SubvolID: 256},
		{Mountpoint: "/home", Subvol: "@home", SubvolID: 257},
	}
	if err := checkRollbackPinned("@", mounts); err != nil {
		t.Fatalf("@ is pinned by path only, got %v", err)
	}
	err := checkRollbackPinned("@home", mounts)
	var taskErr *bridgetasks.Error
	if !errors.As(err, &taskErr) || taskErr.Code != 400 || !strings.Contains(err.Error(), "/home is mounted with subvolid= in /etc/fstab; replace it with subvol=/@home") {
		t.Fatalf("err = %v, want a 400 naming /home", err)
	}
}

func TestCheckRollbackPinnedRefusesRootflagsSubvolID(t *testing.T) {
	stubRollbackPinFiles(t, "UUID=x / btrfs subvol=/@ 0 0\n", "BOOT_IMAGE=/vmlinuz root=UUID=x ro rootflags=subvol=@,subvolid=256 quiet\n")
	err := checkRollbackPinned("@", []btrfs.Mount{{Mountpoint: "/", Subvol: "@", SubvolID: 256}})
	var taskErr *bridgetasks.Error
	if !errors.As(err, &taskErr) || taskErr.Code != 400 || !strings.Contains(err.Error(), "rootflags") {
		t.Fatalf("err = %v, want a 400 naming rootflags", err)
	}
}
