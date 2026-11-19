package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestScrubRecordsOutcomeAndReports(t *testing.T) {
	k := raid1Kernel()
	seen := make(chan struct{})
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		<-seen // finish only after progress was reported
		return scrubProgress{DataBytesScrubbed: 10 << 30, CsumErrors: 1, CorrectedErrors: 1}, nil
	}
	stubKernel(t, k)

	var mu sync.Mutex
	var once sync.Once
	reports := 0
	got, err := ScrubFrom(context.Background(), dataUUID, "", func(BtrfsScrubStatus) {
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
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
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
	got, err := ScrubFrom(ctx, dataUUID, "", func(BtrfsScrubStatus) {})
	if !errors.Is(err, context.Canceled) || got.Status != "canceled" || got.BytesScrubbed != 2<<30 || got.Percent == 100 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// The loop sends the cancel again on every tick until the calls return.
	calls := k.Calls()
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2", "SCRUB_CANCEL"}; !reflect.DeepEqual(slices.Compact(calls), want) {
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
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "finished", StartedAt: &old, ErrorCount: 7})
	if err := writeProgsStatus(progsStatus); err != nil {
		t.Fatal(err)
	}
	if rec, have := loadScrubRecord(dataUUID); !have {
		t.Fatal("no record")
	} else if s, ok := lastScrub(dataUUID, rec, have); !ok || s.ErrorCount != 3 {
		t.Fatalf("progs newer: %+v ok=%v", s, ok)
	}
	newer := time.Unix(1800000000, 0)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "finished", StartedAt: &newer, ErrorCount: 7})
	if rec, have := loadScrubRecord(dataUUID); !have {
		t.Fatal("no record")
	} else if s, ok := lastScrub(dataUUID, rec, have); !ok || s.ErrorCount != 7 {
		t.Fatalf("ours newer: %+v ok=%v", s, ok)
	}
}

func TestScrubFromStartsAtOffsets(t *testing.T) {
	k := raid1Kernel()
	k.maxID = 3
	k.devices[3] = &fakeDevice{path: "/dev/sdc1", total: 100 << 30, used: 51 << 30, physical: 100 << 30}
	stubKernel(t, k)
	got, err := ResumeScrub(context.Background(), dataUUID, "", map[int]uint64{1: 4096, 3: 0}, func(BtrfsScrubStatus) {})
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
		run           string // the record's job; job-1 resumes
		starts        map[int]uint64
		uncorrectable uint64
		status        string
		calls         []string
	}{
		{"resumed after a cancel", "canceled", "", map[int]uint64{2: 4096}, 3, "finished", []string{"SCRUB 2 from 4096"}},
		{"full scrub", "canceled", "", nil, 1, "finished", []string{"SCRUB 1", "SCRUB 2"}},
		{"resumed after a finished scrub", "finished", "", map[int]uint64{2: 4096}, 1, "finished", []string{"SCRUB 2 from 4096"}},
		// A device aborted as Suspend canceled the scrub: the resume skips
		// it, so the outcome stays aborted and keeps the first part's errors.
		{"resumed after an abort", "aborted", "", map[int]uint64{2: 4096}, 3, "aborted", []string{"SCRUB 2 from 4096"}},
		// A record an older build left running, crashed mid-resume, counts
		// as an abort, so the next resume keeps the first part's counters.
		{"resumed after a crash of an older build", "running", "", map[int]uint64{2: 4096}, 3, "aborted", []string{"SCRUB 2 from 4096"}},
		// The job's own record left running by a power cut is continued:
		// without saved device positions every device starts at 0, and the
		// outcome keeps the counters so far.
		{"resumed after a power cut", "running", "job-1", nil, 3, "finished", []string{"SCRUB 1", "SCRUB 2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := raid1Kernel()
			k.devices[2].scrubResult = scrubProgress{DataBytesScrubbed: 1 << 30, CsumErrors: 1, UncorrectableErrors: 1}
			stubKernel(t, k)
			saveScrubRecord(context.Background(), dataUUID, scrubRecord{
				Status: tc.record, StartedAt: &first, ErrorCount: 2, UncorrectableErrors: 2, BytesScrubbed: 3 << 30,
				Run: tc.run,
			})
			got, err := ResumeScrub(context.Background(), dataUUID, "job-1", tc.starts, func(BtrfsScrubStatus) {})
			if err != nil || got.Status != tc.status || got.UncorrectableErrors != tc.uncorrectable || got.FinishedAt == nil {
				t.Fatalf("got=%+v err=%v, want %s with %d uncorrectable", got, err, tc.status, tc.uncorrectable)
			}
			merged := tc.uncorrectable == 3
			if got.StartedAt.Equal(first) != merged || (got.ErrorCount == 3) != merged || (got.BytesScrubbed == 4<<30) != merged {
				t.Fatalf("got=%+v, merged=%v", got, merged)
			}
			if rec, ok := loadScrubRecord(dataUUID); !ok || rec.Status != tc.status || rec.UncorrectableErrors != got.UncorrectableErrors || !rec.StartedAt.Equal(*got.StartedAt) || rec.Run != "job-1" {
				t.Fatalf("record = %+v ok=%v, want %+v of job-1", rec, ok, got)
			}
			calls := k.Calls()
			slices.Sort(calls)
			if !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("calls = %v, want %v", calls, tc.calls)
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
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
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
	got, err := ScrubFrom(ctx, dataUUID, "", func(BtrfsScrubStatus) {})
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
	var running scrubRecord
	k.scrub = func(_, _ uint64) (scrubProgress, error) {
		running, _ = loadScrubRecord(dataUUID)
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "canceled", StartedAt: &first, ErrorCount: 2, UncorrectableErrors: 2, BytesScrubbed: 3 << 30})
	if _, err := ResumeScrub(context.Background(), dataUUID, "job-1", map[int]uint64{2: 4096}, func(BtrfsScrubStatus) {}); err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" || running.StartedAt == nil || !running.StartedAt.Equal(first) || running.FinishedAt != nil ||
		running.ErrorCount != 2 || running.UncorrectableErrors != 2 || running.BytesScrubbed != 3<<30 || running.Run != "job-1" {
		t.Fatalf("running record = %+v, want the first part's counters", running)
	}
	// Device 1, idle at the checkpoint, is kept out of a later resume.
	if d := running.Devices[1]; !d.Done || running.Devices[2].Offset != 4096 || running.Devices[2].Done {
		t.Fatalf("running devices = %+v, want 1 done and 2 at 4096", running.Devices)
	}
}

// saveScrubRecord stores one filesystem's scrub record, as a test sets one
// up.
func saveScrubRecord(ctx context.Context, uuid string, rec scrubRecord) {
	if err := updateScrubRecords(ctx, func(records map[string]scrubRecord) bool {
		records[uuid] = rec
		return true
	}); err != nil {
		panic(err)
	}
}

// saveEveryTick makes a running scrub save on every progress tick its
// offsets moved.
func saveEveryTick(t *testing.T) {
	t.Helper()
	old := scrubSaveInterval
	scrubSaveInterval = 0
	t.Cleanup(func() { scrubSaveInterval = old })
}

// waitForRecord polls the scrub record until ok accepts it. It runs in the
// fake kernel's scrub hooks, off the test goroutine, so it reports a
// timeout with Error.
func waitForRecord(t *testing.T, ok func(scrubRecord) bool) scrubRecord {
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec, _ := loadScrubRecord(dataUUID)
		if ok(rec) {
			return rec
		}
		if time.Now().After(deadline) {
			t.Errorf("scrub record never matched: %+v", rec)
			return rec
		}
		time.Sleep(time.Millisecond)
	}
}

