package storage

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// fakeDrive is what smartctl reads of /dev/sda: its JSON report, which a test
// changes as the drive's state changes, and the self-tests started and
// aborted on it.
type fakeDrive struct {
	mu      sync.Mutex
	report  string
	reads   int
	readErr error
	// next is the report after the next read, as a drive that moves on.
	next    []string
	starts  []string
	aborts  []string
	saved   []smartCheckpoint
	startOK chan struct{}
	// startEnd, when set, is how smartctl -t ends, once it started.
	startEnd func(ctx context.Context) error
}

func newFakeDrive(t *testing.T, report string) *fakeDrive {
	t.Helper()
	d := &fakeDrive{report: report, startOK: make(chan struct{}, 4)}
	previousJSON, previousStart, previousAbort, previousSave, previousInterval := smartctlJSON, startSmartTest, abortSmartTest, saveSmartCheckpoint, pollInterval
	t.Cleanup(func() {
		smartctlJSON, startSmartTest, abortSmartTest, saveSmartCheckpoint, pollInterval = previousJSON, previousStart, previousAbort, previousSave, previousInterval
	})
	pollInterval = func(string) time.Duration { return time.Millisecond }
	smartctlJSON = func(ctx context.Context, device string) ([]byte, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if device != "sda" {
			return nil, fmt.Errorf("no drive %s", device)
		}
		d.reads++
		if d.readErr != nil {
			return nil, d.readErr
		}
		report := d.report
		if len(d.next) > 0 {
			d.report, d.next = d.next[0], d.next[1:]
		}
		return []byte(report), nil
	}
	startSmartTest = func(ctx context.Context, device, testType string) error {
		d.mu.Lock()
		d.starts = append(d.starts, device+" "+testType)
		end := d.startEnd
		d.mu.Unlock()
		d.startOK <- struct{}{}
		if end != nil {
			return end(ctx)
		}
		return nil
	}
	abortSmartTest = func(_ context.Context, device string) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.aborts = append(d.aborts, device)
		return nil
	}
	saveSmartCheckpoint = func(_ context.Context, value any) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		cp, ok := value.(smartCheckpoint)
		if !ok {
			t.Errorf("saved %T, want a smartCheckpoint", value)
		}
		if cp.Started != (len(d.starts) > 0) {
			t.Errorf("checkpoint says started %t after %d starts", cp.Started, len(d.starts))
		}
		d.saved = append(d.saved, cp)
		return nil
	}
	return d
}

// then makes the drive read as reports, one per read, after the current one.
func (d *fakeDrive) then(reports ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next = append(d.next, reports...)
}

func (d *fakeDrive) set(report string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.report = report
}

func (d *fakeDrive) calls() (starts, aborts []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.starts), slices.Clone(d.aborts)
}

// ataTest is one entry of an ATA drive's self-test log: kind 1 short, 2
// extended; status the status byte.
type ataTest struct {
	kind, status, hours int
	text                string
}

