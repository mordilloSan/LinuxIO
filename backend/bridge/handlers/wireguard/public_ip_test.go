package wireguard

import (
	"context"
	"errors"
	"testing"
)

func TestResolveEndpointHostPassesCallerContext(t *testing.T) {
	newWireGuardTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var got error
	getPublicIPFunc = func(ctx context.Context) (string, error) {
		got = ctx.Err()
		return "", got
	}

	if host := resolveEndpointHost(ctx, WireGuardConfig{}, "Test"); host != "" {
		t.Fatalf("host = %q, want empty when the lookup fails", host)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("lookup context error = %v, want context.Canceled", got)
	}
}
