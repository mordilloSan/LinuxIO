package filelock

import (
	"context"
	"errors"
	"path/filepath"
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

func TestWithExclusiveRunsAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	called := false

	if err := WithExclusive(context.Background(), path, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithExclusive: %v", err)
	}
	if !called {
		t.Fatal("WithExclusive did not call function")
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
