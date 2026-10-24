package daemon

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
	"time"
)

func TestWireProgressRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	p := NewWireProgress(&buf)
	p.Step("Checking database integrity")
	p.ScanProgress(2, 10, 4096)
	p.Summary(IndexRunStats{Files: 10, Dirs: 2, TotalSize: 4096, DeletedEntries: 3, SkippedDirs: 1, Duration: 1500 * time.Millisecond})

	decoder := jsontext.NewDecoder(bytes.NewReader(buf.Bytes()))
	var step, scan, summary indexWireEvent
	if err := json.UnmarshalDecode(decoder, &step); err != nil || step.Type != "step" || step.Message != "Checking database integrity" {
		t.Errorf("step event = %+v err=%v", step, err)
	}
	if err := json.UnmarshalDecode(decoder, &scan); err != nil || scan.Type != "scan" || scan.Dirs != 2 || scan.Files != 10 || scan.Size != 4096 {
		t.Errorf("scan event = %+v err=%v", scan, err)
	}
	if err := json.UnmarshalDecode(decoder, &summary); err != nil || summary.Type != "summary" || summary.Files != 10 || summary.DeletedEntries != 3 ||
		summary.SkippedDirs != 1 || summary.DurationMs != 1500 {
		t.Errorf("summary event = %+v", summary)
	}
}
