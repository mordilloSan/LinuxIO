package btrfs

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

// captureLogs sends the package's log lines to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &logs
}

// logLines returns the lines logged with message msg.
func logLines(logs *bytes.Buffer, msg string) []string {
	var lines []string
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, `msg="`+msg+`"`) {
			lines = append(lines, line)
		}
	}
	return lines
}

// wantLogLine checks that msg was logged once, with every attribute in attrs.
func wantLogLine(t *testing.T, logs *bytes.Buffer, msg string, attrs ...string) {
	t.Helper()
	lines := logLines(logs, msg)
	if len(lines) != 1 {
		t.Fatalf("lines %q = %q, want one; logs:\n%s", msg, lines, logs)
	}
	for _, attr := range attrs {
		if !strings.Contains(lines[0]+" ", " "+attr+" ") {
			t.Fatalf("line %q lacks %s", lines[0], attr)
		}
	}
}

// returnedEpisode stores a pool that ran without disk 2, which is back.
func returnedEpisode(t *testing.T) {
	t.Helper()
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}, Missing: []missingDisk{{ID: 2, UUID: devUUID(2)}}}
	}); err != nil {
		t.Fatal(err)
	}
}

// wantNoReset checks that no device's error counters were reset.
func wantNoReset(t *testing.T, k *fakeKernel) {
	t.Helper()
	if i := slices.IndexFunc(k.Calls(), func(c string) bool { return strings.HasPrefix(c, "RESET_DEV_STATS") }); i >= 0 {
		t.Fatalf("calls = %v, want no counters reset", k.Calls())
	}
}

// resyncedMessage returns the message of the whole again notice.
func resyncedMessage(t *testing.T) string {
	t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range doc.Alerts {
		if a.Key == "resynced-"+dataUUID {
			return a.Message
		}
	}
	t.Fatalf("no whole again notice in %+v", doc.Alerts)
	return ""
}

func TestMarkDegradedRecordsTheMissingDisks(t *testing.T) {
	var s poolState
	s.markDegraded([]BtrfsDevice{{ID: 1}, {ID: 2, UUID: devUUID(2), Missing: true, Errors: BtrfsDeviceErrors{Corruption: 1}}, {ID: 3}})
	want := []missingDisk{{ID: 2, UUID: devUUID(2), Errors: BtrfsDeviceErrors{Corruption: 1}}}
	if !slices.Equal(s.Present, []uint64{1, 3}) || !slices.Equal(withoutAt(s.Missing), want) {
		t.Fatalf("state = %+v, want devid 2 missing with its UUID and counters, and 1 and 3 present", s)
	}
	// The hot spare's delay counts from when the disk was recorded missing.
	if at := s.Missing[0].At; at < time.Now().Add(-time.Minute).Unix() || at > time.Now().Unix() {
		t.Fatalf("missing at %d, want now", at)
	}
}

// The scrub counts every stale copy it rewrites on the returned disk as a
// corruption or generation error of that disk (scrub.c
// scrub_stripe_report_errors), repaired or not. A resync that made the pool
// whole resets those counters on the disk that returned and only there, logs
// what they were, and the notice names them.
func TestResyncResetsTheReturnedDisksCounters(t *testing.T) {
	k := uuidKernel()
	k.scrub = func(_, _ uint64) (scrubProgress, error) { return scrubProgress{CorrectedErrors: 259970}, nil }
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 259840, Generation: 130})
	// Errors disk 1 reported while it stayed present are not the resync's.
	k.setCounters(1, BtrfsDeviceErrors{Corruption: 3})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	if got := k.counters(2); got != (BtrfsDeviceErrors{}) {
		t.Fatalf("returned disk's counters = %+v, want them reset", got)
	}
	if got := k.counters(1); got != (BtrfsDeviceErrors{Corruption: 3}) {
		t.Fatalf("present disk's counters = %+v, want them kept", got)
	}
	wantLogLine(t, logs, "btrfs device error counters reset after resync",
		"uuid="+dataUUID, "devid=2", "device=/dev/sdb1", "write=0", "read=0", "flush=0", "corruption=259840", "generation=130")
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 (259840 corruption and 130 generation errors)") {
		t.Fatalf("whole again notice = %q, want the reset counters named", msg)
	}
}

// The reset is what clears the health alert, at once: the resync refreshes
// the pool's alerts when it ends.
func TestResyncResolvesTheHealthAlertOfItsCorrections(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 7, Generation: 1})
	returnedEpisode(t)
	if _, err := CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := alertKeys(t)["health-"+dataUUID]; got != alerts.SeverityError {
		t.Fatalf("health alert before the resync = %q, want it raised", got)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if got := alertKeys(t); got["health-"+dataUUID] != "" || got["resynced-"+dataUUID] != alerts.SeverityInfo {
		t.Fatalf("alerts after the resync = %v, want the health alert resolved and the whole again notice", got)
	}
}

