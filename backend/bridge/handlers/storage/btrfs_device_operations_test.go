package storage

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const testBtrfsUUID = "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d"

func TestCheckBtrfsDeviceFree(t *testing.T) {
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/sdb", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdc", Type: "disk", FSType: "ext4", Mountpoints: []string{}},
		{Path: "/dev/sdd", Type: "disk", FSType: "ext4", Mountpoints: []string{"/srv"}},
		{Path: "/dev/sde", Type: "disk", FSType: "btrfs", Mountpoints: []string{}},
		{Path: "/dev/sdf", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdf1", Type: "part", Mountpoints: []string{}, Parents: []string{"/dev/sdf"}},
	}
	filesystems := []btrfs.BtrfsFilesystem{{UUID: testBtrfsUUID, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/sde"}}}}
	cases := []struct {
		device  string
		force   bool
		wantErr string
	}{
		{"/dev/sdb", false, ""},
		{"/dev/sdc", true, ""},
		{"/dev/sdc", false, "has a ext4 signature"},
		{"/dev/sdd", true, "is mounted"},
		{"/dev/sde", true, "already belongs to btrfs filesystem"},
		{"/dev/sdf", true, "has partitions or holders"},
		{"/dev/nope", true, "not found"},
	}
	for _, c := range cases {
		err := checkBtrfsDeviceFree(devices, filesystems, c.device, c.force)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s force=%v: err = %v", c.device, c.force, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s force=%v: err = %v, want %q", c.device, c.force, err, c.wantErr)
		}
	}
}

func TestScrubStartsRoundTrip(t *testing.T) {
	oldPositions, oldCancel := btrfsScrubPositions, btrfsScrubCancel
	t.Cleanup(func() { btrfsScrubPositions, btrfsScrubCancel = oldPositions, oldCancel })
	btrfsScrubCancel = func(context.Context, string) error { return btrfs.ErrScrubNotRunning }
	for _, positions := range []map[int]uint64{{2: 7}, nil} {
		btrfsScrubPositions = func(context.Context, string) (map[int]uint64, error) { return positions, nil }
		checkpoint, err := suspendBtrfsScrub(context.Background(), apischema.UUIDRequest{UUID: testBtrfsUUID}, nil)
		if err != nil {
			t.Fatalf("suspend with %v: %v", positions, err)
		}
		starts, err := scrubStarts(checkpoint)
		if err != nil || !reflect.DeepEqual(starts, positions) {
			t.Fatalf("checkpoint %s gives %v err=%v, want %v", checkpoint, starts, err, positions)
		}
	}
}

func TestScrubBtrfsChecksTheStatus(t *testing.T) {
	old := btrfsScrubFrom
	t.Cleanup(func() { btrfsScrubFrom = old })
	for status, want := range map[string]error{"finished": nil, "canceled": context.Canceled, "aborted": errors.New("any")} {
		btrfsScrubFrom = func(context.Context, string, map[int]uint64, func(btrfs.BtrfsScrubStatus)) (btrfs.BtrfsScrubStatus, error) {
			return btrfs.BtrfsScrubStatus{Status: status}, nil
		}
		_, err := scrubBtrfs(context.Background(), nil, testBtrfsUUID, nil)
		if (err == nil) != (want == nil) || errors.Is(want, context.Canceled) != errors.Is(err, context.Canceled) {
			t.Fatalf("status %s: err = %v, want %v", status, err, want)
		}
	}
}