// devUUID is the device UUID the fake kernel gives device id.
func devUUID(id int) string { return fmt.Sprintf("%08d-0000-4000-8000-000000000000", id) }

// threeDevices is raid1Kernel with a third device, each with its own UUID.
func threeDevices() *fakeKernel {
	k := raid1Kernel()
	k.maxID = 3
	k.devices[3] = &fakeDevice{path: "/dev/sdc1", total: 100 << 30, used: 51 << 30, physical: 100 << 30}
	for id, d := range k.devices {
		d.uuid = devUUID(int(id))
	}
	return k
}

// While a scrub runs, its record holds each device's UUID, offset and
// counters, and which devices finished or aborted, so a power cut loses at
// most one save interval. The status keeps the counters of the calls that
// returned, the devices those of the calls still running.
func TestScrubSavesWhereEachDeviceStands(t *testing.T) {
	k := threeDevices()
	saved := make(chan scrubRecord, 1)
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		switch id {
		case 2:
			return scrubProgress{DataBytesScrubbed: 5 << 20, LastPhysical: 900 << 20}, nil
		case 3:
			return scrubProgress{DataBytesScrubbed: 1 << 20, CsumErrors: 1, LastPhysical: 30 << 20}, unix.EIO
		}
		k.setRunning(1, &scrubProgress{DataBytesScrubbed: 50 << 20, CsumErrors: 1, LastPhysical: 100 << 20})
		saved <- waitForRecord(t, func(r scrubRecord) bool {
			return r.Devices[1].Offset == 100<<20 && r.Devices[2].Done && r.Devices[3].Aborted
		})
		k.setRunning(1, nil)
		return scrubProgress{DataBytesScrubbed: 120 << 20, CsumErrors: 1, LastPhysical: 200 << 20}, nil
	}
	stubKernel(t, k)
	saveEveryTick(t)
	got, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, unix.EIO) || got.Status != "aborted" || got.BytesScrubbed != 126<<20 || got.ErrorCount != 2 {
		t.Fatalf("got=%+v err=%v, want the abort of device 3", got, err)
	}
	rec := <-saved
	wantDevices := map[int]scrubDeviceRecord{
		1: {UUID: devUUID(1), Offset: 100 << 20, scrubCounters: scrubCounters{ErrorCount: 1, BytesScrubbed: 50 << 20}},
		2: {UUID: devUUID(2), Offset: 900 << 20, Done: true},
		3: {UUID: devUUID(3), Offset: 30 << 20, Aborted: true},
	}
	if rec.Status != "running" || rec.Run != "job-1" || rec.BytesScrubbed != 6<<20 || rec.ErrorCount != 1 || !reflect.DeepEqual(rec.Devices, wantDevices) {
		t.Fatalf("saved record = %+v, want devices %+v and the returned calls' counters", rec, wantDevices)
	}
	if total := rec.total(); total.BytesScrubbed != 56<<20 || total.ErrorCount != 2 {
		t.Fatalf("saved total = %+v", total)
	}
	final, _ := loadScrubRecord(dataUUID)
	wantDevices[1] = scrubDeviceRecord{UUID: devUUID(1), Offset: 200 << 20, Done: true}
	if final.Status != "aborted" || final.BytesScrubbed != 126<<20 || final.FinishedAt == nil || !reflect.DeepEqual(final.Devices, wantDevices) {
		t.Fatalf("final record = %+v, want devices %+v", final, wantDevices)
	}
}

// A power cut leaves the job's record running. Its resume starts each device
// one byte below its saved offset (0 stays 0), skips the devices that
// finished, keeps an abort, and adds the counters so far to its own. A
// device the record names with another UUID, or does not name, starts at 0.
func TestResumeScrubContinuesTheJobsRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name    string
		devices map[int]scrubDeviceRecord
		status  string
		calls   []string
	}{
		{
			name: "from each offset",
			devices: map[int]scrubDeviceRecord{
				1: {UUID: devUUID(1), Offset: 4096, scrubCounters: scrubCounters{ErrorCount: 1, UncorrectableErrors: 1, BytesScrubbed: 100 << 20}},
				2: {UUID: devUUID(2), Offset: 0},
				3: {UUID: devUUID(3), Offset: 9000, Done: true},
			},
			status: "finished",
			calls:  []string{"SCRUB 1 from 4095", "SCRUB 2"},
		},
		{
			name: "an abort is kept",
			devices: map[int]scrubDeviceRecord{
				1: {UUID: devUUID(1), Offset: 4096, scrubCounters: scrubCounters{ErrorCount: 1, UncorrectableErrors: 1, BytesScrubbed: 100 << 20}},
				2: {UUID: devUUID(2), Offset: 500, Aborted: true},
				3: {UUID: devUUID(3), Offset: 9000, Done: true},
			},
			status: "aborted",
			calls:  []string{"SCRUB 1 from 4095"},
		},
		{
			name: "another disk under a device id, or none saved",
			devices: map[int]scrubDeviceRecord{
				1: {UUID: devUUID(1), Offset: 4096, scrubCounters: scrubCounters{ErrorCount: 1, UncorrectableErrors: 1, BytesScrubbed: 100 << 20}},
				2: {UUID: devUUID(7), Offset: 8192, Done: true},
			},
			status: "finished",
			calls:  []string{"SCRUB 1 from 4095", "SCRUB 2", "SCRUB 3"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := threeDevices()
			for _, d := range k.devices {
				d.scrubResult = scrubProgress{DataBytesScrubbed: 10 << 20, CsumErrors: 1}
			}
			stubKernel(t, k)
			saveScrubRecord(context.Background(), dataUUID, scrubRecord{
				Status: "running", StartedAt: &first, ErrorCount: 2, UncorrectableErrors: 2, BytesScrubbed: 1 << 30,
				Run:     "job-1",
				Devices: tc.devices,
			})
			got, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(BtrfsScrubStatus) {})
			calls := k.Calls()
			slices.Sort(calls)
			if !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("calls = %v, want %v", calls, tc.calls)
			}
			// The status so far, device 1's saved counters, then 10 MiB and
			// one error per device scrubbed now.
			n := uint64(len(tc.calls))
			if err != nil || got.Status != tc.status || !got.StartedAt.Equal(first) ||
				got.BytesScrubbed != 1<<30+100<<20+n*10<<20 || got.ErrorCount != 3+n || got.UncorrectableErrors != 3 {
				t.Fatalf("got=%+v err=%v, want %s", got, err, tc.status)
			}
		})
	}
}

