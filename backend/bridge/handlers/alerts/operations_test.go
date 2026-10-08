package alerts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func useTempStore(t *testing.T) {
	t.Helper()
	old := store.Path
	store.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { store.Path = old })
}

func writeCorrupt(path string) error {
	return os.WriteFile(path, []byte("{"), 0o600)
}

func TestListFiltersDismissedAndComputesSeenPerUID(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "a", Severity: "info", Title: "A", OccurrenceID: "1"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "b", Severity: "error", Title: "B", OccurrenceID: "1"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "c", Severity: "info", Title: "C", OccurrenceID: "1"})
	_ = store.Dismiss(ctx, "s/c")
	_ = store.MarkSeen(ctx, "1000", []string{"s/a"})

	list, err := listForUID(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Alerts) != 2 {
		t.Fatalf("alerts = %+v", list.Alerts)
	}
	if list.Unseen != 1 {
		t.Fatalf("unseen = %d", list.Unseen)
	}
	for _, a := range list.Alerts {
		if a.ID == "s/a" && !a.Seen || a.ID == "s/b" && a.Seen {
			t.Fatalf("seen wrong: %+v", a)
		}
		if a.FirstOccurrence == "" || a.LastOccurrence == "" {
			t.Fatalf("timestamps missing: %+v", a)
		}
	}
	other, _ := listForUID(1001)
	if other.Unseen != 2 {
		t.Fatalf("other uid unseen = %d", other.Unseen)
	}
}

func TestMarkAllSeenAndDismissReturnFreshList(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "a", Severity: "info", Title: "A"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "b", Severity: "info", Title: "B"})
	list, err := markSeenForUID(ctx, 1000, nil)
	if err != nil || list.Unseen != 0 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	list, err = dismissForUID(ctx, 1000, "s/a")
	if err != nil || len(list.Alerts) != 1 || list.Alerts[0].ID != "s/b" {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	if _, err := dismissForUID(ctx, 1000, "s/missing"); err == nil {
		t.Fatal("dismiss of unknown id succeeded")
	}
}

func TestListSurfacesCorruptStore(t *testing.T) {
	useTempStore(t)
	if err := writeCorrupt(store.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := listForUID(1000); err == nil {
		t.Fatal("corrupt store listed as empty")
	}
}