// inTask runs fn inside a real task, interrupted or not, as Resume hooks run.
func inTask(t *testing.T, interrupted bool, fn func(ctx context.Context)) {
	t.Helper()
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity("test.resume", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	task.Start(func(ctx context.Context, task *bridgeipc.Task, _ any) (any, error) {
		if interrupted {
			task.BeginInterrupt()
		}
		fn(ctx)
		return nil, nil
	})
	<-task.Done()
}

// hooksOf runs Suspend, then Resume from its checkpoint.
func hooksOf[Req any](req Req, suspend func(context.Context, Req, jsontext.Value) (jsontext.Value, error), resume func(context.Context, Req, jsontext.Value) (btrfs.BtrfsFilesystem, error)) func(context.Context) (jsontext.Value, error, error) {
	return func(ctx context.Context) (jsontext.Value, error, error) {
		cp, err := suspend(ctx, req, nil)
		_, rerr := resume(ctx, req, cp)
		return cp, err, rerr
	}
}

// callIn names a call, marking one made with a context that already ended.
func callIn(ctx context.Context, call string) string {
	if ctx.Err() != nil {
		return call + " (ended context)"
	}
	return call
}

// replaceFollow is how following a replace ends: with err, or because the
// user cancels the job; a follow after the kernel cancel ends afterCancel.
type replaceFollow struct {
	err, afterCancel error
	userCancels      bool
}

func (f replaceFollow) next(ctx context.Context, canceled bool) error {
	switch {
	case canceled:
		return f.afterCancel
	case f.userCancels:
		task, _ := bridgeipc.TaskFromContext(ctx)
		task.Cancel()
		return ctx.Err()
	}
	return f.err
}

// TestBtrfsResumeHooks drives each resumable route's Suspend and Resume
// through the btrfs seams: what Suspend checkpoints, and which btrfs calls
// Resume makes from it.
func TestBtrfsResumeHooks(t *testing.T) {
	var calls []string
	record := func(format string, args ...any) { calls = append(calls, fmt.Sprintf(format, args...)) }
	var (
		paused    bool
		positions map[int]uint64
		follow    replaceFollow
	)
	oldPause, oldResumeBalance, oldBalanceResult := btrfsPauseBalance, btrfsResumeBalance, btrfsBalanceResult
	oldPositions, oldScrubCancel, oldScrubFrom := btrfsScrubPositions, btrfsScrubCancel, btrfsScrubFrom
	oldResumeReplace, oldCancelReplace := btrfsResumeReplace, btrfsCancelReplace
	oldResumeRemove, oldFind := btrfsResumeRemove, btrfsFindFilesystem
	t.Cleanup(func() {
		btrfsPauseBalance, btrfsResumeBalance, btrfsBalanceResult = oldPause, oldResumeBalance, oldBalanceResult
		btrfsScrubPositions, btrfsScrubCancel, btrfsScrubFrom = oldPositions, oldScrubCancel, oldScrubFrom
		btrfsResumeReplace, btrfsCancelReplace = oldResumeReplace, oldCancelReplace
		btrfsResumeRemove, btrfsFindFilesystem = oldResumeRemove, oldFind
	})
	btrfsPauseBalance = func(context.Context, string) (bool, error) {
		record("pause-balance")
		return paused, nil
	}
	btrfsResumeBalance = func(_ context.Context, _ string, _ btrfs.BalanceOptions, wasPaused bool, _ func(float64)) error {
		record("resume-balance paused=%v", wasPaused)
		return nil
	}
	btrfsBalanceResult = func(context.Context, string, *string, *string) (btrfs.BtrfsFilesystem, error) {
		record("balance-result")
		return btrfs.BtrfsFilesystem{}, nil
	}
	btrfsScrubPositions = func(context.Context, string) (map[int]uint64, error) {
		record("scrub-positions")
		return positions, nil
	}
	btrfsScrubCancel = func(context.Context, string) error {
		record("scrub-cancel")
		return nil
	}
	btrfsScrubFrom = func(_ context.Context, _ string, starts map[int]uint64, _ func(btrfs.BtrfsScrubStatus)) (btrfs.BtrfsScrubStatus, error) {
		record("scrub-from %v nil=%v", starts, starts == nil)
		return btrfs.BtrfsScrubStatus{Status: "finished"}, nil
	}
	btrfsResumeReplace = func(ctx context.Context, _ string, id int, _ time.Time, _ func(float64)) error {
		record("%s", callIn(ctx, fmt.Sprintf("resume-replace %d", id)))
		return follow.next(ctx, slices.Contains(calls, "cancel-replace 3"))
	}
	btrfsCancelReplace = func(ctx context.Context, _ string, id int) error {
		record("%s", callIn(ctx, fmt.Sprintf("cancel-replace %d", id)))
		return nil
	}
	btrfsResumeRemove = func(_ context.Context, _ string, id int, _ func(float64)) error {
		record("resume-remove %d", id)
		return nil
	}
	btrfsFindFilesystem = func(ctx context.Context, _ string) (btrfs.BtrfsFilesystem, error) {
		record("%s", callIn(ctx, "find"))
		return btrfs.BtrfsFilesystem{}, nil
	}

	uuid := apischema.UUIDRequest{UUID: testBtrfsUUID}
	device := apischema.BtrfsDeviceRequest{UUID: testBtrfsUUID, DeviceID: 3}
	replace := apischema.BtrfsDeviceReplaceRequest{UUID: testBtrfsUUID, DeviceID: 3, NewDevice: "/dev/sdx"}
	balance := apischema.BtrfsBalanceRequest{UUID: testBtrfsUUID}
	cases := []struct {
		name        string
		paused      bool
		positions   map[int]uint64
		follow      replaceFollow
		interrupted bool
		hooks       func(ctx context.Context) (jsontext.Value, error, error)
		checkpoint  string
		calls       []string
		wantCancel  bool
	}{
		{
			name: "balance paused", paused: true,
			hooks:      hooksOf(balance, suspendBtrfsBalance, resumeBtrfsBalance),
			checkpoint: `{"paused":true}`,
			calls:      []string{"pause-balance", "resume-balance paused=true", "balance-result"},
		},
		{
			name:       "balance not paused",
			hooks:      hooksOf(balance, suspendBtrfsBalance, resumeBtrfsBalance),
			checkpoint: `{"paused":false}`,
			calls:      []string{"pause-balance", "resume-balance paused=false", "balance-result"},
		},
		{
			name: "scrub with positions", positions: map[int]uint64{2: 7},
			hooks:      hooksOf(uuid, suspendBtrfsScrub, resumeBtrfsScrub),
			checkpoint: `{"remaining":{"2":7}}`,
			calls:      []string{"scrub-positions", "scrub-cancel", "scrub-from map[2:7] nil=false", "find"},
		},
		{
			name:  "scrub without positions rescans everything",
			hooks: hooksOf(uuid, suspendBtrfsScrub, resumeBtrfsScrub),
			calls: []string{"scrub-positions", "scrub-cancel", "scrub-from map[] nil=true", "find"},
		},
		{
			name:  "replace followed to its end",
			hooks: hooksOf(replace, suspendBtrfsReplace, resumeBtrfsReplace),
			calls: []string{"resume-replace 3", "find"},
		},
		{
			name:       "replace canceled by the user is canceled in the kernel",
			follow:     replaceFollow{userCancels: true, afterCancel: errors.New("the replace of device 3 did not survive the interruption; start it again")},
			hooks:      hooksOf(replace, suspendBtrfsReplace, resumeBtrfsReplace),
			calls:      []string{"resume-replace 3", "cancel-replace 3", "resume-replace 3"},
			wantCancel: true,
		},
		{
			// The kernel had installed the new disk before the cancel reached it.
			name:   "replace that finished as the user canceled is a success",
			follow: replaceFollow{userCancels: true},
			hooks:  hooksOf(replace, suspendBtrfsReplace, resumeBtrfsReplace),
			calls:  []string{"resume-replace 3", "cancel-replace 3", "resume-replace 3", "find"},
		},
		{
			name: "replace interrupted is left to the kernel", follow: replaceFollow{err: context.Canceled}, interrupted: true,
			hooks:      hooksOf(replace, suspendBtrfsReplace, resumeBtrfsReplace),
			calls:      []string{"resume-replace 3"},
			wantCancel: true,
		},
		{
			name:  "remove resumes the removal",
			hooks: hooksOf(device, suspendBtrfsRemove, resumeBtrfsRemove),
			calls: []string{"resume-remove 3", "find"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls, paused, positions, follow = nil, c.paused, c.positions, c.follow
			var checkpoint jsontext.Value
			var suspendErr, resumeErr error
			inTask(t, c.interrupted, func(ctx context.Context) { checkpoint, suspendErr, resumeErr = c.hooks(ctx) })
			if suspendErr != nil {
				t.Fatalf("suspend: %v", suspendErr)
			}
			if string(checkpoint) != c.checkpoint {
				t.Errorf("checkpoint = %q, want %q", checkpoint, c.checkpoint)
			}
			if !reflect.DeepEqual(calls, c.calls) {
				t.Errorf("btrfs calls = %v, want %v", calls, c.calls)
			}
			if c.wantCancel != errors.Is(resumeErr, context.Canceled) {
				t.Errorf("resume error = %v, want canceled=%v", resumeErr, c.wantCancel)
			}
			if !c.wantCancel && resumeErr != nil {
				t.Errorf("resume: %v", resumeErr)
			}
		})
	}
}

// A removal reports the share it has moved through its task, in the first run
// and in a resumed one.
func TestBtrfsRemoveReportsProgress(t *testing.T) {
	oldRemove, oldResume, oldFind := btrfsRemoveDevice, btrfsResumeRemove, btrfsFindFilesystem
	t.Cleanup(func() { btrfsRemoveDevice, btrfsResumeRemove, btrfsFindFilesystem = oldRemove, oldResume, oldFind })
	moved := func(_ context.Context, _ string, _ int, report func(float64)) error {
		report(40)
		return nil
	}
	btrfsRemoveDevice, btrfsResumeRemove = moved, moved
	btrfsFindFilesystem = func(context.Context, string) (btrfs.BtrfsFilesystem, error) { return btrfs.BtrfsFilesystem{}, nil }
	req := apischema.BtrfsDeviceRequest{UUID: testBtrfsUUID, DeviceID: 3}
	for name, run := range map[string]func(context.Context, *bridgeipc.Task) error{
		"first run": func(ctx context.Context, task *bridgeipc.Task) error {
			_, err := runBtrfsDeviceRemoveTask(ctx, task, req)
			return err
		},
		"resumed": func(ctx context.Context, _ *bridgeipc.Task) error {
			_, err := resumeBtrfsRemove(ctx, req, nil)
			return err
		},
	} {
		inTask(t, false, func(ctx context.Context) {
			task, _ := bridgeipc.TaskFromContext(ctx)
			if err := run(ctx, task); err != nil {
				t.Errorf("%s: %v", name, err)
				return
			}
			progress := task.Snapshot().Progress
			if progress == nil || progress.Percentage == nil || *progress.Percentage != 40 || progress.Phase != "remove" {
				t.Errorf("%s: progress = %+v, want 40%% of a remove", name, progress)
			}
		})
	}
}

// A resume that cannot finish fails the job with its cause, and neither
// reports success nor reads the filesystem.
func TestBtrfsResumeHookFailures(t *testing.T) {
	cause := errors.New("device busy")
	found := false
	oldResumeRemove, oldScrubFrom, oldFind := btrfsResumeRemove, btrfsScrubFrom, btrfsFindFilesystem
	t.Cleanup(func() {
		btrfsResumeRemove, btrfsScrubFrom, btrfsFindFilesystem = oldResumeRemove, oldScrubFrom, oldFind
	})
	btrfsResumeRemove = func(context.Context, string, int, func(float64)) error { return cause }
	// A device of the first part aborted, so the merged outcome is aborted.
	btrfsScrubFrom = func(context.Context, string, map[int]uint64, func(btrfs.BtrfsScrubStatus)) (btrfs.BtrfsScrubStatus, error) {
		return btrfs.BtrfsScrubStatus{Status: "aborted"}, nil
	}
	btrfsFindFilesystem = func(context.Context, string) (btrfs.BtrfsFilesystem, error) {
		found = true
		return btrfs.BtrfsFilesystem{}, nil
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	for _, c := range []struct {
		name   string
		resume func() error
		cause  error
		want   []string
	}{
		{
			name: "device removal",
			resume: func() error {
				_, err := resumeBtrfsRemove(ctx, apischema.BtrfsDeviceRequest{UUID: testBtrfsUUID, DeviceID: 3}, nil)
				return err
			},
			cause: cause,
			want:  []string{"resume removal of btrfs device 3 of " + testBtrfsUUID, "device busy"},
		},
		{
			name: "aborted scrub",
			resume: func() error {
				_, err := resumeBtrfsScrub(ctx, apischema.UUIDRequest{UUID: testBtrfsUUID}, jsontext.Value(`{"remaining":{"2":7}}`))
				return err
			},
			want: []string{"scrub btrfs filesystem " + testBtrfsUUID + " ended aborted"},
		},
	} {
		found = false
		logs.Reset()
		err := c.resume()
		if err == nil || c.cause != nil && !errors.Is(err, c.cause) || found || strings.Contains(logs.String(), "level=INFO") {
			t.Fatalf("%s: err = %v, find called = %v, logs = %q", c.name, err, found, logs.String())
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: err = %q, want it to contain %q", c.name, err, w)
			}
		}
	}
}

func TestScrubCancelMarksATimersResync(t *testing.T) {
	oldMark, oldWithdraw, oldCancel := btrfsMarkResyncCanceled, btrfsWithdrawResyncCancel, btrfsScrubCancel
	t.Cleanup(func() {
		btrfsMarkResyncCanceled, btrfsWithdrawResyncCancel, btrfsScrubCancel = oldMark, oldWithdraw, oldCancel
	})
	unit := btrfs.ResyncUnitName(testBtrfsUUID)
	mark, cancel, withdraw := "mark:"+unit, "cancel:"+testBtrfsUUID, "withdraw:"+unit
	cancelFailure := errors.New("no scrub is running")

	for _, c := range []struct {
		name                            string
		markErr, cancelErr, withdrawErr error
		want                            []string
	}{
		{"cancel leaves the mark", nil, nil, nil, []string{mark, cancel}},
		{"a failed mark does not stop the cancel", errors.New("store unavailable"), nil, nil, []string{mark, cancel}},
		{"a failed cancel withdraws the mark", nil, cancelFailure, nil, []string{mark, cancel, withdraw}},
		{"a failed withdraw keeps the cancel's error", nil, cancelFailure, errors.New("store unavailable"), []string{mark, cancel, withdraw}},
	} {
		var calls []string
		btrfsMarkResyncCanceled = func(_ context.Context, unit string) error {
			calls = append(calls, "mark:"+unit)
			return c.markErr
		}
		btrfsWithdrawResyncCancel = func(_ context.Context, unit string) error {
			calls = append(calls, "withdraw:"+unit)
			return c.withdrawErr
		}
		btrfsScrubCancel = func(_ context.Context, uuid string) error {
			calls = append(calls, "cancel:"+uuid)
			return c.cancelErr
		}

		err := CancelBtrfsScrub(t.Context(), testBtrfsUUID)
		if c.cancelErr == nil && err != nil || c.cancelErr != nil && !errors.Is(err, c.cancelErr) {
			t.Errorf("%s: CancelBtrfsScrub = %v, want %v", c.name, err, c.cancelErr)
		}
		if !slices.Equal(calls, c.want) {
			t.Errorf("%s: calls = %v, want %v", c.name, calls, c.want)
		}
	}
}
