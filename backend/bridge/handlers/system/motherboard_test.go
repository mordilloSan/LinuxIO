package system

import (
	"context"
	"errors"
	"testing"
)

func TestFetchBaseboardInfoReturnsEmptyWhenDMIAndSensorsUnavailable(t *testing.T) {
	info, err := fetchBaseboardInfo(
		context.Background(),
		t.TempDir(),
		func(context.Context) map[string]float64 { return nil },
	)
	if err != nil {
		t.Fatalf("fetchBaseboardInfo() error = %v", err)
	}
	if info.Baseboard.Manufacturer != "" || info.Baseboard.Model != "" ||
		info.BIOS.Vendor != "" || info.BIOS.Version != "" ||
		info.Temperatures == nil || len(info.Temperatures.Sensors) != 0 {
		t.Fatalf("fetchBaseboardInfo() = %+v, want empty motherboard info", info)
	}
}

func TestFetchBaseboardInfoPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchBaseboardInfo(
		ctx,
		t.TempDir(),
		func(context.Context) map[string]float64 {
			t.Fatal("temperature lookup called for canceled context")
			return nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchBaseboardInfo() error = %v, want context.Canceled", err)
	}
}
