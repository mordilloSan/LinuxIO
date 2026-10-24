package alerts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestRaiseEvictsWhenStoreIsFull(t *testing.T) {
	useTempStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := Document{Version: 1}
	for i := range maxAlerts {
		at := base.Add(time.Duration(i) * time.Minute)
		doc.Alerts = append(doc.Alerts, Alert{ID: ID("test", "k"+strconv.Itoa(i)), Source: "test", Key: "k" + strconv.Itoa(i), LastOccurrence: at})
	}
	dismissed := base.Add(time.Hour)
	doc.Alerts[100].DismissedAt = &dismissed // newer than k0, but the only dismissed one
	if err := write(doc); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := Raise(ctx, obs("new")); err != nil {
		t.Fatal(err)
	}
	if err := Raise(ctx, obs("new2")); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, a := range got.Alerts {
		ids[a.ID] = true
	}
	if len(got.Alerts) != maxAlerts || !ids[ID("test", "new")] || !ids[ID("test", "new2")] {
		t.Fatalf("alerts = %d, want %d including both new alerts", len(got.Alerts), maxAlerts)
	}
	if ids[ID("test", "k100")] || ids[ID("test", "k0")] || !ids[ID("test", "k1")] {
		t.Fatal("expected the dismissed alert, then the oldest alert, to be evicted")
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
		{Source: "s", Key: "k", Severity: SeverityInfo, Title: "   "},
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

func TestRaiseAndResolveNormalise(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	o := obs("Web App/1")
	o.Title = strings.Repeat("t", maxTitleLen+50)
	o.Message = strings.Repeat("é", MaxMessageLen) // 2 bytes per rune: cut on a rune boundary
	if err := Raise(ctx, o); err != nil {
		t.Fatal(err)
	}
	doc, _ := Load()
	a := doc.Alerts[0]
	if a.Key != "web-app-1" || len(a.Title) != maxTitleLen || len(a.Message) > MaxMessageLen || !utf8.ValidString(a.Message) || !strings.HasSuffix(a.Message, "…") {
		t.Fatalf("not normalised: key=%q title=%d message=%d", a.Key, len(a.Title), len(a.Message))
	}
	for key, want := range map[string]string{"-x": "x-x", "_y": "x_y", strings.Repeat("a", 100): strings.Repeat("a", maxKeyLen)} {
		if got := normalizeKey(key); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", key, got, want)
		}
	}
	if err := Resolve(ctx, "test", "Web App/1"); err != nil {
		t.Fatal(err)
	}
	if doc, _ = Load(); len(doc.Alerts) != 0 {
		t.Fatalf("resolve missed the normalised key: %+v", doc.Alerts)
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

func TestRaiseRepairsInvalidUTF8(t *testing.T) {
	useTempStore(t)
	o := obs("a")
	o.Title = "caf\xe9"
	o.Message = "bad \xff byte"
	if err := Raise(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	doc, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 || doc.Alerts[0].Title != "caf�" || doc.Alerts[0].Message != "bad � byte" {
		t.Fatalf("unexpected alerts %+v", doc.Alerts)
	}
}
