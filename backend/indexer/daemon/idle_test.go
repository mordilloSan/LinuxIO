package daemon

import (
	"context"
	"testing"
	"time"
)

func TestShouldStopWhenIdle(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  DaemonConfig
		want bool
	}{
		{name: "unix socket", cfg: DaemonConfig{IdleTimeout: time.Minute}, want: true},
		{name: "disabled", cfg: DaemonConfig{}, want: false},
		{name: "TCP listener", cfg: DaemonConfig{IdleTimeout: time.Minute, ListenAddr: ":8080"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldStopWhenIdle(test.cfg); got != test.want {
				t.Fatalf("shouldStopWhenIdle() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestStopWhenIdleCancelsDaemon(t *testing.T) {
	originalInterval := idleCheckInterval
	idleCheckInterval = time.Millisecond
	t.Cleanup(func() { idleCheckInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	d := &daemon{cfg: DaemonConfig{IdleTimeout: time.Millisecond}}
	d.lastActivityUnix.Store(time.Now().Add(-time.Second).UnixNano())
	go d.stopWhenIdle(ctx, cancel)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("idle daemon did not stop")
	}
}
