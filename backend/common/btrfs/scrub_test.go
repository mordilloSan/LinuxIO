package btrfs

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestScrubRecordsOutcomeAndReports(t *testing.T) {
	k := raid1Kernel()
	seen := make(chan struct{})
	k.scrub = func(id uint64) (scrubProgress, error) {
		<-seen // finish only after progress was reported
		return scrubProgress{DataBytesScrubbed: 10 << 30, CsumErrors: 1, CorrectedErrors: 1}, nil
	}
	stubKernel(t, k)

	var mu sync.Mutex
	var once sync.Once
	reports := 0
	got, err := Scrub(context.Background(), dataUUID, func(BtrfsScrubStatus) {
		mu.Lock()
		reports++
		mu.Unlock()
		once.Do(func() { close(seen) })
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "finished" || got.Percent != 100 || got.BytesScrubbed != 20<<30 || got.ErrorCount != 2 || got.CorrectedErrors != 2 || got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("scrub = %+v", got)
	}
	mu.Lock()
	if reports == 0 {
		t.Fatal("no progress reported")
	}
	mu.Unlock()
	calls := k.Calls()
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "SCRUB ") || !strings.HasPrefix(calls[1], "SCRUB ") {
		t.Fatalf("calls = %v", calls)
	}
	rec, ok := loadScrubRecord(dataUUID)
	if !ok || rec.Status != "finished" || rec.ErrorCount != 2 {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
	// The inventory now reports the recorded scrub.
	fs, err := FindFilesystem(context.Background(), dataUUID)
	if err != nil || fs.Scrub.Status != "finished" || fs.Scrub.ErrorCount != 2 {
		t.Fatalf("inventory scrub = %+v err=%v", fs.Scrub, err)
	}
}

func TestScrubCancelsWithContext(t *testing.T) {
	k := raid1Kernel()
	var once sync.Once
	started := make(chan struct{})
	k.scrub = func(id uint64) (scrubProgress, error) {
		once.Do(func() { close(started) })
		<-k.cancelled
		return scrubProgress{DataBytesScrubbed: 1 << 30}, unix.ECANCELED
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	got, err := Scrub(ctx, dataUUID, func(BtrfsScrubStatus) {})
	if !errors.Is(err, context.Canceled) || got.Status != "canceled" || got.BytesScrubbed != 2<<30 || got.Percent == 100 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	calls := k.Calls()
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2", "SCRUB_CANCEL"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if rec, ok := loadScrubRecord(dataUUID); !ok || rec.Status != "canceled" {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
}

func TestScrubCancelNoScrub(t *testing.T) {
	k := raid1Kernel()
	k.fail = map[uintptr]error{iocScrubCancel: unix.ENOTCONN}
	stubKernel(t, k)
	if err := ScrubCancel(context.Background(), dataUUID); err == nil || err.Error() != "no scrub is running" {
		t.Fatalf("err = %v", err)
	}
}

const progsStatus = "scrub status:1\n" +
	dataUUID + ":1|data_extents_scrubbed:10|tree_extents_scrubbed:5|data_bytes_scrubbed:1000|tree_bytes_scrubbed:200|read_errors:0|csum_errors:3|verify_errors:0|no_csum:0|csum_discards:0|super_errors:0|malloc_errors:0|uncorrectable_errors:1|corrected_errors:2|last_physical:0|t_start:1700000000|t_resumed:0|duration:90|canceled:0|finished:1\n" +
	dataUUID + ":2|data_bytes_scrubbed:1000|tree_bytes_scrubbed:200|csum_errors:0|uncorrectable_errors:0|corrected_errors:0|t_start:1700000001|t_resumed:0|duration:120|canceled:0|finished:1\n"

func TestParseProgsScrubStatus(t *testing.T) {
	s, ok := parseProgsScrubStatus(progsStatus)
	if !ok {
		t.Fatal("not parsed")
	}
	want := BtrfsScrubStatus{Status: "finished", ErrorCount: 3, UncorrectableErrors: 1, CorrectedErrors: 2, BytesScrubbed: 2400, Percent: 100}
	start := time.Unix(1700000000, 0)
	end := start.Add(120 * time.Second)
	want.StartedAt, want.FinishedAt = &start, &end
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %+v, want %+v", s, want)
	}
	if s, ok := parseProgsScrubStatus(strings.Replace(progsStatus, "canceled:0|finished:1\n", "canceled:1|finished:0\n", 1)); !ok || s.Status != "canceled" {
		t.Fatalf("canceled: %+v ok=%v", s, ok)
	}
	if _, ok := parseProgsScrubStatus("scrub status:1\n"); ok {
		t.Fatal("empty file parsed")
	}
}

func TestLastScrubPrefersNewer(t *testing.T) {
	stubKernel(t, raid1Kernel())
	old := time.Unix(1600000000, 0)
	saveScrubRecord(dataUUID, BtrfsScrubStatus{Status: "finished", StartedAt: &old, ErrorCount: 7})
	if err := writeProgsStatus(progsStatus); err != nil {
		t.Fatal(err)
	}
	if s, ok := lastScrub(dataUUID); !ok || s.ErrorCount != 3 {
		t.Fatalf("progs newer: %+v ok=%v", s, ok)
	}
	newer := time.Unix(1800000000, 0)
	saveScrubRecord(dataUUID, BtrfsScrubStatus{Status: "finished", StartedAt: &newer, ErrorCount: 7})
	if s, ok := lastScrub(dataUUID); !ok || s.ErrorCount != 7 {
		t.Fatalf("ours newer: %+v ok=%v", s, ok)
	}
}

func TestScrubFromStartsAtOffsets(t *testing.T) {
	k := raid1Kernel()
	k.maxID = 3
	k.devices[3] = &fakeDevice{path: "/dev/sdc1", total: 100 << 30, used: 51 << 30, physical: 100 << 30}
	stubKernel(t, k)
	got, err := ScrubFrom(context.Background(), dataUUID, map[int]uint64{1: 4096, 3: 0}, func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "finished" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	calls := k.Calls()
	slices.Sort(calls)
	if want := []string{"SCRUB 1 from 4096", "SCRUB 3"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestScrubFromMergesTheStoppedRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name          string
		record        string
		starts        map[int]uint64
		uncorrectable uint64
		status        string
	}{
		{"resumed after a cancel", "canceled", map[int]uint64{2: 4096}, 3, "finished"},
		{"full scrub", "canceled", nil, 1, "finished"},
		{"resumed after a finished scrub", "finished", map[int]uint64{2: 4096}, 1, "finished"},
		// A device aborted as Suspend canceled the scrub: the resume skips
		// it, so the outcome stays aborted and keeps the first part's errors.
		{"resumed after an abort", "aborted", map[int]uint64{2: 4096}, 3, "aborted"},
		// A crash mid-resume leaves the record running: it counts as an
		// abort, so the next resume keeps the first part's counters.
		{"resumed after a crash", "running", map[int]uint64{2: 4096}, 3, "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := raid1Kernel()
			k.devices[2].scrubResult = scrubProgress{DataBytesScrubbed: 1 << 30, CsumErrors: 1, UncorrectableErrors: 1}
			stubKernel(t, k)
			saveScrubRecord(dataUUID, BtrfsScrubStatus{Status: tc.record, StartedAt: &first, ErrorCount: 2, UncorrectableErrors: 2, BytesScrubbed: 3 << 30})
			got, err := ScrubFrom(context.Background(), dataUUID, tc.starts, func(BtrfsScrubStatus) {})
			if err != nil || got.Status != tc.status || got.UncorrectableErrors != tc.uncorrectable || got.FinishedAt == nil {
				t.Fatalf("got=%+v err=%v, want %s with %d uncorrectable", got, err, tc.status, tc.uncorrectable)
			}
			merged := tc.uncorrectable == 3
			if got.StartedAt.Equal(first) != merged || (got.ErrorCount == 3) != merged || (got.BytesScrubbed == 4<<30) != merged {
				t.Fatalf("got=%+v, merged=%v", got, merged)
			}
			if rec, ok := loadScrubRecord(dataUUID); !ok || rec.Status != tc.status || rec.UncorrectableErrors != got.UncorrectableErrors || !rec.StartedAt.Equal(*got.StartedAt) {
				t.Fatalf("record = %+v ok=%v, want %+v", rec, ok, got)
			}
		})
	}
}

func TestScrubPositionsSkipsIdleDevices(t *testing.T) {
	k := raid1Kernel()
	k.devices[1].running = &scrubProgress{LastPhysical: 8 << 20}
	stubKernel(t, k)
	got, err := ScrubPositions(context.Background(), dataUUID)
	if err != nil || !reflect.DeepEqual(got, map[int]uint64{1: 8 << 20}) {
		t.Fatalf("positions = %v err=%v", got, err)
	}
	// An unreadable position is not an idle device: the caller rescans.
	k.fail = map[uintptr]error{iocScrubProgress: unix.EIO}
	if got, err := ScrubPositions(context.Background(), dataUUID); err == nil {
		t.Fatalf("positions = %v despite EIO", got)
	}
}

func TestScrubCancelSparesARunningReplace(t *testing.T) {
	k := raid1Kernel()
	state := uint64(replaceStateStarted)
	k.replaceStatus = func() (uint64, uint64) { return state, 500 }
	stubKernel(t, k)
	if err := ScrubCancel(context.Background(), dataUUID); err == nil || slices.Contains(k.Calls(), "SCRUB_CANCEL") {
		t.Fatalf("err = %v, calls = %v", err, k.Calls())
	}
	// STATUS still reports a replace that finished long ago.
	state = replaceStateFinished
	if err := ScrubCancel(context.Background(), dataUUID); err != nil || !slices.Contains(k.Calls(), "SCRUB_CANCEL") {
		t.Fatalf("err = %v, calls = %v", err, k.Calls())
	}
}

// A device that aborted while the scrub was canceled must fail the scrub: an
// interrupted one would be resumed without that device, whose position the
// kernel no longer reports, and its record would read finished.
func TestScrubAbortWinsOverCancel(t *testing.T) {
	k := raid1Kernel()
	aborted := make(chan struct{})
	k.scrub = func(id uint64) (scrubProgress, error) {
		if id == 1 {
			close(aborted)
			return scrubProgress{}, unix.EIO
		}
		<-k.cancelled
		return scrubProgress{DataBytesScrubbed: 1 << 30}, unix.ECANCELED
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-aborted
		cancel()
	}()
	got, err := Scrub(ctx, dataUUID, func(BtrfsScrubStatus) {})
	if !errors.Is(err, unix.EIO) || errors.Is(err, context.Canceled) || got.Status != "aborted" {
		t.Fatalf("got=%+v err=%v, want the abort", got, err)
	}
	if rec, ok := loadScrubRecord(dataUUID); !ok || rec.Status != "aborted" {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
}

// A crash mid-resume must not lose what the first part found: the running
// record already carries its counters.
func TestScrubFromRecordsTheSeedWhileRunning(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := raid1Kernel()
	var running BtrfsScrubStatus
	k.scrub = func(uint64) (scrubProgress, error) {
		running, _ = loadScrubRecord(dataUUID)
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	saveScrubRecord(dataUUID, BtrfsScrubStatus{Status: "canceled", StartedAt: &first, ErrorCount: 2, UncorrectableErrors: 2, BytesScrubbed: 3 << 30})
	if _, err := ScrubFrom(context.Background(), dataUUID, map[int]uint64{2: 4096}, func(BtrfsScrubStatus) {}); err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" || running.StartedAt == nil || !running.StartedAt.Equal(first) || running.FinishedAt != nil ||
		running.ErrorCount != 2 || running.UncorrectableErrors != 2 || running.BytesScrubbed != 3<<30 {
		t.Fatalf("running record = %+v, want the first part's counters", running)
	}
}