// A read, write or flush error is a fault of the disk, not a stale copy. The
// kernel resets all five counters of a device or none, so the resync keeps
// them all, the health alert stays and the log and the notice say why.
func TestResyncKeepsTheCountersOfADiskWithIOErrors(t *testing.T) {
	for name, io := range map[string]BtrfsDeviceErrors{
		"read":  {Read: 1},
		"write": {Write: 2},
		"flush": {Flush: 3},
	} {
		t.Run(name, func(t *testing.T) {
			k := uuidKernel()
			stubKernel(t, k)
			logs := captureLogs(t)
			want := io
			want.Corruption, want.Generation = 10, 2
			k.setCounters(2, want)
			returnedEpisode(t)

			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			wantWhole(t)
			wantNoReset(t, k)
			if got := k.counters(2); got != want {
				t.Fatalf("counters = %+v, want %+v kept", got, want)
			}
			wantLogLine(t, logs, "btrfs device error counters kept after resync for its i/o errors",
				"uuid="+dataUUID, "devid=2", "device=/dev/sdb1", "corruption=10", "generation=2")
			if got := alertKeys(t)["health-"+dataUUID]; got != alerts.SeverityError {
				t.Fatalf("health alert = %q, want it kept up", got)
			}
			if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 kept its error counters") {
				t.Fatalf("whole again notice = %q, want the kept counters explained", msg)
			}
		})
	}
}

// Only a resync that found the pool whole resets anything: not one whose
// scrub was canceled, whose writes failed during its scrub, that still has
// chunks to convert, or during which a disk dropped out again.
func TestResyncResetsNoCountersUnlessWhole(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T) *fakeKernel{
		"a canceled scrub": func(t *testing.T) *fakeKernel {
			k := uuidKernel()
			k.scrub = func(_, _ uint64) (scrubProgress, error) { return scrubProgress{}, unix.ECANCELED }
			stubKernel(t, k)
			returnedEpisode(t)
			return k
		},
		"writes that failed during its scrub": func(t *testing.T) *fakeKernel {
			k := uuidKernel()
			// On disk 1, so that only the failed attempt keeps disk 2's.
			k.scrub = func(id, _ uint64) (scrubProgress, error) {
				if id == 1 {
					k.setErrors(1, 4, 0)
				}
				return scrubProgress{}, nil
			}
			stubKernel(t, k)
			returnedEpisode(t)
			return k
		},
		"chunks left to convert": func(t *testing.T) *fakeKernel {
			k := uuidKernel()
			k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
			k.balance = func(*balanceArgs) error { return nil } // converts nothing
			stubKernel(t, k)
			returnedEpisode(t)
			return k
		},
		"a disk that dropped out again": func(t *testing.T) *fakeKernel {
			k := uuidKernel()
			k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
			k.balance = func(*balanceArgs) error {
				k.devices[1].path = ""
				return nil
			}
			stubKernel(t, k)
			returnedEpisode(t)
			return k
		},
	} {
		t.Run(name, func(t *testing.T) {
			k := setup(t)
			k.setCounters(2, BtrfsDeviceErrors{Corruption: 5, Generation: 1})

			if err := Resync(t.Context(), dataUUID, nil); err == nil {
				t.Fatal("Resync succeeded, want it to fail")
			}
			wantNoReset(t, k)
			if states, _ := loadPoolStates(); !states[dataUUID].Degraded {
				t.Fatalf("state = %+v, want the episode left", states[dataUUID])
			}
		})
	}
}

// A scrub that found copies it could not repair leaves the pool's data
// damaged: the resync ends, the scrub alert reports them, and the counters
// stay as evidence.
func TestResyncKeepsTheCountersAfterUncorrectableErrors(t *testing.T) {
	k := uuidKernel()
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			return scrubProgress{CorrectedErrors: 5, UncorrectableErrors: 1}, nil
		}
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 6})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync since its scrub found uncorrectable errors",
		"uuid="+dataUUID, "uncorrectable=1")
	if got := alertKeys(t); got["health-"+dataUUID] != alerts.SeverityError || got["scrub-"+dataUUID] != alerts.SeverityError {
		t.Fatalf("alerts = %v, want the health and scrub alerts up", got)
	}
}

