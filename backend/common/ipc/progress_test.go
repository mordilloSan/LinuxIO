package ipc

import (
	"bytes"
	"encoding/json"
	"testing"
)

type testProgress struct {
	Bytes int64 `json:"bytes"`
	Total int64 `json:"total"`
}

func countFrames(t *testing.T, buf *bytes.Buffer) int {
	t.Helper()
	count := 0
	for buf.Len() > 0 {
		_, err := ReadRelayFrame(buf)
		if err != nil {
			break
		}
		count++
	}
	return count
}

func TestProgressTracker_Throttling(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 100) // write every 100 bytes

	// Small increments below threshold — should not write
	if err := pt.Report(50, 1000, testProgress{50, 1000}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Error("expected no write below interval")
	}

	// Cross the threshold — should write
	if err := pt.Report(100, 1000, testProgress{100, 1000}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected write at interval")
	}
}

func TestProgressTracker_FinalUpdateAlwaysSent(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 1000) // large interval

	// Jump straight to total — should always write even if interval not reached
	if err := pt.Report(500, 500, testProgress{500, 500}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected final update to always be sent")
	}
}

func TestProgressTracker_ZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 100)

	// Zero total — no progress should be written
	if err := pt.Report(500, 0, testProgress{500, 0}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Error("expected no write when total is 0")
	}
}

func TestProgressTracker_AsCallback(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 100)

	cancelled := false
	cancelFn := func() bool { return cancelled }

	cb := pt.AsCallback(cancelFn, func(processed, total int64) any {
		return testProgress{processed, total}
	}, 300)

	// Incremental calls
	cb.ReportProgress(50)  // 50 total, below threshold
	cb.ReportProgress(60)  // 110 total, crosses threshold → write
	cb.ReportProgress(190) // 300 total, equals total → write (final)

	frames := countFrames(t, &buf)
	if frames != 2 {
		t.Errorf("expected 2 frames (threshold + final), got %d", frames)
	}

	// Cancel check
	if cb.IsCancelled() {
		t.Error("should not be cancelled yet")
	}
	cancelled = true
	if !cb.IsCancelled() {
		t.Error("should be cancelled")
	}
}

func TestProgressTracker_AsCallback_ZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 100)

	progressCalled := false
	cb := pt.AsCallback(func() bool { return false }, func(processed, total int64) any {
		progressCalled = true
		return testProgress{processed, total}
	}, 0)

	cb.ReportProgress(42)

	if progressCalled {
		t.Error("expected makeProgress to not be called when total is 0")
	}
	if buf.Len() != 0 {
		t.Error("expected no progress frame when total is 0")
	}
}

func TestProgressTracker_CorrectPayload(t *testing.T) {
	var buf bytes.Buffer
	pt := NewProgressTracker(&buf, 0, 10)

	if err := pt.Report(100, 100, testProgress{100, 100}); err != nil {
		t.Fatal(err)
	}

	frame, err := ReadRelayFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Opcode != OpStreamProgress {
		t.Errorf("expected OpStreamProgress, got %x", frame.Opcode)
	}

	var got testProgress
	if err := json.Unmarshal(frame.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 100 || got.Total != 100 {
		t.Errorf("unexpected payload: %+v", got)
	}
}
