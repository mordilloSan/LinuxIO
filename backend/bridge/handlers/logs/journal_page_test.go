package logs

import (
	"context"
	"encoding/json/v2"
	"os/exec"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// TestGetGeneralLogsPage pages backwards from the 1st-newest cursor and checks
// the boundary entry is excluded, journal order is preserved, and HasMore
// reflects the remaining history.
func TestGetGeneralLogsPage(t *testing.T) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		t.Skip("journalctl not available")
	}
	out, err := exec.Command("journalctl", "-q", "-n", "10", "-o", "json", "--no-pager", "--reverse").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Skip("journal not readable in this environment")
	}
	newest := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(newest) < 5 {
		t.Skip("journal too small for pagination test")
	}
	var boundary struct {
		Cursor string `json:"__CURSOR"`
	}
	if err = json.Unmarshal([]byte(newest[0]), &boundary); err != nil {
		t.Fatalf("parse boundary: %v", err)
	}
	expectedCursors := make([]string, 3)
	for i, entry := range newest[1:4] {
		var fields struct {
			Cursor string `json:"__CURSOR"`
		}
		if err = json.Unmarshal([]byte(entry), &fields); err != nil {
			t.Fatalf("parse expected page entry %d: %v", i, err)
		}
		expectedCursors[i] = fields.Cursor
	}

	if _, err = GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{
		Cursor:     boundary.Cursor,
		Lines:      new("1"),
		TimePeriod: new("24h"),
	}); err != nil {
		t.Fatalf("GetGeneralLogsPage with cursor and time window: %v", err)
	}

	resp, err := GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{
		Cursor: boundary.Cursor,
		Lines:  new("3"),
	})
	if err != nil {
		t.Fatalf("GetGeneralLogsPage: %v", err)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(resp.Entries))
	}
	if !resp.HasMore {
		t.Error("HasMore = false, want true (journal has >4 entries)")
	}
	for i, entry := range resp.Entries {
		var fields struct {
			Cursor string `json:"__CURSOR"`
		}
		if err := json.Unmarshal([]byte(entry), &fields); err != nil {
			t.Fatalf("entry not valid JSON: %v", err)
		}
		if fields.Cursor == boundary.Cursor {
			t.Error("boundary entry leaked into the page")
		}
		if fields.Cursor != expectedCursors[i] {
			t.Errorf("entry %d cursor = %q, want %q", i, fields.Cursor, expectedCursors[i])
		}
	}
}

func TestGetGeneralLogsPageRejectsBadCursor(t *testing.T) {
	if _, err := GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{Cursor: "bad\ncursor"}); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}