// A reset the kernel refuses leaves the counters and their alert, and the
// pool is whole all the same.
func TestResyncStaysWholeWhenTheResetFails(t *testing.T) {
	k := uuidKernel()
	k.statsResetFail = unix.EPERM
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatalf("Resync = %v, want the pool whole though its counters were not reset", err)
	}
	wantWhole(t)
	if got := k.counters(2); got != (BtrfsDeviceErrors{Corruption: 5}) {
		t.Fatalf("counters = %+v, want them as they were", got)
	}
	wantLogLine(t, logs, "failed to reset btrfs device error counters", "uuid="+dataUUID, "devid=2", "device=/dev/sdb1")
	// The notice beside the health alert says the counters were kept.
	if got := alertKeys(t); got["health-"+dataUUID] != alerts.SeverityError || got["resynced-"+dataUUID] != alerts.SeverityWarning {
		t.Fatalf("alerts = %v, want the health alert up beside a whole again warning", got)
	}
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 kept its error counters (5 corruption errors): they could not be reset.") {
		t.Fatalf("whole again notice = %q, want the failed reset named", msg)
	}
}

// The reset comes right before the state ends the episode. A crash between
// the two leaves the episode scrubbed, and the next resync resets again:
// nothing when the counters are still zero, and again when a power loss
// before the kernel's commit brought them back.
func TestResyncResetIsIdempotent(t *testing.T) {
	for name, back := range map[string]BtrfsDeviceErrors{
		"the process crashed":              {},
		"the power failed before a commit": {Corruption: 5},
	} {
		t.Run(name, func(t *testing.T) {
			k := uuidKernel()
			stubKernel(t, k)
			k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
			returnedEpisode(t)
			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			rec, _ := loadScrubRecord(dataUUID)
			if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
				s[dataUUID] = poolState{Generation: 9, Degraded: true, Scrubbed: true, ResyncRun: rec.Run, Present: []uint64{1}, Missing: []missingDisk{{ID: 2, UUID: devUUID(2)}}}
			}); err != nil {
				t.Fatal(err)
			}
			k.setCounters(2, back)
			before := len(k.Calls())
			logs := captureLogs(t)

			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			wantWhole(t)
			if got := k.counters(2); got != (BtrfsDeviceErrors{}) {
				t.Fatalf("counters = %+v, want them zero", got)
			}
			again := slices.Contains(k.Calls()[before:], "RESET_DEV_STATS 2")
			if logged := len(logLines(logs, "btrfs device error counters reset after resync")) == 1; again != (back != BtrfsDeviceErrors{}) || logged != again {
				t.Fatalf("reset again %v, logged %v; want both only for counters that came back", again, logged)
			}
		})
	}
}

// An episode that names no missing disk cannot tell the resync which disk
// returned, so it resets nothing.
func TestResyncOfAnEpisodeWithoutMissingDisksResetsNothing(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
}

// Errors a disk counted before it went missing are its own, and the stale
// copies explain none of them: a disk that had any keeps all its counters.
func TestResyncKeepsTheCountersOfADiskWithErrorsFromBefore(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 12, Generation: 1})
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1},
			Missing: []missingDisk{{ID: 2, UUID: devUUID(2), Errors: BtrfsDeviceErrors{Corruption: 2}}}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync for errors counted when it went missing",
		"uuid="+dataUUID, "devid=2", "device=/dev/sdb1", "corruption=12", "generation=1")
	if got := alertKeys(t)["health-"+dataUUID]; got != alerts.SeverityError {
		t.Fatalf("health alert = %q, want it kept up", got)
	}
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 kept its error counters") {
		t.Fatalf("whole again notice = %q, want the kept counters explained", msg)
	}
}

// A devid freed by removing the missing disk goes to the next disk added
// when it was the highest (volumes.c find_next_devid). That disk missed
// nothing, so what it counts is its own: the resync resets only the disk
// that went missing, known by its device UUID too.
func TestResyncResetsNothingOnAnotherDiskUnderTheDevid(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}, Missing: []missingDisk{{ID: 2, UUID: devUUID(7)}}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
}

// A failed read, write or flush counted between the check and the reset is
// reset with the rest, since the kernel zeroes all five at once: the line
// is a warning with the counts the reset returned, and the notice names it
// apart from the stale copies.
func TestResyncNamesAnIOErrorCountedAsTheCountersWereReset(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	check := ioctlFD
	ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
		if req == iocGetDevStats && (*devStatsArgs)(arg).Flags&devStatsReset != 0 {
			k.setCounters(2, BtrfsDeviceErrors{Read: 1, Corruption: 5})
		}
		return check(fd, req, arg)
	}
	t.Cleanup(func() { ioctlFD = check })

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantLogLine(t, logs, "btrfs device error counters reset after resync with i/o errors", "devid=2", "read=1", "corruption=5")
	if msg := resyncedMessage(t); !strings.Contains(msg, "A read, write or flush failed on /dev/sdb1 (1 read and 5 corruption errors) in the instant its counters were reset") {
		t.Fatalf("whole again notice = %q, want the late i/o error named", msg)
	}
	if got := alertKeys(t)["resynced-"+dataUUID]; got != alerts.SeverityWarning {
		t.Fatalf("whole again notice = %q, want a warning", got)
	}
}

