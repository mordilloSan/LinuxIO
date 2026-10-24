package filebrowser

import (
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func reports(g *progressThrottle, processed, total int64, at time.Time) bool {
	_, _, ok := g.setAt(processed, total, at)
	return ok
}

func TestTransferGateUsesTimeCadence(t *testing.T) {
	gate := newTransferGate(1024)
	start := time.Unix(0, 0)

	if !reports(gate, 100, 1000, start) {
		t.Fatal("first progress should report")
	}
	if reports(gate, 200, 1000, start.Add(transferProgressMaxInterval-time.Millisecond)) {
		t.Fatal("small progress before interval should not report")
	}
	if !reports(gate, 300, 1000, start.Add(transferProgressMaxInterval)) {
		t.Fatal("small progress after interval should report")
	}
}

func TestTransferGateUsesByteCap(t *testing.T) {
	gate := newTransferGate(1024)
	start := time.Unix(0, 0)

	if !reports(gate, 100, 10_000, start) {
		t.Fatal("first progress should report")
	}
	if !reports(gate, 1124, 10_000, start.Add(time.Millisecond)) {
		t.Fatal("progress at byte cap should report before interval")
	}
}

func TestTransferGateAlwaysReportsCompletion(t *testing.T) {
	gate := newTransferGate(1024)
	start := time.Unix(0, 0)

	if !reports(gate, 100, 1000, start) {
		t.Fatal("first progress should report")
	}
	if !reports(gate, 1000, 1000, start.Add(time.Millisecond)) {
		t.Fatal("completion should report before interval")
	}
}

func TestJobThrottlePacesByIntervalThenBytesOrPercent(t *testing.T) {
	throttle := newJobThrottle(userconfig.PersistedJobSettings{ProgressMinIntervalMs: 250, ProgressMinBytesMB: 16})
	start := time.Unix(0, 0)
	const total = 1 << 30

	if !reports(throttle, 1, total, start) {
		t.Fatal("first progress should report")
	}
	if reports(throttle, 64<<20, total, start.Add(time.Millisecond)) {
		t.Fatal("progress inside the minimum interval should not report")
	}
	if !reports(throttle, 64<<20, total, start.Add(250*time.Millisecond)) {
		t.Fatal("progress past the interval and minimum bytes should report")
	}
	if reports(throttle, 64<<20+1, total, start.Add(time.Second)) {
		t.Fatal("no byte or percentage step should not report")
	}
	if !reports(throttle, total, total, start.Add(time.Second)) {
		t.Fatal("completion should report")
	}
}

func TestCountThrottleReportsEveryAdvanceAfterInterval(t *testing.T) {
	throttle := newCountThrottle(userconfig.PersistedJobSettings{ProgressMinIntervalMs: 250})
	start := time.Unix(0, 0)

	if !reports(throttle, 1, 0, start) {
		t.Fatal("first count should report")
	}
	if reports(throttle, 2, 0, start.Add(time.Millisecond)) {
		t.Fatal("count inside the minimum interval should not report")
	}
	if !reports(throttle, 2, 0, start.Add(250*time.Millisecond)) {
		t.Fatal("count past the interval should report")
	}
	if reports(throttle, 2, 0, start.Add(time.Second)) {
		t.Fatal("unchanged count should not report")
	}
}