// A record of another job, or of none, is no progress of this one: without
// a checkpoint the resume scrubs every device from 0, counting anew.
func TestResumeScrubIgnoresAnotherRunsRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	for _, run := range []string{"job-0", ""} {
		k := threeDevices()
		stubKernel(t, k)
		saveScrubRecord(context.Background(), dataUUID, scrubRecord{
			Status: "running", StartedAt: &first, ErrorCount: 2, BytesScrubbed: 1 << 30,
			Run:     run,
			Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: 4096}},
		})
		got, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(BtrfsScrubStatus) {})
		calls := k.Calls()
		slices.Sort(calls)
		if want := []string{"SCRUB 1", "SCRUB 2", "SCRUB 3"}; err != nil || !reflect.DeepEqual(calls, want) || got.StartedAt.Equal(first) || got.ErrorCount != 0 {
			t.Fatalf("record of %q: got=%+v err=%v calls=%v, want a fresh full scrub", run, got, err, calls)
		}
	}
}

// The live percentage of a resumed scrub, in its reports and in the
// inventory, counts what was scrubbed before the resume.
func TestResumedScrubReportsTheProgressSoFar(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := raid1Kernel()
	k.devices[1].uuid, k.devices[2].uuid = devUUID(1), devUUID(2)
	var mu sync.Mutex
	var reported []float64
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			return scrubProgress{}, nil
		}
		k.setRunning(1, &scrubProgress{DataBytesScrubbed: 1 << 30, LastPhysical: 5 << 30})
		defer k.setRunning(1, nil)
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			n := len(reported)
			mu.Unlock()
			if n > 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		fs, err := FindFilesystem(context.Background(), dataUUID)
		// 21 GiB of the 81 GiB raid1 data and metadata.
		if err != nil || fs.Scrub.Status != "running" || fs.Scrub.BytesScrubbed != 21<<30 || fs.Scrub.Percent < 25 || !fs.Scrub.StartedAt.Equal(first) {
			t.Errorf("inventory scrub = %+v err=%v, want 21 GiB of it from before the resume", fs.Scrub, err)
		}
		return scrubProgress{DataBytesScrubbed: 2 << 30, LastPhysical: 10 << 30}, nil
	}
	stubKernel(t, k)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{
		Status: "canceled", StartedAt: &first, BytesScrubbed: 20 << 30,
		Run:     "job-1",
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: 4 << 30}, 2: {UUID: devUUID(2), Done: true}},
	})
	got, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(s BtrfsScrubStatus) {
		mu.Lock()
		reported = append(reported, s.Percent)
		mu.Unlock()
	})
	if err != nil || got.Status != "finished" || got.BytesScrubbed != 22<<30 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	// 20 GiB of 81 GiB before the resume: about 24.7%.
	if len(reported) == 0 || reported[0] < 24 || !slices.IsSorted(reported) {
		t.Fatalf("reported %v, want it from 24%% up", reported)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"SCRUB 1 from 4294967295"}) {
		t.Fatalf("calls = %v", calls)
	}
}

// An EINTR after another device's call was canceled, by the scrub cancel
// route or Suspend while ctx is live, is that cancel: the device is not
// issued again.
func TestScrubInterruptedAfterAnotherDeviceCanceled(t *testing.T) {
	k := raid1Kernel()
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 1 {
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 77 << 20}, unix.ECANCELED
		}
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[1].Offset == 77<<20 })
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 88 << 20}, unix.EINTR
	}
	stubKernel(t, k)
	got, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "canceled" {
		t.Fatalf("got=%+v err=%v, want canceled", got, err)
	}
	// The loop sends the cancel on to calls still out.
	calls := slices.DeleteFunc(k.Calls(), func(c string) bool { return c == "SCRUB_CANCEL" })
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// A device replace makes the kernel refuse every scrub: refused up front,
// the record left alone.
func TestScrubRefusedDuringAReplace(t *testing.T) {
	k := raid1Kernel()
	k.replaceStatus = func() (uint64, uint64) { return replaceStateStarted, 100 }
	stubKernel(t, k)
	first := time.Unix(1700000000, 0)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "finished", StartedAt: &first})
	_, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, ErrScrubRunning) || slices.ContainsFunc(k.Calls(), func(c string) bool { return strings.HasPrefix(c, "SCRUB") }) {
		t.Fatalf("err = %v, calls = %v, want a refusal before any call", err, k.Calls())
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.Status != "finished" || !rec.StartedAt.Equal(first) {
		t.Fatalf("record = %+v", rec)
	}
}

// A scrub that starts between the check and the call is refused by the
// kernel on every device (EINPROGRESS): it fails with ErrScrubRunning and
// puts back the record it replaced.
func TestScrubRefusedByTheKernelPutsBackTheRecord(t *testing.T) {
	k := raid1Kernel()
	k.scrub = func(_, start uint64) (scrubProgress, error) {
		return scrubProgress{LastPhysical: start}, unix.EINPROGRESS
	}
	stubKernel(t, k)
	first := time.Unix(1700000000, 0)
	other := scrubRecord{Status: "running", StartedAt: &first, BytesScrubbed: 1 << 30, Run: "job-0"}
	saveScrubRecord(context.Background(), dataUUID, other)
	_, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, ErrScrubRunning) {
		t.Fatalf("err = %v, want ErrScrubRunning", err)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.Run != "job-0" || rec.BytesScrubbed != 1<<30 || !rec.StartedAt.Equal(first) || rec.Devices != nil {
		t.Fatalf("record = %+v, want the other scrub's", rec)
	}
}