// ataReport is smartctl's JSON report of an ATA drive with serial, running a
// test with remaining percent left (-1 for none) after its last test ended
// with lastStatus, and log, newest first.
func ataReport(serial string, remaining, lastStatus int, log ...ataTest) string {
	status := map[string]any{"value": lastStatus, "string": "last test"}
	switch {
	case remaining >= 0:
		status = map[string]any{"value": 0xf0 | remaining/10, "string": "in progress", "remaining_percent": remaining}
	case lastStatus>>4 == 0:
		status["passed"] = true
	case lastStatus>>4 > 2:
		status["passed"] = false
	}
	table := []any{}
	for _, entry := range log {
		table = append(table, map[string]any{
			"type":           map[string]any{"value": entry.kind, "string": "test"},
			"status":         map[string]any{"value": entry.status, "string": entry.text},
			"lifetime_hours": entry.hours,
		})
	}
	report := map[string]any{
		"model_name":    "Disk 1000",
		"serial_number": serial,
		"ata_smart_data": map[string]any{
			"self_test": map[string]any{"status": status},
		},
		"ata_smart_self_test_log": map[string]any{"standard": map[string]any{"table": table}},
	}
	// A drive behind some USB bridges reports neither.
	if serial != "" {
		report["wwn"] = map[string]any{"naa": 5, "oui": 3152, "id": 77}
	}
	raw, err := json.Marshal(report)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// nvmeReport is smartctl's JSON report of an NVMe drive running the test of
// code op (0 for none), with log entries of {code, result, hours}.
func nvmeReport(serial string, op int, log ...[3]int) string {
	table := []any{}
	for _, entry := range log {
		table = append(table, map[string]any{
			"self_test_code":   map[string]any{"value": entry[0]},
			"self_test_result": map[string]any{"value": entry[1], "string": fmt.Sprintf("result %d", entry[1])},
			"power_on_hours":   entry[2],
		})
	}
	raw, err := json.Marshal(map[string]any{
		"model_name":    "NVMe 1000",
		"serial_number": serial,
		"nvme_self_test_log": map[string]any{
			"current_self_test_operation":          map[string]any{"value": op},
			"current_self_test_completion_percent": 40,
			"table":                                table,
		},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

var (
	earlierShort = ataTest{kind: 1, status: 0x00, hours: 100, text: "Completed without error"}
	earlierLong  = ataTest{kind: 2, status: 0x00, hours: 90, text: "Completed without error"}
)

// inSmartTask runs fn inside a real task, as a job and its Resume hook run,
// and returns the task once fn has returned.
func inSmartTask(t *testing.T, fn func(ctx context.Context, task *bridgeipc.Task) (any, error)) (*bridgeipc.Task, <-chan error) {
	t.Helper()
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity("storage.run_smart_test", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	done := make(chan error, 1)
	task.Start(func(ctx context.Context, task *bridgeipc.Task, _ any) (any, error) {
		result, err := fn(ctx, task)
		done <- err
		return result, err
	})
	return task, done
}

func waitSmart(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not end")
		return nil
	}
}

func shortCheckpoint(t *testing.T, serial string, log ...ataTest) jsontext.Value {
	t.Helper()
	drive, err := driveFrom(ataReport(serial, -1, 0, log...))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(smartCheckpoint{Test: "short", Identity: drive.identity, Log: drive.log, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// driveFrom parses a report as readSmartDrive does.
func driveFrom(report string) (smartDrive, error) {
	previous := smartctlJSON
	defer func() { smartctlJSON = previous }()
	smartctlJSON = func(context.Context, string) ([]byte, error) { return []byte(report), nil }
	return readSmartDrive(context.Background(), "sda")
}

var shortRequest = apischema.DeviceTestTypeRequest{Device: "sda", TestType: "short"}

// The job reads who the drive is and its self-test log, and saves them,
// before it starts the test, and saves again that it started it.
func TestSmartTestSavesTheDriveBeforeItStarts(t *testing.T) {
	drive := newFakeDrive(t, ataReport("S1", -1, 0, earlierShort))
	drive.then(ataReport("S1", 50, 0, earlierShort), ataReport("S1", -1, 0, ataTest{kind: 1, status: 0, hours: 101, text: "Completed without error"}, earlierShort))

	_, done := inSmartTask(t, func(ctx context.Context, task *bridgeipc.Task) (any, error) {
		return runSmartTestTask(ctx, task, shortRequest)
	})
	if err := waitSmart(t, done); err != nil {
		t.Fatalf("self-test: %v", err)
	}
	if len(drive.saved) != 2 || drive.saved[0].Started || !drive.saved[1].Started {
		t.Fatalf("saved %+v, want a checkpoint before the start and one after", drive.saved)
	}
	cp := drive.saved[1]
	if cp.Test != "short" || cp.Identity.Serial != "S1" || cp.Identity.WWN == "" || len(cp.Log) != 1 {
		t.Fatalf("checkpoint = %+v, want the short test, the drive's serial and WWN and its log", cp)
	}
	if starts, _ := drive.calls(); !slices.Equal(starts, []string{"sda short"}) {
		t.Fatalf("started %v, want one short test", starts)
	}
}

// Picked up while the drive still runs the test, the job follows it to its
// end and starts none.
func TestSmartPickUpFollowsATestTheDriveStillRuns(t *testing.T) {
	drive := newFakeDrive(t, ataReport("S1", 70, 0, earlierShort))
	drive.then(ataReport("S1", 40, 0, earlierShort), ataReport("S1", -1, 0, ataTest{kind: 1, status: 0, hours: 101, text: "Completed without error"}, earlierShort))
	checkpoint := shortCheckpoint(t, "S1", earlierShort)

	var result SmartTestResult
	_, done := inSmartTask(t, func(ctx context.Context, _ *bridgeipc.Task) (any, error) {
		var err error
		result, err = resumeSmartTest(ctx, shortRequest, checkpoint)
		return result, err
	})
	if err := waitSmart(t, done); err != nil || !result.Success {
		t.Fatalf("pick-up = %+v, %v; want the followed test's success", result, err)
	}
	if starts, aborts := drive.calls(); len(starts) != 0 || len(aborts) != 0 {
		t.Fatalf("started %v and aborted %v, want neither", starts, aborts)
	}
}

// An ATA drive may log its running test, which pushes the oldest test out of
// a full log: the job still takes the test it runs for its own.
func TestSmartPickUpFollowsATestTheDriveLogsAsRunning(t *testing.T) {
	older := []ataTest{earlierShort, earlierLong, {kind: 1, status: 0, hours: 80, text: "Completed without error"}}
	running := ataTest{kind: 1, status: 0xf7, hours: 101, text: "Self-test routine in progress"}
	drive := newFakeDrive(t, ataReport("S1", 70, 0, running, older[0], older[1]))
	drive.then(ataReport("S1", -1, 0, ataTest{kind: 1, status: 0, hours: 101, text: "Completed without error"}, older[0], older[1]))

	var result SmartTestResult
	_, done := inSmartTask(t, func(ctx context.Context, _ *bridgeipc.Task) (any, error) {
		var err error
		result, err = resumeSmartTest(ctx, shortRequest, shortCheckpoint(t, "S1", older...))
		return result, err
	})
	if err := waitSmart(t, done); err != nil || !result.Success {
		t.Fatalf("pick-up = %+v, %v; want the followed test's success", result, err)
	}

	// A running test of another kind is not the job's.
	newFakeDrive(t, ataReport("S1", 70, 0, ataTest{kind: 2, status: 0xf7, hours: 101}, older[0], older[1]))
	if _, err := resumeSmartTest(context.Background(), shortRequest, shortCheckpoint(t, "S1", older...)); !errors.Is(err, bridgeipc.ErrNotResumed) {
		t.Fatalf("pick-up of an extended test = %v, want the job left interrupted", err)
	}
}

// A cancel while the pick-up reads the drive ends the job canceled, not
// interrupted.
func TestSmartPickUpCanceledWhileReading(t *testing.T) {
	newFakeDrive(t, ataReport("S1", 70, 0, earlierShort))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resumeSmartTest(ctx, shortRequest, shortCheckpoint(t, "S1", earlierShort)); !errors.Is(err, context.Canceled) || errors.Is(err, bridgeipc.ErrNotResumed) {
		t.Fatalf("pick-up = %v, want canceled", err)
	}
}

// Picked up after the test ended, the job reads its outcome from the drive's
// self-test log, which the drive keeps across a reboot.
func TestSmartPickUpReadsHowAnEndedTestWent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		report      string
		wantErr     string
		interrupted bool // the job stays interrupted
	}{
		{name: "completed", report: ataReport("S1", -1, 0, ataTest{kind: 1, status: 0x00, hours: 101, text: "Completed without error"}, earlierShort)},
		{name: "read failure", report: ataReport("S1", -1, 0x73, ataTest{kind: 1, status: 0x73, hours: 101, text: "Completed: read failure"}, earlierShort), wantErr: "Completed: read failure"},
		{name: "aborted by the host", report: ataReport("S1", -1, 0x10, ataTest{kind: 1, status: 0x10, hours: 101, text: "Aborted by host"}, earlierShort), wantErr: "Aborted by host"},
		{name: "interrupted by a reset", report: ataReport("S1", -1, 0x20, ataTest{kind: 1, status: 0x20, hours: 101, text: "Interrupted (host reset)"}, earlierShort), wantErr: "interrupted by a reset", interrupted: true},
		// Ours ended, and a test someone started since runs.
		{name: "completed, another running", report: ataReport("S1", 80, 0, ataTest{kind: 1, status: 0x00, hours: 101, text: "Completed without error"}, earlierShort)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drive := newFakeDrive(t, tc.report)
			result, err := resumeSmartTest(context.Background(), shortRequest, shortCheckpoint(t, "S1", earlierShort))
			switch {
			case tc.wantErr == "" && (err != nil || !result.Success):
				t.Fatalf("pick-up = %+v, %v; want success", result, err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("pick-up = %v, want %q", err, tc.wantErr)
			case errors.Is(err, bridgeipc.ErrNotResumed) != tc.interrupted:
				t.Fatalf("pick-up = %v, left interrupted %t; want %t", err, errors.Is(err, bridgeipc.ErrNotResumed), tc.interrupted)
			}
			if starts, aborts := drive.calls(); len(starts) != 0 || len(aborts) != 0 {
				t.Fatalf("started %v and aborted %v, want neither", starts, aborts)
			}
		})
	}
}

// When the drive cannot say what became of the job's test, or is not the
// job's drive, the job stays interrupted and nothing is started or aborted.
func TestSmartPickUpLeavesInterruptedWhatTheDriveCannotTell(t *testing.T) {
	newer := ataTest{kind: 1, status: 0, hours: 101, text: "Completed without error"}
	for _, tc := range []struct {
		name       string
		checkpoint func(t *testing.T) jsontext.Value
		report     string
		readErr    error
	}{
		{name: "no checkpoint", checkpoint: func(*testing.T) jsontext.Value { return nil }, report: ataReport("S1", -1, 0, newer, earlierShort)},
		{name: "another drive", report: ataReport("S2", -1, 0, newer, earlierShort)},
		{name: "another drive running a test", report: ataReport("S2", 50, 0, earlierShort)},
		{name: "unreadable", readErr: errors.New("device gone")},
		{name: "never started", report: ataReport("S1", -1, 0, earlierShort)},
		{name: "two tests since", report: ataReport("S1", -1, 0, newer, newer, earlierShort)},
		{name: "a test of another kind", report: ataReport("S1", -1, 0, ataTest{kind: 2, status: 0, hours: 101}, earlierShort)},
		{name: "a log that does not continue", report: ataReport("S1", -1, 0, newer, earlierLong)},
		{name: "no serial", checkpoint: func(t *testing.T) jsontext.Value { return shortCheckpoint(t, "", earlierShort) }, report: ataReport("", -1, 0, newer, earlierShort)},
		// Its host died between the save and the start: the test the drive
		// runs, or the one it logged, may be another program's.
		{name: "not started, a test running", checkpoint: notStarted, report: ataReport("S1", 50, 0, earlierShort)},
		{name: "not started, a test logged", checkpoint: notStarted, report: ataReport("S1", -1, 0, newer, earlierShort)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drive := newFakeDrive(t, tc.report)
			drive.readErr = tc.readErr
			checkpoint := shortCheckpoint(t, "S1", earlierShort)
			if tc.checkpoint != nil {
				checkpoint = tc.checkpoint(t)
			}
			if _, err := resumeSmartTest(context.Background(), shortRequest, checkpoint); !errors.Is(err, bridgeipc.ErrNotResumed) {
				t.Fatalf("pick-up = %v, want the job left interrupted", err)
			}
			if starts, aborts := drive.calls(); len(starts) != 0 || len(aborts) != 0 {
				t.Fatalf("started %v and aborted %v, want neither", starts, aborts)
			}
		})
	}
}

func notStarted(t *testing.T) jsontext.Value {
	t.Helper()
	var cp smartCheckpoint
	if err := json.Unmarshal(shortCheckpoint(t, "S1", earlierShort), &cp); err != nil {
		t.Fatal(err)
	}
	cp.Started = false
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// An NVMe drive says which test it runs: a short job never takes an extended
// test, and does take its own.
func TestSmartPickUpOfAnNVMeDriveChecksTheTestItRuns(t *testing.T) {
	earlier := [3]int{1, 0, 10}
	cp := func(t *testing.T) jsontext.Value {
		t.Helper()
		drive, err := driveFrom(nvmeReport("N1", 0, earlier))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(smartCheckpoint{Test: "short", Identity: drive.identity, Log: drive.log, Started: true})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	nvme := apischema.DeviceTestTypeRequest{Device: "sda", TestType: "short"}

	newFakeDrive(t, nvmeReport("N1", 2, earlier))
	if _, err := resumeSmartTest(context.Background(), nvme, cp(t)); !errors.Is(err, bridgeipc.ErrNotResumed) {
		t.Fatalf("pick-up of an extended test = %v, want the job left interrupted", err)
	}
	newFakeDrive(t, nvmeReport("N1", 0, [3]int{1, 0, 11}, earlier))
	if result, err := resumeSmartTest(context.Background(), nvme, cp(t)); err != nil || !result.Success {
		t.Fatalf("pick-up of its ended test = %+v, %v; want success", result, err)
	}
}

// Cancel after a pick-up aborts the test in the drive, once it is still the
// job's drive; a stop of the job host leaves the test running.
func TestSmartCancelAfterPickUpAbortsTheTest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stop      func(*bridgeipc.Task)
		swapped   bool
		wantAbort bool
	}{
		{name: "cancel", stop: (*bridgeipc.Task).Cancel, wantAbort: true},
		{name: "cancel after the drive was swapped", stop: (*bridgeipc.Task).Cancel, swapped: true},
		{name: "host stop", stop: func(task *bridgeipc.Task) { task.BeginInterrupt(); task.Interrupt() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drive := newFakeDrive(t, ataReport("S1", 70, 0, earlierShort))
			checkpoint := shortCheckpoint(t, "S1", earlierShort)
			task, done := inSmartTask(t, func(ctx context.Context, _ *bridgeipc.Task) (any, error) {
				return resumeSmartTest(ctx, shortRequest, checkpoint)
			})
			waitFor(t, "the pick-up to follow the test", func() bool {
				drive.mu.Lock()
				defer drive.mu.Unlock()
				return drive.reads >= 3
			})
			if tc.swapped {
				drive.set(ataReport("S2", 70, 0, earlierShort))
			}
			tc.stop(task)
			if err := waitSmart(t, done); !errors.Is(err, context.Canceled) {
				t.Fatalf("pick-up = %v, want stopped", err)
			}
			_, aborts := drive.calls()
			if want := tc.wantAbort; (len(aborts) == 1 && aborts[0] == "sda") != want || len(aborts) > 1 {
				t.Fatalf("aborted %v, want an abort of sda %t", aborts, want)
			}
		})
	}
}

// Cancel in the first run aborts the test it started, on a drive it can
// know again by its serial number or WWN.
func TestSmartCancelAbortsTheTestItStarted(t *testing.T) {
	for _, tc := range []struct {
		serial    string
		wantAbort []string
	}{
		{serial: "S1", wantAbort: []string{"sda"}},
		// Any drive of the model could have its name by now.
		{serial: ""},
	} {
		t.Run("serial "+tc.serial, func(t *testing.T) {
			drive := newFakeDrive(t, ataReport(tc.serial, -1, 0, earlierShort))
			drive.then(ataReport(tc.serial, 70, 0, earlierShort))
			task, done := inSmartTask(t, func(ctx context.Context, task *bridgeipc.Task) (any, error) {
				return runSmartTestTask(ctx, task, shortRequest)
			})
			<-drive.startOK
			task.Cancel()
			if err := waitSmart(t, done); !errors.Is(err, context.Canceled) {
				t.Fatalf("self-test = %v, want canceled", err)
			}
			if _, aborts := drive.calls(); !slices.Equal(aborts, tc.wantAbort) {
				t.Fatalf("aborted %v, want %v", aborts, tc.wantAbort)
			}
		})
	}
}

// The self-test log continues across new tests, also once it is full.
func TestNewSelfTests(t *testing.T) {
	a, b, c, d := selfTestEntry{Kind: "short", Hours: 1}, selfTestEntry{Kind: "short", Hours: 2}, selfTestEntry{Kind: "long", Hours: 3}, selfTestEntry{Kind: "short", Hours: 4}
	for _, tc := range []struct {
		name      string
		base, cur []selfTestEntry
		want      []selfTestEntry
		ok        bool
	}{
		{name: "empty", ok: true},
		{name: "first test", cur: []selfTestEntry{a}, want: []selfTestEntry{a}, ok: true},
		{name: "nothing new", base: []selfTestEntry{b, a}, cur: []selfTestEntry{b, a}, ok: true},
		{name: "one new", base: []selfTestEntry{b, a}, cur: []selfTestEntry{c, b, a}, want: []selfTestEntry{c}, ok: true},
		{name: "full ring, one new", base: []selfTestEntry{c, b, a}, cur: []selfTestEntry{d, c, b}, want: []selfTestEntry{d}, ok: true},
		{name: "full ring, two new", base: []selfTestEntry{c, b, a}, cur: []selfTestEntry{a, d, c}, want: []selfTestEntry{a, d}, ok: true},
		{name: "cleared", base: []selfTestEntry{b, a}, cur: []selfTestEntry{c}},
		{name: "rewritten", base: []selfTestEntry{b, a}, cur: []selfTestEntry{d, c}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := newSelfTests(tc.base, tc.cur)
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Fatalf("newSelfTests = %v, %t; want %v, %t", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// Cancel aborts only a test the job started and the drive still runs as
// the job's: not one smartctl refused to start beside another, not a test of
// another kind, not when the drive runs none.
func TestSmartCancelAbortsOnlyTheJobsTest(t *testing.T) {
	refused := func(ctx context.Context) error {
		<-ctx.Done()
		return errors.New("Can't start self-test without aborting current test")
	}
	for _, tc := range []struct {
		name     string
		running  string // the drive's report while the job runs
		startEnd func(context.Context) error
	}{
		{name: "start refused beside another test", running: ataReport("S1", 50, 0, earlierShort), startEnd: refused},
		{name: "another kind running", running: nvmeReport("S1", 2, [3]int{1, 0, 10})},
		{name: "no test running", running: ataReport("S1", -1, 0, earlierShort)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := ataReport("S1", -1, 0, earlierShort)
			if strings.Contains(tc.running, "nvme") {
				baseline = nvmeReport("S1", 0, [3]int{1, 0, 10})
			}
			drive := newFakeDrive(t, baseline)
			drive.then(tc.running)
			drive.startEnd = tc.startEnd
			task, done := inSmartTask(t, func(ctx context.Context, task *bridgeipc.Task) (any, error) {
				return runSmartTestTask(ctx, task, shortRequest)
			})
			<-drive.startOK
			task.Cancel()
			if err := waitSmart(t, done); err == nil {
				t.Fatal("the canceled job succeeded")
			}
			if _, aborts := drive.calls(); len(aborts) != 0 {
				t.Fatalf("aborted %v, want no abort", aborts)
			}
		})
	}
}