// wholeEpisode stores a pool that ran without disk 2, whose counters were
// baseline when the health run last saw the pool whole.
func wholeEpisode(t *testing.T, baseline BtrfsDeviceErrors) {
	t.Helper()
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1},
			Missing: []missingDisk{{ID: 2, UUID: devUUID(2), Errors: baseline, Whole: true}}}
	}); err != nil {
		t.Fatal(err)
	}
}

// labelLockHeld reports whether another holder has the label lock.
func labelLockHeld(t *testing.T) bool {
	t.Helper()
	return lockHeld(t, filepath.Join(filepath.Dir(PoolStatePath), "btrfs-label.lock"))
}

// lockHeld reports whether another holder has the file lock at path.
func lockHeld(t *testing.T, path string) bool {
	t.Helper()
	release, err := filelock.AcquireExclusive(t.Context(), path, filelock.WithTimeout(20*time.Millisecond))
	if err == nil {
		_ = release()
	}
	return errors.Is(err, filelock.ErrTimeout)
}

// The kernel writes a reset at its next commit, and 7.0 does not commit at
// unmount without a running transaction, so the resync commits one: it sets
// the label to itself, which starts a transaction and commits it
// (ioctl.c btrfs_ioctl_set_fslabel), under the label lock so a rename
// through LinuxIO cannot be undone.
func TestResyncCommitsTheResetBySettingTheLabelToItself(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	locked := false
	k.onCall = func(call string) {
		if strings.HasPrefix(call, "SET_FSLABEL") {
			locked = labelLockHeld(t)
		}
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	calls := k.Calls()
	reset, set := slices.Index(calls, "RESET_DEV_STATS 2"), slices.Index(calls, "SET_FSLABEL data")
	if reset < 0 || set < reset || k.label != "data" {
		t.Fatalf("calls = %v, label %q; want the label set to itself after the reset", calls, k.label)
	}
	if !locked {
		t.Fatal("the label was set without the label lock")
	}
}

// A resync of an episode that recorded no missing disk, as an older
// LinuxIO's, has no reset to commit.
func TestResyncWithNothingToResetLeavesTheLabel(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(k.Calls(), func(c string) bool { return strings.HasPrefix(c, "SET_FSLABEL") }) {
		t.Fatalf("calls = %v, want no label set", k.Calls())
	}
}

// A commit that fails leaves the reset to the kernel's next commit: a
// warning, and the pool is whole.
func TestResyncStaysWholeWhenTheCommitFails(t *testing.T) {
	k := uuidKernel()
	k.fail = map[uintptr]error{iocSetFSLabel: unix.EROFS}
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	if got := k.counters(2); got != (BtrfsDeviceErrors{}) {
		t.Fatalf("counters = %+v, want them reset", got)
	}
	wantLogLine(t, logs, "failed to commit the reset of btrfs device error counters", "uuid="+dataUUID)
}

// A rename through LinuxIO takes the same lock as the resync's commit.
func TestSetLabelHoldsTheLabelLock(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	locked := false
	set := fsIoctl
	fsIoctl = func(mnt string, req uintptr, arg unsafe.Pointer) error {
		if req == iocSetFSLabel {
			locked = labelLockHeld(t)
		}
		return set(mnt, req, arg)
	}
	t.Cleanup(func() { fsIoctl = set })

	if err := SetLabel(t.Context(), dataUUID, "tank"); err != nil || !locked {
		t.Fatalf("SetLabel = %v, under the label lock %v; want it renamed under the lock", err, locked)
	}
}

// The health run saves each device's counters while it sees the pool whole
// outside an episode, and leaves them during one: they are the baseline of
// a disk that goes missing, from before its dropout raised them.
func TestHealthRunSavesTheCountersOfAWholePool(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(1, BtrfsDeviceErrors{Corruption: 1})
	if _, err := CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []deviceCounters{{ID: 1, UUID: devUUID(1), Errors: BtrfsDeviceErrors{Corruption: 1}}, {ID: 2, UUID: devUUID(2)}}
	if states, _ := loadPoolStates(); !slices.Equal(states[dataUUID].Counters, want) {
		t.Fatalf("saved counters = %+v, want %+v", states[dataUUID].Counters, want)
	}

	// In an episode, with every disk back, the saved counters stay.
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		state := s[dataUUID]
		state.Degraded = true
		s[dataUUID] = state
	}); err != nil {
		t.Fatal(err)
	}
	k.setCounters(2, BtrfsDeviceErrors{Write: 9})
	if _, err := CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if states, _ := loadPoolStates(); !slices.Equal(states[dataUUID].Counters, want) {
		t.Fatalf("saved counters during an episode = %+v, want %+v", states[dataUUID].Counters, want)
	}
}