// The job host and linuxio-worker resync save their pools' scrubs
// through the records' lock, so neither loses the other's entry.
func TestScrubRecordsOfTwoPoolsSavedTogether(t *testing.T) {
	stubStatePaths(t, t.TempDir())
	var wg sync.WaitGroup
	for _, uuid := range []string{"pool-a", "pool-b"} {
		wg.Go(func() {
			for i := range 50 {
				saveScrubRecord(context.Background(), uuid, scrubRecord{Status: "running", BytesScrubbed: uint64(i)})
			}
		})
	}
	wg.Wait()
	records := loadScrubRecords()
	if records["pool-a"].BytesScrubbed != 49 || records["pool-b"].BytesScrubbed != 49 {
		t.Fatalf("records = %+v, want both pools' last saves", records)
	}
}

// A scrub the kernel refuses because another runs must leave that one's
// record alone, and issue nothing.
func TestScrubRefusedLeavesTheRunningRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := raid1Kernel()
	k.devices[2].running = &scrubProgress{LastPhysical: 8 << 20}
	stubKernel(t, k)
	running := scrubRecord{
		Status: "running", StartedAt: &first, BytesScrubbed: 1 << 30,
		Run:     "job-0",
		Devices: map[int]scrubDeviceRecord{1: {Offset: 4096}},
	}
	saveScrubRecord(context.Background(), dataUUID, running)
	_, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, ErrScrubRunning) || len(k.Calls()) != 0 {
		t.Fatalf("err = %v, calls = %v, want a refusal before any call", err, k.Calls())
	}
	if rec, _ := loadScrubRecord(dataUUID); !reflect.DeepEqual(rec.Devices, running.Devices) || rec.Run != "job-0" || !rec.StartedAt.Equal(first) {
		t.Fatalf("record = %+v, want the running scrub's", rec)
	}
}

// Since 6.19 a freeze ends a scrub call with EINTR. The scrub issues it
// again one byte below where it stopped, saves where it stands first, sums
// both calls' counters, and its progress does not drop meanwhile.
func TestScrubIssuesAnInterruptedCallAgain(t *testing.T) {
	k := raid1Kernel()
	var mu sync.Mutex
	var reported []uint64
	lastReport := func() uint64 {
		mu.Lock()
		defer mu.Unlock()
		if len(reported) == 0 {
			return 0
		}
		return reported[len(reported)-1]
	}
	var reissued scrubRecord
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 2:
			return scrubProgress{LastPhysical: 3 << 30}, nil
		case start == 0:
			return scrubProgress{DataBytesScrubbed: 600 << 20, CsumErrors: 1, LastPhysical: 1 << 30}, unix.EINTR
		}
		reissued, _ = loadScrubRecord(dataUUID)
		k.setRunning(1, &scrubProgress{DataBytesScrubbed: 100 << 20, LastPhysical: 1100 << 20})
		deadline := time.Now().Add(5 * time.Second)
		for lastReport() < 700<<20 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		k.setRunning(1, nil)
		return scrubProgress{DataBytesScrubbed: 700 << 20, LastPhysical: 2 << 30}, nil
	}
	stubKernel(t, k)
	got, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(s BtrfsScrubStatus) {
		mu.Lock()
		reported = append(reported, s.BytesScrubbed)
		mu.Unlock()
	})
	if err != nil || got.Status != "finished" || got.BytesScrubbed != 1300<<20 || got.ErrorCount != 1 {
		t.Fatalf("got=%+v err=%v, want both calls of device 1 counted", got, err)
	}
	calls := k.Calls()
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 1 from 1073741823", "SCRUB 2"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if d := reissued.Devices[1]; reissued.Status != "running" || d.Offset != 1<<30 || d.Done || reissued.total().BytesScrubbed != 600<<20 {
		t.Fatalf("record when issued again = %+v, want device 1 at 1 GiB with its first call counted", reissued)
	}
	if last := lastReport(); last < 700<<20 {
		t.Fatalf("last report %d bytes, want the first call's 600 MiB and the second's 100 MiB", last)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.IsSorted(reported) {
		t.Fatalf("reported %v, want it never to drop", reported)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.Devices[1].Offset != 2<<30 || !rec.Devices[1].Done {
		t.Fatalf("final record = %+v", rec)
	}
}

// An EINTR once the scrub is canceled is the cancel, not a freeze to wait
// out: nothing is issued again and the scrub ends canceled.
func TestScrubInterruptedAfterCancelIsCanceled(t *testing.T) {
	k := raid1Kernel()
	started := make(chan struct{}, 2)
	k.scrub = func(_, _ uint64) (scrubProgress, error) {
		started <- struct{}{}
		<-k.cancelled
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 8 << 20}, unix.EINTR
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		<-started
		cancel()
	}()
	got, err := ScrubFrom(ctx, dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, context.Canceled) || got.Status != "canceled" || got.BytesScrubbed != 2<<20 {
		t.Fatalf("got=%+v err=%v, want canceled", got, err)
	}
	calls := slices.DeleteFunc(k.Calls(), func(c string) bool { return c == "SCRUB_CANCEL" })
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// A kernel that keeps interrupting a call before it gets past its start
// must not make the scrub spin: after maxScrubStalls such calls the device
// aborts. A call re-reads the dev extent holding its start, so reading some
// of it below the start is no progress.
func TestScrubGivesUpOnInterruptsThatMoveNothing(t *testing.T) {
	k := raid1Kernel()
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 2:
			return scrubProgress{DataBytesScrubbed: 1 << 20}, nil
		case start == 0:
			return scrubProgress{DataBytesScrubbed: 10 << 20, LastPhysical: 64 << 20}, unix.EINTR
		}
		return scrubProgress{DataBytesScrubbed: 4 << 20, LastPhysical: start - 32<<20}, unix.EINTR
	}
	stubKernel(t, k)
	got, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, unix.EINTR) || got.Status != "aborted" {
		t.Fatalf("got=%+v err=%v, want aborted", got, err)
	}
	calls := k.Calls()
	slices.Sort(calls)
	again := "SCRUB 1 from 67108863"
	if want := []string{"SCRUB 1", again, again, again, "SCRUB 2"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if rec, _ := loadScrubRecord(dataUUID); !rec.Devices[1].Aborted || rec.Status != "aborted" {
		t.Fatalf("record = %+v, want device 1 aborted", rec)
	}
}

