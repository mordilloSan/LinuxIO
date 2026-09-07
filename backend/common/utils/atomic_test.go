package utils

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestWriteFileAtomicSetsFileOwnership(t *testing.T) {
	path := t.TempDir() + "/state.yaml"
	uid, gid := os.Getuid(), os.Getgid()

	if err := WriteFileAtomic(path, []byte("state: ready\n"), 0o640, uid, gid); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat output type = %T, want *syscall.Stat_t", info.Sys())
	}
	if got := int(stat.Uid); got != uid {
		t.Fatalf("file uid = %d, want %d", got, uid)
	}
	if got := int(stat.Gid); got != gid {
		t.Fatalf("file gid = %d, want %d", got, gid)
	}
	if got, err := os.ReadFile(path); err != nil {
		t.Fatalf("read output: %v", err)
	} else if string(got) != "state: ready\n" {
		t.Fatalf("file contents = %q", got)
	}
}

func TestWriteFileAtomicRejectsInvalidOwnership(t *testing.T) {
	path := t.TempDir() + "/state.yaml"
	for _, test := range []struct {
		name string
		uid  int
		gid  int
		want string
	}{
		{name: "negative uid", uid: -1, gid: os.Getgid(), want: "uid -1 is invalid"},
		{name: "negative gid", uid: os.Getuid(), gid: -1, want: "gid -1 is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := WriteFileAtomic(path, []byte("state: ready\n"), 0o640, test.uid, test.gid)
			if err == nil {
				t.Fatal("WriteFileAtomic succeeded with invalid ownership")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want substring %q", err, test.want)
			}
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("target exists after rejected ownership: %v", statErr)
			}
		})
	}
}

func TestWriteFileAtomicRejectsIncompleteOwnership(t *testing.T) {
	path := t.TempDir() + "/state.yaml"
	if err := WriteFileAtomic(path, []byte("state: ready\n"), 0o640, os.Getuid()); err == nil {
		t.Fatal("WriteFileAtomic succeeded with incomplete ownership")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("target exists after rejected ownership: %v", err)
	}
}
