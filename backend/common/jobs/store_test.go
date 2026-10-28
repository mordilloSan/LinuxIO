package jobs

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	return NewStore(filepath.Join(base, "state"), filepath.Join(base, "run"))
}

// compact normalizes the indentation the store applies to nested raw values.
func compact(v jsontext.Value) string {
	v = slices.Clone(v)
	_ = v.Compact()
	return string(v)
}

func testID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", n)
}

func testRecord(n int, route string) Record {
	return Record{
		ID:        testID(n),
		Route:     route,
		Owner:     Owner{UID: 1000, Username: "alice"},
		State:     StateRunning,
		CreatedAt: time.Now().UTC(),
		StartedAt: time.Now().UTC(),
		Request:   jsontext.Value(`{"x":1}`),
		Executor:  Executor{Kind: "host", PID: 1, PIDStart: 1},
		Protocol:  Protocol,
	}
}

func mustCreate(t *testing.T, s *Store, r Record) {
	t.Helper()
	if err := s.Create(t.Context(), r); err != nil {
		t.Fatalf("Create(%s): %v", r.ID, err)
	}
}

func TestStoreCreateGetAndDuplicate(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "network.apply")
	r.Target = []string{"eth0"}
	mustCreate(t, s, r)

	got, err := s.Get(t.Context(), r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != r.ID || got.Route != r.Route || got.Owner != r.Owner || got.State != StateRunning || len(got.Target) != 1 || compact(got.Request) != `{"x":1}` {
		t.Fatalf("Get = %+v", got)
	}
	if err := s.Create(t.Context(), r); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create error = %v, want ErrExists", err)
	}
	if _, err := s.Get(t.Context(), testID(2)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error = %v, want ErrNotFound", err)
	}

	for path, want := range map[string]os.FileMode{s.stateDir: 0o700, s.runDir: 0o700, s.recordPath(r.ID): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v; want %v", path, info, err, want)
		}
	}
}