// A call issued again during a freeze waits for the thaw, and SCRUB_CANCEL
// finds nothing to cancel meanwhile (ENOTCONN). A cancel must outlast the
// freeze: the scrub sends it again until its calls return, so the scrub
// that starts at the thaw is canceled and ScrubFrom returns.
func TestScrubCancelOutlastsAFreeze(t *testing.T) {
	k := raid1Kernel()
	blocked, thaw, scrubbing, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var whileFrozen atomic.Int32
	var once sync.Once
	k.scrubCancel = func() error {
		select {
		case <-scrubbing:
			once.Do(func() { close(canceled) })
			return nil
		default:
			whileFrozen.Add(1)
			return unix.ENOTCONN
		}
	}
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 2:
			return scrubProgress{DataBytesScrubbed: 1 << 20}, nil
		case start == 0:
			return scrubProgress{DataBytesScrubbed: 10 << 20, LastPhysical: 64 << 20}, unix.EINTR
		}
		close(blocked)
		<-thaw // in mnt_want_write
		close(scrubbing)
		<-canceled
		return scrubProgress{DataBytesScrubbed: 5 << 20, LastPhysical: 70 << 20}, unix.ECANCELED
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-blocked
		cancel()
		for whileFrozen.Load() < 3 {
			time.Sleep(time.Millisecond)
		}
		close(thaw)
	}()
	got, err := ScrubFrom(ctx, dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, context.Canceled) || got.Status != "canceled" || got.BytesScrubbed != 16<<20 {
		t.Fatalf("got=%+v err=%v, want canceled", got, err)
	}
	if n := whileFrozen.Load(); n < 3 {
		t.Fatalf("%d cancels while frozen, want at least 3", n)
	}
}

// A scrub between kernel calls, or waiting for a thaw, has nothing in the
// kernel (ENOTCONN everywhere): a second scrub is refused by the first one's
// running record and live process, and leaves that record alone.
func TestScrubRefusedWhileTheScrubOnRecordWaits(t *testing.T) {
	k := raid1Kernel()
	waiting, thaw := make(chan struct{}), make(chan struct{})
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 2:
			return scrubProgress{}, nil
		case start == 0:
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 64 << 20}, unix.EINTR
		}
		close(waiting)
		<-thaw // issued again, waiting in mnt_want_write
		return scrubProgress{LastPhysical: 128 << 20}, nil
	}
	stubKernel(t, k)
	done := make(chan error, 1)
	go func() {
		_, err := ScrubFrom(context.Background(), dataUUID, "job-A", func(BtrfsScrubStatus) {})
		done <- err
	}()
	<-waiting
	// Device 2 finished, device 1 waits: nothing saves until the thaw.
	before := waitForRecord(t, func(r scrubRecord) bool { return r.Devices[2].Done && r.Devices[1].Offset == 64<<20 })
	calls := len(k.Calls())
	_, err := ScrubFrom(context.Background(), dataUUID, "job-B", func(BtrfsScrubStatus) {})
	after, _ := loadScrubRecord(dataUUID)
	callsAfter := k.Calls()
	close(thaw)
	if errA := <-done; errA != nil {
		t.Fatalf("scrub A: %v", errA)
	}
	if !errors.Is(err, ErrScrubRunning) || len(callsAfter) != calls || before.Run != "job-A" || !reflect.DeepEqual(after, before) {
		t.Fatalf("err = %v, calls %v, record %+v, want a refusal leaving %+v", err, callsAfter, after, before)
	}
}

// An EINTR after a call was canceled is that cancel even in a scrub that
// already reads aborted, whose status no cancel changes: nothing is issued
// again.
func TestScrubInterruptedInAnAbortedScrubAfterACancel(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := raid1Kernel()
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 1 {
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 77 << 20}, unix.ECANCELED
		}
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[1].Offset == 77<<20 })
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 88 << 20}, unix.EINTR
	}
	stubKernel(t, k)
	// An older build's record of a scrub a device aborted.
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "aborted", StartedAt: &first})
	got, err := ResumeScrub(context.Background(), dataUUID, "job-1", map[int]uint64{1: 4096, 2: 4096}, func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "aborted" {
		t.Fatalf("got=%+v err=%v, want the abort kept", got, err)
	}
	calls := slices.DeleteFunc(k.Calls(), func(c string) bool { return c == "SCRUB_CANCEL" })
	slices.Sort(calls)
	if want := []string{"SCRUB 1 from 4096", "SCRUB 2 from 4096"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// A device the inventory could not read is left out of its list, so a
// scrub would skip it, a resume drop its saved state, and the outcome read
// finished: both are refused before anything is written.
func TestScrubRefusedWhenDevicesUnread(t *testing.T) {
	k := raid1Kernel()
	k.fail = map[uintptr]error{iocGetDevStats: unix.EIO}
	stubKernel(t, k)
	first := time.Unix(1700000000, 0)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "running", StartedAt: &first, Run: "job-1"})
	rec, _ := loadScrubRecord(dataUUID)
	want := "could not read the devices of pool " + dataUUID
	if _, err := ScrubFrom(context.Background(), dataUUID, "job-2", func(BtrfsScrubStatus) {}); err == nil || err.Error() != want {
		t.Fatalf("scrub: %v, want %s", err, want)
	}
	if _, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(BtrfsScrubStatus) {}); err == nil || err.Error() != want {
		t.Fatalf("resume: %v, want %s", err, want)
	}
	if after, _ := loadScrubRecord(dataUUID); len(k.Calls()) != 0 || !reflect.DeepEqual(after, rec) {
		t.Fatalf("calls %v, record %+v, want neither touched", k.Calls(), after)
	}
}

