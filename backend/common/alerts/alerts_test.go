package alerts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func useTempStore(t *testing.T) {
	t.Helper()
	old := Path
	Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { Path = old })
}

func obs(key string) Observation {
	return Observation{Source: "test", Key: key, Severity: SeverityInfo, Title: "Title", OccurrenceID: "occ-1"}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	useTempStore(t)
	doc, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Alerts) != 0 {
		t.Fatalf("unexpected doc %+v", doc)
	}
}

func TestRaiseCreatesRecordWithRestrictiveMode(t *testing.T) {
	useTempStore(t)
	if err := Raise(context.Background(), obs("a")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	doc, _ := Load()
	if len(doc.Alerts) != 1 || doc.Alerts[0].ID != "test/a" || doc.Alerts[0].OccurrenceCount != 1 {
		t.Fatalf("unexpected alerts %+v", doc.Alerts)
	}
}

func TestRaiseSameOccurrenceIsSilent(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = Raise(ctx, obs("a"))
	_ = Dismiss(ctx, "test/a")
	_ = MarkSeen(ctx, "1000", nil)
	if err := Raise(ctx, obs("a")); err != nil {
		t.Fatal(err)
	}
	doc, _ := Load()
	a := doc.Alerts[0]
	if a.OccurrenceCount != 1 || a.DismissedAt == nil || a.Seen["1000"] != 1 {
		t.Fatalf("unchanged observation mutated state: %+v", a)
	}
}

func TestRaiseNewOccurrenceRestoresAndUnsees(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = Raise(ctx, obs("a"))
	_ = Dismiss(ctx, "test/a")
	_ = MarkSeen(ctx, "1000", nil)
	o := obs("a")
	o.OccurrenceID = "occ-2"
	o.Severity = SeverityError
	o.Title = "New title"
	if err := Raise(ctx, o); err != nil {
		t.Fatal(err)
	}
	doc, _ := Load()
	a := doc.Alerts[0]
	if a.OccurrenceCount != 2 || a.DismissedAt != nil || a.Seen["1000"] != 1 || a.Severity != SeverityError || a.Title != "New title" {
		t.Fatalf("material change not applied: %+v", a)
	}
	if !a.LastOccurrence.After(a.FirstOccurrence) && !a.LastOccurrence.Equal(a.FirstOccurrence) {
		t.Fatalf("last occurrence before first: %+v", a)
	}
}

func TestResolveRemovesAndIsIdempotent(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = Raise(ctx, obs("a"))
	if err := Resolve(ctx, "test", "a"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(ctx, "test", "a"); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	doc, _ := Load()
	if len(doc.Alerts) != 0 {
		t.Fatalf("expected empty, got %+v", doc.Alerts)
	}
}

func TestDismissUnknownIsNotFound(t *testing.T) {
	useTempStore(t)
	if err := Dismiss(context.Background(), "test/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkSeenSubset(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = Raise(ctx, obs("a"))
	_ = Raise(ctx, obs("b"))
	if err := MarkSeen(ctx, "1000", []string{"test/a"}); err != nil {
		t.Fatal(err)
	}
	doc, _ := Load()
	for _, a := range doc.Alerts {
		want := 0
		if a.ID == "test/a" {
			want = 1
		}
		if a.Seen["1000"] != want {
			t.Fatalf("%s seen = %d, want %d", a.ID, a.Seen["1000"], want)
		}
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	useTempStore(t)
	cases := []Observation{
		{Source: "Bad Source", Key: "k", Severity: SeverityInfo, Title: "t"},
		{Source: "s", Key: "", Severity: SeverityInfo, Title: "t"},
		{Source: "s", Key: "k", Severity: "critical", Title: "t"},
		{Source: "s", Key: "k", Severity: SeverityInfo, Title: ""},
		{Source: "s", Key: "k", Severity: SeverityInfo, Title: "t", Link: "https://evil"},
		{Source: "s", Key: "k", Severity: SeverityInfo, Title: "t", Link: "//evil"},
		{Source: "s", Key: "k", Severity: SeverityInfo, Title: "t", Message: string(make([]byte, MaxMessageLen+1))},
	}
	for i, c := range cases {
		if err := Raise(context.Background(), c); err == nil {
			t.Fatalf("case %d accepted %+v", i, c)
		}
	}
	if _, err := os.Stat(Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid raise created the file")
	}
}

func TestCorruptFileIsNotOverwritten(t *testing.T) {
	useTempStore(t)
	if err := os.WriteFile(Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Raise(context.Background(), obs("a")); err == nil {
		t.Fatal("raise succeeded on corrupt store")
	}
	data, _ := os.ReadFile(Path)
	if string(data) != "{not json" {
		t.Fatalf("corrupt file was rewritten: %q", data)
	}
	if _, err := Load(); err == nil {
		t.Fatal("load succeeded on corrupt store")
	}
}

func TestUnsupportedVersionIsRejected(t *testing.T) {
	useTempStore(t)
	_ = os.WriteFile(Path, []byte(`{"version":2,"alerts":[]}`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("load accepted version 2")
	}
}

func TestTimestampsUseInjectedClock(t *testing.T) {
	useTempStore(t)
	fixed := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	old := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = old })
	_ = Raise(context.Background(), obs("a"))
	doc, _ := Load()
	if !doc.Alerts[0].FirstOccurrence.Equal(fixed) {
		t.Fatalf("first occurrence = %v, want %v", doc.Alerts[0].FirstOccurrence, fixed)
	}
}