// A disk that goes missing takes its baseline from the pool last seen
// whole, not from what it reads once gone, which holds the failed writes of
// its dropout; without a saved baseline for that devid and UUID it keeps
// what it reads.
func TestMarkDegradedTakesTheBaselineFromThePoolWhole(t *testing.T) {
	gone := BtrfsDevice{ID: 2, UUID: devUUID(2), Missing: true, Errors: BtrfsDeviceErrors{Write: 40, Flush: 2}}
	s := poolState{Counters: []deviceCounters{{ID: 1, UUID: devUUID(1)}, {ID: 2, UUID: devUUID(2)}}}
	s.markDegraded([]BtrfsDevice{{ID: 1, UUID: devUUID(1)}, gone})
	if want := []missingDisk{{ID: 2, UUID: devUUID(2), Whole: true}}; !slices.Equal(withoutAt(s.Missing), want) {
		t.Fatalf("missing = %+v, want %+v", s.Missing, want)
	}
	s = poolState{Counters: []deviceCounters{{ID: 2, UUID: devUUID(7)}}}
	s.markDegraded([]BtrfsDevice{{ID: 1, UUID: devUUID(1)}, gone})
	if want := []missingDisk{{ID: 2, UUID: devUUID(2), Errors: gone.Errors}}; !slices.Equal(withoutAt(s.Missing), want) {
		t.Fatalf("missing without a baseline = %+v, want %+v", s.Missing, want)
	}
}

// A disk that dropped out of the mounted pool counted the writes and
// flushes that failed as it went. LinuxIO never resets a disk that counts a
// failed read, write or flush, whatever the cause: it keeps them all, the
// health alert stays, and the notice says why.
func TestResyncKeepsADiskThatDroppedOutWhileMounted(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Write: 30, Flush: 2, Corruption: 100})
	wholeEpisode(t, BtrfsDeviceErrors{})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync for its i/o errors", "devid=2", "write=30", "flush=2", "corruption=100")
	if got := alertKeys(t); got["health-"+dataUUID] != alerts.SeverityError || got["resynced-"+dataUUID] != alerts.SeverityWarning {
		t.Fatalf("alerts = %v, want the health alert up beside a whole again warning", got)
	}
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 kept its error counters (30 write, 2 flush and 100 corruption errors): it counts a failed read, write or flush.") {
		t.Fatalf("whole again notice = %q, want the kept counters explained", msg)
	}
}

// A disk whose counters were not zero when the pool was last seen whole
// counts errors of its own: it keeps them all.
func TestResyncKeepsADiskWhoseBaselineHadErrors(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 100})
	wholeEpisode(t, BtrfsDeviceErrors{Generation: 1})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync for errors counted before the episode", "devid=2", "corruption=100")
}

// A read that fails on the returned disk during the resync is a fault of
// the disk: it keeps its counters. (A failed write or flush fails the
// attempt itself, staleFor.)
func TestResyncKeepsADiskThatFailedReadsDuringTheResync(t *testing.T) {
	k := uuidKernel()
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			k.setCounters(2, BtrfsDeviceErrors{Read: 1, Corruption: 100})
		}
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 100})
	wholeEpisode(t, BtrfsDeviceErrors{})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync for its i/o errors", "devid=2", "read=1", "corruption=100")
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 kept its error counters") {
		t.Fatalf("whole again notice = %q, want the kept counters explained", msg)
	}
}

// A scrub that found errors it could not correct leaves data damaged: the
// notice does not claim the pool whole without saying so and how many, and
// the scrub alert stays up at error severity until a scrub finds none.
func TestResyncNoticeNamesUncorrectableErrors(t *testing.T) {
	k := uuidKernel()
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 {
			return scrubProgress{CorrectedErrors: 40, UncorrectableErrors: 3}, nil
		}
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 43})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	var notice alerts.Alert
	for _, a := range doc.Alerts {
		if a.Key == "resynced-"+dataUUID {
			notice = a
		}
	}
	if notice.Severity != alerts.SeverityWarning || !strings.Contains(notice.Title, "3 errors could not be corrected") ||
		!strings.Contains(notice.Message, "found 3 errors it could not correct") {
		t.Fatalf("notice = %+v, want it to say 3 errors could not be corrected", notice)
	}
	if got := alertKeys(t)["scrub-"+dataUUID]; got != alerts.SeverityError {
		t.Fatalf("scrub alert = %q, want it up at error severity", got)
	}
	wantNoReset(t, k)
}