// The job's own record left aborted (a device aborted, then Suspend canceled
// the rest) continues from the record: the aborted device skipped and the
// abort kept, the others one byte below their offsets, not from the
// checkpoint's raw positions.
func TestResumeScrubContinuesItsAbortedRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{
		Status: "aborted", StartedAt: &first, ErrorCount: 1, BytesScrubbed: 1 << 30,
		Run: "job-1",
		Devices: map[int]scrubDeviceRecord{
			1: {UUID: devUUID(1), Offset: 500, Aborted: true},
			2: {UUID: devUUID(2), Offset: 4096},
			3: {UUID: devUUID(3), Offset: 9000, Done: true},
		},
	})
	got, err := ResumeScrub(context.Background(), dataUUID, "job-1", map[int]uint64{2: 8192}, func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "aborted" || !got.StartedAt.Equal(first) || got.ErrorCount != 1 || got.BytesScrubbed != 1<<30 {
		t.Fatalf("got=%+v err=%v, want the abort and counters kept", got, err)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"SCRUB 2 from 4095"}) {
		t.Fatalf("calls = %v, want device 2 from its record", calls)
	}
}

// With every call issued again after a freeze and waiting for the thaw,
// SCRUB_CANCEL finds nothing (ENOTCONN). ScrubCancel, the route's and
// Suspend's, still takes the cancel: it marks the running record, the scrub
// reads the mark and cancels the calls once they start at the thaw. The
// resume of the canceled scrub does not inherit the mark.
func TestScrubCancelReachesCallsWaitingForAThaw(t *testing.T) {
	k := raid1Kernel()
	waiting, thaw, canceled := make(chan struct{}, 2), make(chan struct{}), make(chan struct{})
	var scrubbing atomic.Bool
	var once sync.Once
	k.scrubCancel = func() error {
		if !scrubbing.Load() {
			return unix.ENOTCONN
		}
		once.Do(func() { close(canceled) })
		return nil
	}
	k.scrub = func(_, start uint64) (scrubProgress, error) {
		if start == 0 {
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 64 << 20}, unix.EINTR
		}
		waiting <- struct{}{}
		<-thaw // in mnt_want_write
		scrubbing.Store(true)
		<-canceled
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 70 << 20}, unix.ECANCELED
	}
	stubKernel(t, k)
	type outcome struct {
		status BtrfsScrubStatus
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		s, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
		done <- outcome{s, err}
	}()
	<-waiting
	<-waiting
	cancelErr := ScrubCancel(context.Background(), dataUUID)
	marked, _ := loadScrubRecord(dataUUID)
	close(thaw)
	var got outcome
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		scrubbing.Store(true)
		once.Do(func() { close(canceled) })
		got = <-done
		t.Error("the scrub went on after the thaw")
	}
	if cancelErr != nil || !marked.CancelRequested || got.err != nil || got.status.Status != "canceled" {
		t.Fatalf("ScrubCancel: %v, marked %v; scrub %+v err=%v, want canceled", cancelErr, marked.CancelRequested, got.status, got.err)
	}
	k.scrub, k.scrubCancel = nil, nil
	resumed, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(BtrfsScrubStatus) {})
	if err != nil || resumed.Status != "finished" {
		t.Fatalf("resumed = %+v err=%v, want finished despite the old request", resumed, err)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.CancelRequested {
		t.Fatalf("record = %+v, want the request gone", rec)
	}
}

// A cancel requested after the loop last read the record and before its
// next save is taken over by that save, not written away.
func TestScrubSaveKeepsARequestedCancel(t *testing.T) {
	k := raid1Kernel()
	k.scrubCancel = func() error { return unix.ENOTCONN } // every call waits for a thaw
	stubKernel(t, k)
	fs, err := FindFilesystem(context.Background(), dataUUID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r := newScrubRun(fs, scrubRecord{Status: "finished", StartedAt: &now, Run: "job-1"})
	r.save(context.Background())
	if err := ScrubCancel(context.Background(), dataUUID); err != nil {
		t.Fatal(err)
	}
	r.save(context.Background())
	if rec, _ := loadScrubRecord(dataUUID); !r.canceled || !rec.CancelRequested {
		t.Fatalf("canceled %v, record %+v, want the request kept", r.canceled, rec)
	}
}

// Without a LinuxIO scrub running in a live process, ENOTCONN still means no
// scrub runs; a cancel the kernel refuses because a replace runs marks
// nothing.
func TestScrubCancelWithoutALiveScrub(t *testing.T) {
	k := raid1Kernel()
	k.scrubCancel = func() error { return unix.ENOTCONN }
	stubKernel(t, k)
	first := time.Unix(1700000000, 0)
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "running", StartedAt: &first, Run: "job-1"})
	if err := ScrubCancel(context.Background(), dataUUID); !errors.Is(err, ErrScrubNotRunning) {
		t.Fatalf("err = %v, want ErrScrubNotRunning", err)
	}
	live := scrubRecord{Status: "running", StartedAt: &first, Run: "job-1"}
	live.writtenByThisProcess()
	saveScrubRecord(context.Background(), dataUUID, live)
	k.replaceStatus = func() (uint64, uint64) { return replaceStateStarted, 10 }
	if err := ScrubCancel(context.Background(), dataUUID); err == nil || errors.Is(err, ErrScrubNotRunning) {
		t.Fatalf("err = %v, want the replace's refusal", err)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.CancelRequested {
		t.Fatalf("record = %+v, want no request", rec)
	}
}

// A cancel that reached one device while the other's call was being issued
// again after a freeze must reach that call too: the loop keeps sending it
// once any call was canceled.
func TestScrubKeepsCancelingAfterOneDeviceWasCanceled(t *testing.T) {
	k := raid1Kernel()
	reissued, canceled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	cancelOnce := func() { once.Do(func() { close(canceled) }) }
	k.scrubCancel = func() error {
		select {
		case <-reissued:
			cancelOnce()
		default:
		}
		return nil
	}
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 1:
			<-reissued
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 77 << 20}, unix.ECANCELED
		case start == 0:
			return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 64 << 20}, unix.EINTR
		}
		close(reissued)
		<-canceled
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 70 << 20}, unix.ECANCELED
	}
	stubKernel(t, k)
	done := make(chan BtrfsScrubStatus, 1)
	go func() {
		s, _ := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
		done <- s
	}()
	var got BtrfsScrubStatus
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		cancelOnce()
		got = <-done
		t.Error("the call issued again was never canceled")
	}
	if got.Status != "canceled" {
		t.Fatalf("got = %+v, want canceled", got)
	}
}

