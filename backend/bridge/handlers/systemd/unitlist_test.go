package systemd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestForEachListedUnitLimitedCapsConcurrency(t *testing.T) {
	entries := make([]listedUnit, maxConcurrentUnitPropertyFetches*3)
	var active int32
	var maxSeen int32

	err := forEachListedUnitLimited(context.Background(), entries, func(_ int, _ listedUnit) {
		now := atomic.AddInt32(&active, 1)
		for {
			seen := atomic.LoadInt32(&maxSeen)
			if now <= seen || atomic.CompareAndSwapInt32(&maxSeen, seen, now) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&active, -1)
	})
	if err != nil {
		t.Fatalf("forEachListedUnitLimited returned error: %v", err)
	}
	if got := atomic.LoadInt32(&maxSeen); got > int32(maxConcurrentUnitPropertyFetches) {
		t.Fatalf("max concurrency = %d, want <= %d", got, maxConcurrentUnitPropertyFetches)
	}
}

func TestForEachListedUnitLimitedHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := forEachListedUnitLimited(ctx, []listedUnit{{Name: "demo.service"}}, func(int, listedUnit) {
		t.Fatal("callback should not run for canceled context")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
