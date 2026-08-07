package system

import (
	"context"
	"errors"
	"testing"
)

func TestGetCurrentServerTimeHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := GetCurrentServerTime(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetCurrentServerTime error = %v, want context.Canceled", err)
	}
}