// A cancel requested after the last tick is taken over by the save before
// an interrupted call is issued again: the call is not issued, since it
// would only wait for the thaw, and the scrub ends canceled.
func TestScrubInterruptedCallNotIssuedAfterACancel(t *testing.T) {
	k := raid1Kernel()
	k.scrubCancel = func() error { return unix.ENOTCONN }
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			return scrubProgress{LastPhysical: 1 << 30}, nil
		}
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[2].Done })
		if err := ScrubCancel(context.Background(), dataUUID); err != nil {
			t.Errorf("ScrubCancel: %v", err)
		}
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 64 << 20}, unix.EINTR
	}
	stubKernel(t, k)
	progressInterval = time.Hour // no tick reads the request first
	got, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "canceled" {
		t.Fatalf("got=%+v err=%v, want canceled", got, err)
	}
	calls := slices.DeleteFunc(k.Calls(), func(c string) bool { return c == "SCRUB_CANCEL" })
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want device 1 not issued again", calls)
	}
}

// A record a power loss left flagged shares its job and start with the
// resumed run, and after the reboot the job host may get the same pid and
// start tick: the boot tells them apart, so the resume does not cancel
// itself. A run that could not stamp its process takes no request.
func TestScrubTakesOnlyItsOwnCancelRequest(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := raid1Kernel()
	stubKernel(t, k)
	stale := scrubRecord{Status: "running", StartedAt: &first, Run: "job-1", CancelRequested: true}
	stale.writtenByThisProcess()
	stale.BootID = "the boot before the power loss"
	saveScrubRecord(context.Background(), dataUUID, stale)
	got, err := ResumeScrub(context.Background(), dataUUID, "job-1", nil, func(BtrfsScrubStatus) {})
	if err != nil || got.Status != "finished" || slices.Contains(k.Calls(), "SCRUB_CANCEL") {
		t.Fatalf("got=%+v err=%v calls=%v, want the resume to finish", got, err, k.Calls())
	}

	fs, err := FindFilesystem(context.Background(), dataUUID)
	if err != nil {
		t.Fatal(err)
	}
	r := newScrubRun(fs, scrubRecord{Status: "finished", StartedAt: &first, Run: "job-2"})
	r.rec.PID, r.rec.PIDStart, r.rec.BootID = 0, 0, ""
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "running", StartedAt: &first, Run: "job-2", CancelRequested: true})
	r.save(context.Background())
	if r.canceled || r.cancelRequested() {
		t.Fatal("a run without a process stamp took a cancel request")
	}
}

// When the kernel canceled the running calls, the scrub keeps canceling the
// rest by itself: the record is not flagged, so a scrub that starts next
// cannot take the request. Without a live scrub on record, ENOTCONN answers
// that none runs without the lock or a writable records file.
func TestScrubCancelFlagsOnlyWhenTheKernelHadNothing(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	first := time.Unix(1700000000, 0)
	live := scrubRecord{Status: "running", StartedAt: &first, Run: "job-1"}
	live.writtenByThisProcess()
	saveScrubRecord(context.Background(), dataUUID, live)
	if err := ScrubCancel(context.Background(), dataUUID); err != nil {
		t.Fatal(err)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.CancelRequested {
		t.Fatalf("record = %+v, flagged although the kernel canceled", rec)
	}
	k.scrubCancel = func() error { return unix.ENOTCONN }
	// A records folder that cannot exist, so its lock cannot be taken.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ScrubRecordPath = filepath.Join(blocker, "scrub.json")
	if err := ScrubCancel(context.Background(), dataUUID); !errors.Is(err, ErrScrubNotRunning) {
		t.Fatalf("err = %v, want ErrScrubNotRunning", err)
	}
}

// flushedAt and flushedEnd are where the flush tests' device 1 reports it
// stands while its call runs, and where the call ends.
const flushedAt, flushedEnd = 100 << 20, 200 << 20

// watchFlushes stubs flushDevice with fail and logs each flush, with device
// 1's offset on record at that moment, beside each poll of device 1. It
// returns the log so far.
func watchFlushes(fail func() bool) func() []string {
	var mu sync.Mutex
	var events []string
	note := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	poll := fsIoctl
	fsIoctl = func(mnt string, req uintptr, arg unsafe.Pointer) error {
		err := poll(mnt, req, arg)
		if req == iocScrubProgress && err == nil {
			if a := (*scrubArgs)(arg); a.DevID == 1 {
				note(fmt.Sprintf("poll %d", a.Progress.LastPhysical))
			}
		}
		return err
	}
	flushDevice = func(path string) error {
		rec, _ := loadScrubRecord(dataUUID)
		note(fmt.Sprintf("flush %s, saved %d", path, rec.Devices[1].Offset))
		if fail() {
			return unix.EIO
		}
		return nil
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(events)
	}
}

// flushKernel is a raid1Kernel, saving every tick, whose device 2 finishes at
// once and whose device 1 reports flushedAt, waits for hold, then finishes
// at flushedEnd.
func flushKernel(t *testing.T, hold func()) {
	t.Helper()
	k := raid1Kernel()
	k.devices[1].uuid, k.devices[2].uuid = devUUID(1), devUUID(2)
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			return scrubProgress{LastPhysical: 50 << 20}, nil
		}
		k.setRunning(1, &scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: flushedAt})
		hold()
		k.setRunning(1, nil)
		return scrubProgress{DataBytesScrubbed: 2 << 20, LastPhysical: flushedEnd}, nil
	}
	stubKernel(t, k)
	saveEveryTick(t)
}

// The kernel's repair writes carry no flush, so a saved offset could outlive
// the repairs below it in a power cut. Before a save moves an offset or marks
// a device done, and before the outcome, the scrub flushes each device it
// scrubs: poll, flush, write.
func TestScrubFlushesBeforeSavingAnOffset(t *testing.T) {
	flushKernel(t, func() {
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[1].Offset == flushedAt })
	})
	events := watchFlushes(func() bool { return false })
	if got, err := ScrubFrom(t.Context(), dataUUID, "job-1", func(BtrfsScrubStatus) {}); err != nil || got.Status != "finished" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	log := events()
	polled := slices.Index(log, fmt.Sprintf("poll %d", flushedAt))
	if polled < 0 {
		t.Fatalf("events %v: device 1's offset was never polled", log)
	}
	// The first save after the poll flushes both devices, then writes.
	for _, dev := range []string{"/dev/sda1", "/dev/sdb1"} {
		i := slices.IndexFunc(log[polled:], func(e string) bool { return strings.HasPrefix(e, "flush "+dev) })
		if i < 0 || log[polled+i] != fmt.Sprintf("flush %s, saved 0", dev) {
			t.Fatalf("events %v: %s not flushed between the poll and the write of offset %d", log, dev, flushedAt)
		}
	}
	// The outcome is written after a last flush.
	last := log[len(log)-1]
	if !strings.HasPrefix(last, "flush ") || strings.HasSuffix(last, fmt.Sprintf("saved %d", flushedEnd)) {
		t.Fatalf("events %v: no flush before the outcome", log)
	}
	if rec, _ := loadScrubRecord(dataUUID); rec.Status != "finished" || rec.Devices[1].Offset != flushedEnd || !rec.Devices[1].Done {
		t.Fatalf("record = %+v", rec)
	}
}

