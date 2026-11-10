package jobs

import (
	"encoding/json/jsontext"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUnitRunID(t *testing.T) {
	got, err := UnitRunID("1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("UnitRunID: %v", err)
	}
	if want := "12345678-90ab-cdef-1234-567890abcdef"; got != want {
		t.Errorf("UnitRunID = %q, want %q", got, want)
	}
	if err := ValidateID(got); err != nil {
		t.Errorf("ValidateID(%q): %v", got, err)
	}

	for name, in := range map[string]string{
		"empty":     "",
		"short":     strings.Repeat("a", 31),
		"uppercase": "1234567890ABCDEF1234567890ABCDEF",
		"zero":      strings.Repeat("0", 32),
	} {
		if id, err := UnitRunID(in); err == nil {
			t.Errorf("UnitRunID(%s %q) = %q, want an error", name, in, id)
		}
	}
}

func TestRecordMetadataRoundTrips(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "scheduled.script")
	r.Metadata = jsontext.Value(`{"label":"x"}`)
	mustCreate(t, s, r)

	got, err := s.Get(t.Context(), r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if compact(got.Metadata) != `{"label":"x"}` {
		t.Errorf("Metadata = %s, want {\"label\":\"x\"}", got.Metadata)
	}
}

// scheduledRecord is a record of a unit's run that ended age ago, or is still
// running when state is StateRunning.
func scheduledRecord(n int, unit string, state State, now time.Time, age time.Duration) Record {
	r := testRecord(n, "scheduled.script")
	r.Target = []string{unit}
	r.State = state
	r.CreatedAt = now.Add(-age - time.Minute)
	if state != StateRunning {
		r.FinishedAt = now.Add(-age)
	}
	return r
}

func withRunFiles(t *testing.T, s *Store, r Record) {
	t.Helper()
	mustCreate(t, s, r)
	if err := os.WriteFile(s.OutputPath(r.ID), []byte("line\n"), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	if err := s.WriteProgress(r.ID, jsontext.Value(`{}`)); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestPrunePerTargetKeepsNewestPerUnit(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	oldest := scheduledRecord(1, "a.service", StateCompleted, now, 3*time.Hour)
	middle := scheduledRecord(2, "a.service", StateFailed, now, 2*time.Hour)
	newest := scheduledRecord(3, "a.service", StateCompleted, now, time.Hour)
	other := scheduledRecord(4, "b.service", StateCompleted, now, 4*time.Hour)
	running := scheduledRecord(5, "a.service", StateRunning, now, 5*time.Hour)
	for _, r := range []Record{oldest, middle, newest, other, running} {
		withRunFiles(t, s, r)
	}

	if err := s.PrunePerTarget(t.Context(), now, 2); err != nil {
		t.Fatalf("PrunePerTarget: %v", err)
	}

	if _, err := s.Get(t.Context(), oldest.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("oldest run of the unit kept: %v", err)
	}
	for _, path := range []string{s.OutputPath(oldest.ID), s.progressPath(oldest.ID)} {
		if exists(path) {
			t.Errorf("%s kept", path)
		}
	}
	for _, r := range []Record{middle, newest, other, running} {
		if _, err := s.Get(t.Context(), r.ID); err != nil {
			t.Errorf("record %s pruned: %v", r.ID, err)
		}
		for _, path := range []string{s.OutputPath(r.ID), s.progressPath(r.ID)} {
			if !exists(path) {
				t.Errorf("%s removed", path)
			}
		}
	}
}

func TestPrunePerTargetDropsOldRuns(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	old := scheduledRecord(1, "a.service", StateCompleted, now, 31*24*time.Hour)
	withRunFiles(t, s, old)

	if err := s.PrunePerTarget(t.Context(), now, MaxScheduledRunsPerUnit); err != nil {
		t.Fatalf("PrunePerTarget: %v", err)
	}
	if _, err := s.Get(t.Context(), old.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("run older than the retention kept: %v", err)
	}
	if exists(s.OutputPath(old.ID)) {
		t.Error("output of an expired run kept")
	}
}

func TestModTimeChangesWhenARecordIsWritten(t *testing.T) {
	s := newTestStore(t)

	missing, err := s.ModTime()
	if err != nil || !missing.IsZero() {
		t.Fatalf("ModTime of a missing store = %v, %v; want the zero time and no error", missing, err)
	}

	r := testRecord(1, "scheduled.script")
	mustCreate(t, s, r)
	created, err := s.ModTime()
	if err != nil || created.IsZero() {
		t.Fatalf("ModTime after Create = %v, %v; want a time", created, err)
	}

	// Backdate the directory so only a write by Update can move it forward.
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = os.Chtimes(s.stateDir, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	_, err = s.Update(t.Context(), r.ID, func(r *Record) error {
		r.State = StateFailed
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := s.ModTime()
	if err != nil {
		t.Fatalf("ModTime after Update: %v", err)
	}
	if !updated.After(old) {
		t.Errorf("ModTime after Update = %v, not after the backdated %v", updated, old)
	}
}
