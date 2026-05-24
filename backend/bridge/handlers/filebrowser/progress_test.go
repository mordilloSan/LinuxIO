package filebrowser

import (
	"testing"
	"time"
)

func TestTransferProgressGateUsesTimeCadence(t *testing.T) {
	gate := newTransferProgressGate(1024)
	start := time.Unix(0, 0)

	if !gate.ShouldReportAt(100, 1000, start) {
		t.Fatal("first progress should report")
	}
	if gate.ShouldReportAt(200, 1000, start.Add(transferProgressMaxInterval-time.Millisecond)) {
		t.Fatal("small progress before interval should not report")
	}
	if !gate.ShouldReportAt(300, 1000, start.Add(transferProgressMaxInterval)) {
		t.Fatal("small progress after interval should report")
	}
}

func TestTransferProgressGateUsesByteCap(t *testing.T) {
	gate := newTransferProgressGate(1024)
	start := time.Unix(0, 0)

	if !gate.ShouldReportAt(100, 10_000, start) {
		t.Fatal("first progress should report")
	}
	if !gate.ShouldReportAt(1124, 10_000, start.Add(time.Millisecond)) {
		t.Fatal("progress at byte cap should report before interval")
	}
}

func TestTransferProgressGateAlwaysReportsCompletion(t *testing.T) {
	gate := newTransferProgressGate(1024)
	start := time.Unix(0, 0)

	if !gate.ShouldReportAt(100, 1000, start) {
		t.Fatal("first progress should report")
	}
	if !gate.ShouldReportAt(1000, 1000, start.Add(time.Millisecond)) {
		t.Fatal("completion should report before interval")
	}
}