// A failed attempt leaves the scrub stale, and the next attempt scans the
// disk again from a new start whose counters already hold the failures.
// The write errors of the earlier attempt are kept all the same, since a
// disk that counts any failed read, write or flush is never reset.
func TestResyncKeepsIOErrorsOfAnEarlierFailedAttempt(t *testing.T) {
	k := uuidKernel()
	attempt := 0
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 && attempt == 1 {
			k.setCounters(2, BtrfsDeviceErrors{Write: 34, Corruption: 100})
		}
		return scrubProgress{}, nil
	}
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Write: 30, Corruption: 100})
	wholeEpisode(t, BtrfsDeviceErrors{})

	attempt = 1
	if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), "failed writes") {
		t.Fatalf("first attempt = %v, want it failed by the disk's write errors", err)
	}
	attempt = 2
	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
	if got := k.counters(2); got != (BtrfsDeviceErrors{Write: 34, Corruption: 100}) {
		t.Fatalf("counters = %+v, want them kept", got)
	}
}

// A disk that goes missing while the episode is open gets no saved
// baseline: it may have counted errors of its own since the pool was last
// seen whole. The disks already in the episode keep their entries.
func TestMarkDegradedKeepsTheEpisodesMissingDisks(t *testing.T) {
	var s poolState
	s.Counters = []deviceCounters{{ID: 1, UUID: devUUID(1)}, {ID: 2, UUID: devUUID(2)}}
	s.markDegraded([]BtrfsDevice{{ID: 1, UUID: devUUID(1), Missing: true}, {ID: 2, UUID: devUUID(2)}})
	if s.Since == 0 {
		t.Fatalf("state = %+v, want the episode's start", s)
	}
	since := s.Since
	// Disk 1 is back, not yet resynced; disk 2 counted read errors while
	// present, then went missing.
	s.markDegraded([]BtrfsDevice{{ID: 1, UUID: devUUID(1)}, {ID: 2, UUID: devUUID(2), Missing: true, Errors: BtrfsDeviceErrors{Read: 3}}})
	want := []missingDisk{{ID: 1, UUID: devUUID(1), Whole: true}, {ID: 2, UUID: devUUID(2), Errors: BtrfsDeviceErrors{Read: 3}}}
	if !slices.Equal(withoutAt(s.Missing), want) || s.Since != since {
		t.Fatalf("missing = %+v since %d, want %+v since %d", s.Missing, s.Since, want, since)
	}
}