func TestStoreFinishDeletesTransientCompletedJobs(t *testing.T) {
	s := newTestStore(t)
	done := testRecord(1, "a")
	done.StartedAt = time.Now().Add(-200 * time.Millisecond)
	failed := testRecord(2, "a")
	failed.StartedAt = time.Now().Add(-200 * time.Millisecond)
	mustCreate(t, s, done)
	mustCreate(t, s, failed)
	for _, id := range []string{done.ID, failed.ID} {
		if err := s.WriteProgress(id, jsontext.Value(`{"p":1}`)); err != nil {
			t.Fatalf("WriteProgress: %v", err)
		}
	}

	if _, err := s.Finish(t.Context(), done.ID, StateCompleted, nil, nil); err != nil {
		t.Fatalf("Finish completed: %v", err)
	}
	if _, err := s.Get(t.Context(), done.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("transient completed job still present: %v", err)
	}
	if _, err := os.Stat(s.progressPath(done.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("progress file still present: %v", err)
	}

	got, err := s.Finish(t.Context(), failed.ID, StateFailed, nil, &Error{Code: 2, Message: "boom"})
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	if got.State != StateFailed || got.Error == nil || got.Error.Message != "boom" {
		t.Fatalf("Finish failed returned %+v", got)
	}
	if _, err := s.Get(t.Context(), failed.ID); err != nil {
		t.Fatalf("failed job was not kept: %v", err)
	}
}

func TestStoreFinishKeepsLongJobs(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	r.StartedAt = time.Now().Add(-2 * time.Second)
	mustCreate(t, s, r)

	got, err := s.Finish(t.Context(), r.ID, StateCompleted, jsontext.Value(`{"ok":true}`), nil)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got.FinishedAt.IsZero() || compact(got.Result) != `{"ok":true}` {
		t.Fatalf("Finish = %+v", got)
	}
	stored, err := s.Get(t.Context(), r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State != StateCompleted || stored.FinishedAt.IsZero() || compact(stored.Result) != `{"ok":true}` {
		t.Fatalf("stored = %+v", stored)
	}
}

// A record without a start time ages from its creation, as needsSync does.
func TestStoreFinishAgesFromCreatedWithoutStart(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	r.StartedAt = time.Time{}
	mustCreate(t, s, r)
	if _, err := s.Finish(t.Context(), r.ID, StateCompleted, nil, nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := s.Get(t.Context(), r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("transient job without a start time kept: %v", err)
	}
}

func TestStoreUpdateAppliesAndPersists(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)

	_, err := s.Update(t.Context(), r.ID, func(r *Record) error {
		r.Checkpoint = jsontext.Value(`{"step":2}`)
		r.CancelRequested = true
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := s.Get(t.Context(), r.ID)
	if err != nil || compact(got.Checkpoint) != `{"step":2}` || !got.CancelRequested {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	boom := errors.New("boom")
	if got, err := s.Update(t.Context(), r.ID, func(*Record) error { return boom }); !errors.Is(err, boom) || got.ID != "" {
		t.Fatalf("Update = %+v, %v; want the zero record and boom", got, err)
	}
	if _, err := s.Update(t.Context(), r.ID, func(r *Record) error { r.ID = testID(9); return nil }); err == nil {
		t.Fatal("Update accepted an ID change")
	}
}

func TestNeedsSync(t *testing.T) {
	now := time.Now()
	old := Record{State: StateRunning, StartedAt: now.Add(-2 * time.Second)}
	young := Record{State: StateRunning, StartedAt: now.Add(-100 * time.Millisecond)}
	with := func(r Record, st State) Record { r.State = st; return r }
	withCheckpoint := func(r Record) Record { r.Checkpoint = jsontext.Value(`1`); return r }

	cases := []struct {
		name          string
		before, after Record
		want          bool
	}{
		{"old to completed", old, with(old, StateCompleted), true},
		{"old to interrupted", old, with(old, StateInterrupted), true},
		{"young to failed", young, with(young, StateFailed), false},
		{"old stays running", old, old, false},
		{"checkpoint changed", young, withCheckpoint(young), true},
	}
	for _, tc := range cases {
		if got := needsSync(tc.before, tc.after, now); got != tc.want {
			t.Errorf("%s: needsSync = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestStoreListActiveAndNewestFirst(t *testing.T) {
	s := newTestStore(t)
	base := time.Now().UTC().Add(-time.Hour)
	for i := 1; i <= 3; i++ {
		r := testRecord(i, "a")
		r.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		mustCreate(t, s, r)
	}
	if _, err := s.Finish(t.Context(), testID(2), StateFailed, nil, &Error{Message: "x"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	all, err := s.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 || all[0].ID != testID(3) || all[1].ID != testID(2) || all[2].ID != testID(1) {
		t.Fatalf("List order = %v", ids(all))
	}
	active, err := s.ListActive(t.Context())
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 || active[0].ID != testID(1) || active[1].ID != testID(3) {
		t.Fatalf("ListActive = %v", ids(active))
	}
}

func TestStoreListMissingDirectory(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("List on missing dir = %v, %v", got, err)
	}
}

func ids(records []Record) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.ID
	}
	return out
}

func TestStorePruneKeepsRetentionAndCap(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	old := testRecord(1, "a")
	old.State = StateFailed
	old.FinishedAt = now.Add(-31 * 24 * time.Hour)
	mustCreate(t, s, old)
	if err := s.WriteProgress(old.ID, jsontext.Value(`{}`)); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
	running := testRecord(2, "a")
	running.CreatedAt = now.Add(-40 * 24 * time.Hour)
	mustCreate(t, s, running)

	for i := range MaxTerminalRecordsPerUID + 1 {
		r := testRecord(100+i, "a")
		// The root host's store holds every admin's jobs; the cap is the store's.
		r.Owner.UID += uint32(i % 2)
		r.State = StateCompleted
		r.FinishedAt = now.Add(-time.Duration(i) * time.Minute)
		mustCreate(t, s, r)
	}
	if err := s.Prune(t.Context(), now); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if _, err := s.Get(t.Context(), old.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired record kept: %v", err)
	}
	if _, err := os.Stat(s.progressPath(old.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired progress file kept: %v", err)
	}
	if _, err := s.Get(t.Context(), running.ID); err != nil {
		t.Errorf("running record pruned: %v", err)
	}
	all, err := s.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if terminal := len(all) - 1; terminal != MaxTerminalRecordsPerUID {
		t.Errorf("terminal records kept = %d, want %d", terminal, MaxTerminalRecordsPerUID)
	}
	// The oldest of the 201 is the one dropped.
	if _, err := s.Get(t.Context(), testID(100+MaxTerminalRecordsPerUID)); !errors.Is(err, ErrNotFound) {
		t.Errorf("oldest terminal record kept: %v", err)
	}
}

func TestStoreFinishKeepsTheOutcomeOfAResultTooLargeToStore(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	r.StartedAt = time.Now().UTC().Add(-time.Minute)
	mustCreate(t, s, r)

	big := jsontext.Value(`"` + strings.Repeat("x", maxRecordBytes) + `"`)
	if _, err := s.Finish(t.Context(), r.ID, StateCompleted, big, nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	stored, err := s.Get(t.Context(), r.ID)
	if err != nil || stored.State != StateCompleted || stored.Result != nil {
		t.Fatalf("stored = %s with %d result bytes, %v; want completed without the result", stored.State, len(stored.Result), err)
	}
}

func TestStoreKeepsAJobWhoseRequestIsTooLargeToStore(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	paths := strings.Repeat(`"/srv/data/some/long/path/name",`, maxRecordBytes/30)
	r.Request = jsontext.Value(`{"paths":[` + paths + `"/last"]}`)
	r.Target = []string{strings.Repeat("x", maxRecordBytes)}
	mustCreate(t, s, r)

	stored, err := s.Get(t.Context(), r.ID)
	if err != nil || stored.State != StateRunning || stored.Request != nil || stored.Target != nil {
		t.Fatalf("stored = %s with %d request bytes and %d targets, %v; want running without them",
			stored.State, len(stored.Request), len(stored.Target), err)
	}
}

func TestStoreReconcileCrashedMarksDeadRunningRecords(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, testRecord(1, "a"))
	mustCreate(t, s, testRecord(2, "b"))
	unit := testRecord(3, "c")
	unit.Executor = Executor{Kind: "unit", Unit: "x.service"}
	mustCreate(t, s, unit)

	changed, err := s.ReconcileCrashed(t.Context(), func(e Executor) bool { return false }, ReasonCrash)
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed = %v, want the two host records", ids(changed))
	}
	for _, r := range changed {
		if r.Executor.Kind != "host" || r.State != StateInterrupted || r.Reason != ReasonCrash || r.FinishedAt.IsZero() {
			t.Errorf("changed record = %+v", r)
		}
	}
	got, err := s.Get(t.Context(), unit.ID)
	if err != nil || got.State != StateRunning {
		t.Errorf("unit-run record touched: %+v, %v", got, err)
	}

	// A live executor is left alone.
	again := testRecord(4, "d")
	mustCreate(t, s, again)
	changed, err = s.ReconcileCrashed(t.Context(), func(Executor) bool { return true }, ReasonCrash)
	if err != nil || len(changed) != 0 {
		t.Fatalf("alive executors changed = %v, %v", ids(changed), err)
	}
	got, err = s.Get(t.Context(), again.ID)
	if err != nil || got.State != StateRunning || got.Reason != ReasonNone {
		t.Errorf("alive record = %+v, %v", got, err)
	}
}

func TestStoreReconcileCrashedRejectsBadArgumentsBeforeWriting(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)
	if _, err := s.ReconcileCrashed(t.Context(), nil, ReasonCrash); err == nil {
		t.Error("nil alive accepted")
	}
	changed, err := s.ReconcileCrashed(t.Context(), func(Executor) bool { return false }, Reason("bogus"))
	if err == nil || changed != nil {
		t.Errorf("invalid reason = %v, %v; want an error and no records", ids(changed), err)
	}
	got, err := s.Get(t.Context(), r.ID)
	if err != nil || got.State != StateRunning {
		t.Errorf("record after refused reconcile = %+v, %v", got, err)
	}
}

func TestStoreCreateUnlessActiveRefusesSecondRunningRoute(t *testing.T) {
	s := newTestStore(t)
	first := testRecord(1, "network.apply")
	if err := s.CreateUnlessActive(t.Context(), first); err != nil {
		t.Fatalf("first CreateUnlessActive: %v", err)
	}
	if err := s.CreateUnlessActive(t.Context(), testRecord(2, "network.apply")); !errors.Is(err, ErrActive) {
		t.Fatalf("same route error = %v, want ErrActive", err)
	}
	if _, err := s.Get(t.Context(), testID(2)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused record was written: %v", err)
	}
	if err := s.CreateUnlessActive(t.Context(), testRecord(3, "other")); err != nil {
		t.Fatalf("other route: %v", err)
	}
	if _, err := s.Finish(t.Context(), first.ID, StateFailed, nil, &Error{Message: "x"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := s.CreateUnlessActive(t.Context(), testRecord(4, "network.apply")); err != nil {
		t.Fatalf("after terminal: %v", err)
	}
}

func TestStoreProgressRoundTrip(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)
	if _, err := s.ReadProgress(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadProgress before write = %v, want ErrNotFound", err)
	}
	if err := s.WriteProgress(r.ID, jsontext.Value(`{"pct":50}`)); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
	got, err := s.ReadProgress(r.ID)
	if err != nil || compact(got) != `{"pct":50}` {
		t.Fatalf("ReadProgress = %s, %v", got, err)
	}
}

func TestStoreWriteProgressRefusesInvalidJSONAndMissingRecord(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)
	if err := s.WriteProgress(r.ID, jsontext.Value(`{"pct":`)); err == nil {
		t.Error("invalid progress JSON accepted")
	}
	if _, err := s.Finish(t.Context(), r.ID, StateCompleted, nil, nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := s.WriteProgress(r.ID, jsontext.Value(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("WriteProgress after transient delete = %v, want ErrNotFound", err)
	}
	if _, err := os.Lstat(s.progressPath(r.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan progress file: %v", err)
	}
}

func TestStoreWriteProgressRefusesSymlink(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, s.progressPath(r.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteProgress(r.ID, jsontext.Value(`{}`)); err == nil {
		t.Fatal("WriteProgress replaced a symlink")
	}
	if info, err := os.Lstat(s.progressPath(r.ID)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v, %v", info, err)
	}
}

func TestReadRegularFileRejectsNonLocalName(t *testing.T) {
	dir := t.TempDir()
	if _, err := readRegularFile(filepath.Join(dir, "sub"), "../x", 10); err == nil {
		t.Fatal("non-local name accepted")
	}
}

func TestStorePruneSkipsDirectoryNamedLikeRecord(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, testRecord(1, "a"))
	dir := s.recordPath(testID(2))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t.Context(), time.Now()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if all, err := s.List(t.Context()); err != nil || len(all) != 1 {
		t.Fatalf("List = %v, %v", ids(all), err)
	}
}

func TestStoreRejectsSymlinkedRecord(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, s.recordPath(r.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), r.ID); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get accepted or hid a symlinked record: %v", err)
	}
	if err := s.Create(t.Context(), r); !errors.Is(err, ErrExists) {
		t.Fatalf("Create over a symlink = %v, want ErrExists", err)
	}
}

func TestValidateID(t *testing.T) {
	if err := ValidateID(testID(1)); err != nil {
		t.Errorf("valid ID rejected: %v", err)
	}
	for _, bad := range []string{"", "../x", "00000000-0000-0000-0000-000000000000", "00000000-0000-4000-8000-00000000000A"} {
		if ValidateID(bad) == nil {
			t.Errorf("ValidateID(%q) accepted", bad)
		}
	}
}

func TestStoreToleratesTornRecord(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, testRecord(1, "a"))
	mustCreate(t, s, testRecord(2, "b"))
	torn := map[string][]byte{testID(3): nil, testID(4): []byte("{\"id\": \"garb")}
	for id, data := range torn {
		if err := os.WriteFile(s.recordPath(id), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteProgress(testID(3), jsontext.Value(`{}`)); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("List = %v, %v", ids(all), err)
	}
	active, err := s.ListActive(t.Context())
	if err != nil || len(active) != 2 {
		t.Fatalf("ListActive = %v, %v", ids(active), err)
	}
	if err = s.CreateUnlessActive(t.Context(), testRecord(5, "c")); err != nil {
		t.Fatalf("CreateUnlessActive: %v", err)
	}
	changed, err := s.ReconcileCrashed(t.Context(), func(Executor) bool { return false }, ReasonCrash)
	if err != nil || len(changed) != 3 {
		t.Fatalf("ReconcileCrashed = %v, %v", ids(changed), err)
	}

	if err = s.Prune(t.Context(), time.Now()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for id := range torn {
		if _, err := os.Lstat(s.recordPath(id)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("torn record %s kept: %v", id, err)
		}
	}
	if _, err := os.Stat(s.progressPath(testID(3))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("torn record's progress file kept: %v", err)
	}
	if all, err := s.List(t.Context()); err != nil || len(all) != 3 {
		t.Errorf("good records after Prune = %v, %v", ids(all), err)
	}
}

// A record a newer binary wrote with a member this one does not know still
// reads, and Prune keeps it.
func TestStoreKeepsRecordWithUnknownMember(t *testing.T) {
	s := newTestStore(t)
	r := testRecord(1, "a")
	mustCreate(t, s, r)
	data, err := os.ReadFile(s.recordPath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte(`{"future_member":{"x":1},`), data[1:]...)
	if err = os.WriteFile(s.recordPath(r.ID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(t.Context(), r.ID)
	if err != nil || got.Route != "a" {
		t.Fatalf("Get = %#v, %v; want the record", got, err)
	}
	if err = s.Prune(t.Context(), time.Now()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err = s.Get(t.Context(), r.ID); err != nil {
		t.Fatalf("Get after Prune: %v, want the record kept", err)
	}
}

func TestStoreCreateLeavesSharedParentsTraversable(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(root, "lib", "jobs", "0"), filepath.Join(existing, "jobs", "0"))
	mustCreate(t, s, testRecord(1, "network.apply"))

	for path, want := range map[string]os.FileMode{
		filepath.Join(root, "lib"):              0o755,
		filepath.Join(root, "lib", "jobs"):      0o755,
		filepath.Join(root, "lib", "jobs", "0"): 0o700,
		existing:                                0o750,
		filepath.Join(existing, "jobs"):         0o755,
		filepath.Join(existing, "jobs", "0"):    0o700,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat %s: %v", path, err)
		} else if info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", path, info.Mode().Perm(), want)
		}
	}
}
