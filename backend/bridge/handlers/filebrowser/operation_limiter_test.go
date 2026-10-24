package filebrowser

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestArchiveResourceLimiterQueuesAndReleases(t *testing.T) {
	var limiter archiveResourceLimiter
	release, err := limiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}

	acquired := make(chan func(), 1)
	go func() {
		nextRelease, acquireErr := limiter.acquire(context.Background(), 1)
		if acquireErr != nil {
			t.Errorf("queued acquire returned error: %v", acquireErr)
			return
		}
		acquired <- nextRelease
	}()

	select {
	case nextRelease := <-acquired:
		nextRelease()
		t.Fatal("queued acquire completed before resource was released")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case nextRelease := <-acquired:
		nextRelease()
	case <-time.After(250 * time.Millisecond):
		t.Fatal("queued acquire did not complete after release")
	}
}

func TestArchiveResourceLimiterCancelsWhileWaiting(t *testing.T) {
	var limiter archiveResourceLimiter
	release, err := limiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		nextRelease, acquireErr := limiter.acquire(ctx, 1)
		if nextRelease != nil {
			nextRelease()
		}
		errCh <- acquireErr
	}()

	cancel()
	select {
	case acquireErr := <-errCh:
		if !errors.Is(acquireErr, context.Canceled) {
			t.Fatalf("acquire error = %v, want context.Canceled", acquireErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("queued acquire did not return after cancellation")
	}
}