// A replace puts a new disk under the missing disk's devid and
// UUID (dev-replace.c btrfs_dev_replace_finishing); it holds no stale
// copies, so what it counts is its own. LinuxIO's replace forgets the disk
// it replaced from the episode.
func TestReplaceForgetsTheMissingDiskItReplaced(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	wholeEpisode(t, BtrfsDeviceErrors{})

	if err := Replace(t.Context(), dataUUID, 2, "/dev/sdd", func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if states, _ := loadPoolStates(); len(states[dataUUID].Missing) != 0 {
		t.Fatalf("missing after the replace = %+v, want the replaced disk forgotten", states[dataUUID].Missing)
	}
}

// A replace LinuxIO followed after an interruption.
func TestResumeReplaceForgetsTheMissingDiskItReplaced(t *testing.T) {
	k := uuidKernel()
	since := time.Now().Add(-time.Minute)
	k.replaceStatus = func() (uint64, uint64) { return replaceStateFinished, 1000 }
	k.replaceStarted = uint64(since.Unix() + 1)
	stubKernel(t, k)
	wholeEpisode(t, BtrfsDeviceErrors{})

	if err := ResumeReplace(t.Context(), dataUUID, 2, since, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if states, _ := loadPoolStates(); len(states[dataUUID].Missing) != 0 {
		t.Fatalf("missing after the replace = %+v, want the replaced disk forgotten", states[dataUUID].Missing)
	}
}

// A replace done by hand: the kernel keeps when the last replace
// stopped, and a pool whose last replace finished after the episode began
// resets nothing.
func TestResyncResetsNothingAfterAReplaceDuringTheEpisode(t *testing.T) {
	k := uuidKernel()
	now := time.Now().Unix()
	k.replaceStatus = func() (uint64, uint64) { return replaceStateFinished, 1000 }
	k.replaceStarted, k.replaceStopped = uint64(now-50), uint64(now-10)
	stubKernel(t, k)
	logs := captureLogs(t)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}, Since: now - 100,
			Missing: []missingDisk{{ID: 2, UUID: devUUID(2), Whole: true}}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	wantNoReset(t, k)
	wantLogLine(t, logs, "btrfs device error counters kept after resync since a replace finished during the episode", "uuid="+dataUUID)
}

// The reset and its commit run outside the pool state's lock, so a
// filesystem that blocks them holds no health run up.
func TestResyncResetsOutsideThePoolStateLock(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	var underLock []string
	k.onCall = func(call string) {
		if lockHeld(t, PoolStatePath+".lock") {
			underLock = append(underLock, call)
		}
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(k.Calls(), "RESET_DEV_STATS 2") || len(underLock) != 0 {
		t.Fatalf("calls = %v, under the pool state's lock %v; want the reset and commit outside it", k.Calls(), underLock)
	}
}

// A health run that cannot read the pool states neither starts nor
// resolves a resync: it leaves the resync alert as it was.
func TestCheckHealthLeavesTheResyncAlertWhenTheStateIsUnreadable(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	returnedEpisode(t)
	if _, err := CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := alertKeys(t)["resync-"+dataUUID]; got != alerts.SeverityWarning {
		t.Fatalf("resync alert = %q, want it raised", got)
	}
	if err := os.WriteFile(PoolStatePath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions, err := CheckHealth(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := alertKeys(t)["resync-"+dataUUID]; got != alerts.SeverityWarning || len(actions.Resync) != 0 {
		t.Fatalf("resync alert = %q, resyncs %v; want the alert kept and no resync started", got, actions.Resync)
	}
}

// The commit is repeated: after a crash between the reset and the
// commit, or a commit that failed, the next attempt finds the counters zero
// and resets nothing, yet commits, so the kernel writes the reset.
func TestResyncCommitsOnARetryAfterTheReset(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	rec, _ := loadScrubRecord(dataUUID)
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Scrubbed: true, ResyncRun: rec.Run, Present: []uint64{1},
			Missing: []missingDisk{{ID: 2, UUID: devUUID(2)}}}
	}); err != nil {
		t.Fatal(err)
	}
	before := len(k.Calls())

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(k.Calls()[before:], "SET_FSLABEL data") {
		t.Fatalf("calls of the retry = %v, want the commit again", k.Calls()[before:])
	}
}

// The counters as the episode ends become the baseline of a disk
// that drops out before the next health run: one kept stays non-zero.
func TestResyncSavesTheCountersAsTheEpisodeEnds(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(1, BtrfsDeviceErrors{Corruption: 3})
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	want := []deviceCounters{{ID: 1, UUID: devUUID(1), Errors: BtrfsDeviceErrors{Corruption: 3}}, {ID: 2, UUID: devUUID(2)}}
	if states, _ := loadPoolStates(); !slices.Equal(states[dataUUID].Counters, want) {
		t.Fatalf("saved counters = %+v, want %+v", states[dataUUID].Counters, want)
	}
}

// The reset opens the pool once and issues every ioctl of it, the
// device checks, the reset and the label, on that one descriptor, after
// checking that it holds the pool.
func TestResyncResetsThroughOneDescriptor(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	fds := map[int]bool{}
	byPath := false
	route, byFD := fsIoctl, ioctlFD
	fsIoctl = func(mnt string, req uintptr, arg unsafe.Pointer) error {
		if req == iocSetFSLabel || req == iocGetDevStats && (*devStatsArgs)(arg).Flags&devStatsReset != 0 {
			byPath = true
		}
		return route(mnt, req, arg)
	}
	ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
		fds[fd] = true
		return byFD(fd, req, arg)
	}
	t.Cleanup(func() { fsIoctl, ioctlFD = route, byFD })

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(k.Calls(), "RESET_DEV_STATS 2") || byPath || len(fds) != 1 {
		t.Fatalf("calls %v, by path %v, descriptors %v; want the reset and label on one descriptor", k.Calls(), byPath, fds)
	}
}

// A disk the kernel has marked missing (sysfs devinfo, since 7.1
// when its block device went away) is not reset, though DEV_INFO still
// names it.
func TestResyncResetsNoDiskTheKernelMarkedMissing(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	route := ioctlFD
	ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
		if req == iocDevInfo {
			dir := filepath.Join(sysFSBtrfs, dataUUID, "devinfo", "2")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "missing"), []byte("1\n"), 0o644); err != nil {
				return err
			}
		}
		return route(fd, req, arg)
	}
	t.Cleanup(func() { ioctlFD = route })

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoReset(t, k)
}

