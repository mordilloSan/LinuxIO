package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAcquireExclusiveSerializesHolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")

	release, err := AcquireExclusive(context.Background(), path)
	if err != nil {
		t.Fatalf("AcquireExclusive: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		release2, err := AcquireExclusive(
			context.Background(),
			path,
			WithTimeout(2*time.Second),
			WithRetryDelay(10*time.Millisecond),
		)
		if err != nil {
			done <- err
			return
		}
		done <- release2()
	}()

	select {
	case err := <-done:
		t.Fatalf("second lock acquired while first was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second lock after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestAcquireExclusiveTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")

	release, err := AcquireExclusive(context.Background(), path)
	if err != nil {
		t.Fatalf("AcquireExclusive: %v", err)
	}
	defer func() {
		_ = release()
	}()

	_, err = AcquireExclusive(
		context.Background(),
		path,
		WithTimeout(50*time.Millisecond),
		WithRetryDelay(10*time.Millisecond),
	)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("AcquireExclusive error = %v, want ErrTimeout", err)
	}
}

func TestRunExclusiveRunsAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	called := false

	if err := RunExclusive(context.Background(), path, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("RunExclusive: %v", err)
	}
	if !called {
		t.Fatal("RunExclusive did not call function")
	}
	if release, err := AcquireExclusive(context.Background(), path, WithTimeout(50*time.Millisecond)); err != nil {
		t.Fatalf("lock was not released: %v", err)
	} else if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")

	release, err := AcquireExclusive(context.Background(), path)
	if err != nil {
		t.Fatalf("AcquireExclusive: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestAcquireExclusiveOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	uid, gid := os.Getuid(), os.Getgid()

	release, err := AcquireExclusive(context.Background(), path, WithOwnership(uid, gid))
	if err != nil {
		t.Fatalf("AcquireExclusive: %v", err)
	}
	defer func() { _ = release() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat lock type = %T, want *syscall.Stat_t", info.Sys())
	}
	if got := int(stat.Uid); got != uid {
		t.Fatalf("lock uid = %d, want %d", got, uid)
	}
	if got := int(stat.Gid); got != gid {
		t.Fatalf("lock gid = %d, want %d", got, gid)
	}
}

func TestAcquireExclusiveRejectsInvalidOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
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
			_, err := AcquireExclusive(context.Background(), path, WithOwnership(test.uid, test.gid))
			if err == nil {
				t.Fatal("AcquireExclusive succeeded with invalid ownership")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want substring %q", err, test.want)
			}
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("lock exists after rejected ownership: %v", statErr)
			}
		})
	}
}