// A failed flush skips the save, so the record keeps the offsets last
// flushed until a flush succeeds again.
func TestScrubFailedFlushKeepsTheOldOffsets(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	var failed atomic.Int32
	flushKernel(t, func() {
		deadline := time.Now().Add(5 * time.Second)
		for failed.Load() < 10 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if rec, _ := loadScrubRecord(dataUUID); failed.Load() < 10 || rec.Devices[1].Offset != 0 || rec.Devices[2].Done {
			t.Errorf("record after %d failed flushes = %+v, want the offsets of the start", failed.Load(), rec)
		}
		failing.Store(false)
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[1].Offset == flushedAt && r.Devices[2].Done })
	})
	watchFlushes(func() bool {
		if failing.Load() {
			failed.Add(1)
			return true
		}
		return false
	})
	if got, err := ScrubFrom(t.Context(), dataUUID, "job-1", func(BtrfsScrubStatus) {}); err != nil || got.Status != "finished" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

// A failed flush at the end fails the scrub and leaves the devices as last
// flushed, aborted, not a finished scrub a resync would count.
func TestScrubFailedFinalFlushFailsTheScrub(t *testing.T) {
	var final atomic.Bool
	flushKernel(t, func() {
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[1].Offset == flushedAt && r.Devices[2].Done })
		final.Store(true)
	})
	watchFlushes(final.Load)
	got, err := ScrubFrom(t.Context(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if !errors.Is(err, unix.EIO) || got.Status == "finished" {
		t.Fatalf("got=%+v err=%v, want the failed flush to fail the scrub", got, err)
	}
	// The counters too are those of the last flushed save: device 1's
	// running call had read 1 MiB then, and the 2 MiB its call returned
	// since are read again by whatever continues from those offsets.
	want := map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: flushedAt}, 2: {UUID: devUUID(2), Offset: 50 << 20, Done: true}}
	if rec, _ := loadScrubRecord(dataUUID); rec.Status != "aborted" || rec.FinishedAt == nil || rec.BytesScrubbed != 1<<20 || !reflect.DeepEqual(rec.Devices, want) {
		t.Fatalf("record = %+v, want it aborted with 1 MiB and the devices as last flushed, %+v", rec, want)
	}
}

// The path DEV_INFO reports can name no device node: /dev/root for a root
// filesystem mounted without an initramfs, or a disk's old name. The flush
// goes to each disk the kernel lists as a member, by its kernel name ("!"
// standing for "/"), whatever path DEV_INFO reports.
func TestScrubFlushesThePoolsDisksByKernelName(t *testing.T) {
	k := raid1Kernel()
	k.devices[1].path = "/dev/root"
	stubKernel(t, k)
	for _, name := range []string{"sda2", "cciss!c0d0"} {
		if err := os.MkdirAll(filepath.Join(sysFSBtrfs, dataUUID, "devices", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(sysFSBtrfs, dataUUID, "devices", "root")); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	flushed := map[string]bool{}
	flushDevice = func(path string) error {
		mu.Lock()
		defer mu.Unlock()
		flushed[path] = true
		return nil
	}
	if got, err := ScrubFrom(t.Context(), dataUUID, "job-1", func(BtrfsScrubStatus) {}); err != nil || got.Status != "finished" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if want := map[string]bool{"/dev/sda2": true, "/dev/sdb1": true, "/dev/cciss/c0d0": true}; !reflect.DeepEqual(flushed, want) {
		t.Fatalf("flushed %v, want %v", flushed, want)
	}
}

// A save a failed flush skips still takes over a cancel requested since the
// last tick, as a save that writes does, so an interrupted call is not
// issued again only to wait for a thaw.
func TestScrubSkippedSaveStillTakesACancel(t *testing.T) {
	k := raid1Kernel()
	k.scrubCancel = func() error { return unix.ENOTCONN }
	var failing atomic.Bool
	k.scrub = func(id, start uint64) (scrubProgress, error) {
		switch {
		case id == 2:
			return scrubProgress{LastPhysical: 1 << 30}, nil
		case start != 0:
			return scrubProgress{LastPhysical: 1 << 30}, nil // issued again
		}
		waitForRecord(t, func(r scrubRecord) bool { return r.Devices[2].Done })
		failing.Store(true)
		if err := ScrubCancel(context.Background(), dataUUID); err != nil {
			t.Errorf("ScrubCancel: %v", err)
		}
		return scrubProgress{DataBytesScrubbed: 1 << 20, LastPhysical: 64 << 20}, unix.EINTR
	}
	stubKernel(t, k)
	flushDevice = func(string) error {
		if failing.Load() {
			return unix.EIO
		}
		return nil
	}
	progressInterval = time.Hour // no tick reads the request first
	_, err := ScrubFrom(context.Background(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	calls := slices.DeleteFunc(k.Calls(), func(c string) bool { return c == "SCRUB_CANCEL" })
	slices.Sort(calls)
	if want := []string{"SCRUB 1", "SCRUB 2"}; !errors.Is(err, unix.EIO) || !reflect.DeepEqual(calls, want) {
		t.Fatalf("err=%v calls=%v, want the failed flush and device 1 not issued again", err, calls)
	}
}

// A member list shorter than the pool's present devices, an empty one
// included, cannot cover them all: the flush fails rather than passing, so
// no offset is saved unflushed and the scrub does not end finished.
func TestScrubFlushRefusesAShortMemberList(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if err := os.Remove(filepath.Join(sysFSBtrfs, dataUUID, "devices", "sdb1")); err != nil {
		t.Fatal(err)
	}
	got, err := ScrubFrom(t.Context(), dataUUID, "job-1", func(BtrfsScrubStatus) {})
	if err == nil || got.Status == "finished" || !strings.Contains(err.Error(), "lists 1 disks of pool") {
		t.Fatalf("got=%+v err=%v, want the short member list to fail the flush", got, err)
	}
}