// The gate of the reset itself, in a resync that ends
// whole: the counters are kept and the notice says why, at warning
// severity, when the scrub record is no longer the run's, the pool cannot
// be opened, the mount holds another filesystem, or a replace runs.
func TestResyncKeepsTheCountersWhenItsGateRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(t *testing.T, k *fakeKernel)
		log   string
		says  string
	}{
		"another scrub's record": {
			setup: func(t *testing.T, k *fakeKernel) {
				route := ioctlFD
				ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
					if req == iocFSInfo {
						saveScrubRecord(t.Context(), dataUUID, scrubRecord{Status: "finished", Run: "job-1"})
					}
					return route(fd, req, arg)
				}
				t.Cleanup(func() { ioctlFD = route })
			},
			log: "btrfs device error counters kept after resync since its scrub is no longer on record", says: "another scrub replaced the resync's record",
		},
		"a pool it cannot open": {
			setup: func(t *testing.T, k *fakeKernel) {
				open := openMount
				openMount = func(string) (int, error) { return -1, unix.EACCES }
				t.Cleanup(func() { openMount = open })
			},
			log: "failed to reset btrfs device error counters", says: "the pool could not be opened",
		},
		"another filesystem at the mount": {
			setup: func(t *testing.T, k *fakeKernel) {
				route := ioctlFD
				ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
					err := route(fd, req, arg)
					if req == iocFSInfo {
						(*fsInfoArgs)(arg).FSID = uuidBytes(devUUID(9))
					}
					return err
				}
				t.Cleanup(func() { ioctlFD = route })
			},
			log: "failed to reset btrfs device error counters", says: "the pool could not be opened",
		},
		"a replace running": {
			setup: func(t *testing.T, k *fakeKernel) {
				k.replaceStatus = func() (uint64, uint64) { return replaceStateSuspended, 400 }
			},
			log: "btrfs device error counters kept after resync since a replace runs", says: "a replace runs on the pool",
		},
	} {
		t.Run(name, func(t *testing.T) {
			k := uuidKernel()
			stubKernel(t, k)
			logs := captureLogs(t)
			k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
			returnedEpisode(t)
			c.setup(t, k)

			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			wantWhole(t)
			wantNoReset(t, k)
			if len(logLines(logs, c.log)) == 0 {
				t.Fatalf("no line %q in logs:\n%s", c.log, logs)
			}
			if got := alertKeys(t)["resynced-"+dataUUID]; got != alerts.SeverityWarning {
				t.Fatalf("whole again notice = %q, want a warning", got)
			}
			if msg := resyncedMessage(t); !strings.Contains(msg, "error counters were kept: "+c.says+".") {
				t.Fatalf("whole again notice = %q, want it to say %q", msg, c.says)
			}
		})
	}
}

// A disk that counts any read, write or flush error keeps all its counters,
// whatever else it counts.
func TestResyncKeepsADiskWithOnlyReadOrFlushErrors(t *testing.T) {
	for name, errs := range map[string]BtrfsDeviceErrors{
		"read":  {Read: 1},
		"flush": {Flush: 1},
	} {
		t.Run(name, func(t *testing.T) {
			k := uuidKernel()
			stubKernel(t, k)
			k.setCounters(2, errs)
			wholeEpisode(t, BtrfsDeviceErrors{})

			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			wantNoReset(t, k)
			if got := k.counters(2); got != errs {
				t.Fatalf("counters = %+v, want %+v kept", got, errs)
			}
		})
	}
}

// An episode that recorded no missing disk resets nothing; while a device
// still counts errors the notice is a warning that says why, not a plain
// whole again beside the health alert.
func TestResyncNoticeOfAnEpisodeWithNoRecordedDisk(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Present: []uint64{1}}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoReset(t, k)
	if got := alertKeys(t); got["health-"+dataUUID] != alerts.SeverityError || got["resynced-"+dataUUID] != alerts.SeverityWarning {
		t.Fatalf("alerts = %v, want the health alert up beside a whole again warning", got)
	}
	if msg := resyncedMessage(t); !strings.Contains(msg, "/dev/sdb1 still counts errors (5 corruption errors): LinuxIO resets only the counters of a disk recorded missing in the episode") {
		t.Fatalf("whole again notice = %q, want it to say why the counters stay", msg)
	}
}

// A disk whose counters could not be read is named by its devid
// when the kernel gave no path, with no counts it did not read.
func TestResyncNoticeOfADiskItCouldNotRead(t *testing.T) {
	k := uuidKernel()
	stubKernel(t, k)
	k.setCounters(2, BtrfsDeviceErrors{Corruption: 5})
	returnedEpisode(t)
	route := ioctlFD
	ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
		if req == iocDevInfo {
			return unix.EIO
		}
		return route(fd, req, arg)
	}
	t.Cleanup(func() { ioctlFD = route })

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoReset(t, k)
	if msg := resyncedMessage(t); !strings.Contains(msg, " device 2 kept its error counters: they could not be read.") {
		t.Fatalf("whole again notice = %q, want device 2 named with no counts", msg)
	}
}

// withoutAt is missing without the times its disks were recorded missing.
func withoutAt(missing []missingDisk) []missingDisk {
	out := slices.Clone(missing)
	for i := range out {
		out[i].At = 0
	}
	return out
}

// withoutMissingAt is s with its missing disks withoutAt.
func withoutMissingAt(s poolState) poolState {
	s.Missing = withoutAt(s.Missing)
	return s
}
