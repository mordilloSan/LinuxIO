package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPollUntilProbesImmediately(t *testing.T) {
	calls := 0
	err := pollUntil(context.Background(), time.Hour, time.Hour, func() (bool, error) {
		calls++
		return true, nil
	}, errors.New("timed out"))
	if err != nil {
		t.Fatalf("pollUntil: %v", err)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestPollUntilPreservesProbeCancellationAndTimeoutErrors(t *testing.T) {
	probeErr := errors.New("probe failed")
	if err := pollUntil(context.Background(), time.Hour, time.Hour, func() (bool, error) {
		return false, probeErr
	}, errors.New("timed out")); !errors.Is(err, probeErr) {
		t.Fatalf("probe error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pollUntil(ctx, time.Hour, time.Hour, func() (bool, error) {
		return false, nil
	}, errors.New("timed out")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	timeoutErr := errors.New("exact timeout")
	if err := pollUntil(context.Background(), time.Millisecond, time.Hour, func() (bool, error) {
		return false, nil
	}, timeoutErr); err != timeoutErr {
		t.Fatalf("timeout error = %v, want exact sentinel", err)
	}
}
