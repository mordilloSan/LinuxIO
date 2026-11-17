package btrfs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ScrubRecordPath stores the last scrub LinuxIO ran on each filesystem,
// keyed by UUID, and while one runs where each device stands; the kernel
// keeps no history of its own.
var ScrubRecordPath = version.DataDir + "/btrfs-scrub.json"

// progsScrubDir is where btrfs-progs records the scrubs it ran, such as the
// btrfsmaintenance timer's, as scrub.status.<uuid> files.
var progsScrubDir = "/var/lib/btrfs"

// scrubSaveInterval is how often a running scrub saves where each device
// stands, so a power cut loses at most that much of it; a var so tests can
// shorten it.
var scrubSaveInterval = time.Minute

// maxScrubStalls is how many interrupted kernel calls in a row that moved
// nothing a device's scrub takes before it ends aborted rather than being
// issued again.
const maxScrubStalls = 3

// ErrScrubNotRunning is ScrubCancel's answer when no scrub runs.
var ErrScrubNotRunning = errors.New("no scrub is running")

// ErrScrubRunning is a scrub's answer when the kernel already scrubs the
// filesystem, for another scrub or a device replace.
var ErrScrubRunning = errors.New("a scrub or a device replace is already running")

// scrubRecord is one filesystem's entry in ScrubRecordPath. Its status holds
// what the scrub counted except what the devices' kernel calls still running
// counted, which each device keeps until its call returns; a scrub that
// ended keeps no device counters. An older build reads only the status.
type scrubRecord struct {
	BtrfsScrubStatus
	// Run is the job ID of the scrub; empty outside a job, as for a pool
	// resync.
	Run string `json:"run,omitzero"`
	// Aborted is set when an earlier part of this scrub aborted on a device
	// the record does not name.
	Aborted bool `json:"aborted,omitzero"`
	// Devices is where each device's scrub stands, by device id.
	Devices map[int]scrubDeviceRecord `json:"devices,omitzero"`
	// The process that writes the record, so a running scrub on record can
	// be told from one a crash left.
	PID      int32  `json:"pid,omitzero"`
	PIDStart uint64 `json:"pidStart,omitzero"`
	BootID   string `json:"bootId,omitzero"`
	// CancelRequested is ScrubCancel's request to the running scrub, which
	// reads it every tick: its calls may all wait for a thaw, with nothing
	// in the kernel to cancel. A scrub starting or resuming never inherits it.
	CancelRequested bool `json:"cancelRequested,omitzero"`
}

// live reports whether the record is of a scrub still running in a live
// process, which may have nothing in the kernel between calls or while a
// call waits for a thaw.
func (r scrubRecord) live() bool {
	return r.Status == "running" && r.writerAlive()
}

// writerAlive reports whether the process that wrote the record still runs.
func (r scrubRecord) writerAlive() bool {
	boot, err := jobs.BootID()
	return err == nil && r.BootID == boot && jobs.ProcessAlive(jobs.Executor{PID: r.PID, PIDStart: r.PIDStart})
}

// writtenByThisProcess stamps the record with this process, the writer.
func (r *scrubRecord) writtenByThisProcess() {
	pid := int32(os.Getpid())
	start, err := jobs.ProcessStart(pid)
	if err != nil {
		slog.Warn("btrfs scrub writer not identified", "error", err)
		return
	}
	boot, err := jobs.BootID()
	if err != nil {
		slog.Warn("btrfs scrub writer not identified", "error", err)
		return
	}
	r.PID, r.PIDStart, r.BootID = pid, start, boot
}

// scrubDeviceRecord is one device's part of a scrub on record.
type scrubDeviceRecord struct {
	// UUID is the device's own UUID: a resume skips nothing on a disk that
	// took the device id since.
	UUID string `json:"uuid"`
	// Offset is the kernel's last_physical for the device, the highest it
	// reported in this scrub.
	Offset  uint64 `json:"offset"`
	Done    bool   `json:"done,omitzero"`
	Aborted bool   `json:"aborted,omitzero"`
	// scrubCounters are what the device's running kernel call had counted
	// when last read.
	scrubCounters
}

// scrubCounters are the kernel counters a scrub status sums.
type scrubCounters struct {
	ErrorCount          uint64 `json:"errorCount,omitzero"`
	UncorrectableErrors uint64 `json:"uncorrectableErrors,omitzero"`
	CorrectedErrors     uint64 `json:"correctedErrors,omitzero"`
	BytesScrubbed       uint64 `json:"bytesScrubbed,omitzero"`
}

func countersOf(p scrubProgress) scrubCounters {
	return scrubCounters{
		ErrorCount:          p.ReadErrors + p.CsumErrors + p.VerifyErrors + p.SuperErrors,
		UncorrectableErrors: p.UncorrectableErrors,
		CorrectedErrors:     p.CorrectedErrors,
		BytesScrubbed:       p.DataBytesScrubbed + p.TreeBytesScrubbed,
	}
}

// total is the scrub's status with what the devices' running calls counted.
func (r scrubRecord) total() BtrfsScrubStatus {
	s := r.BtrfsScrubStatus
	for _, d := range r.Devices {
		s.addCounters(d.scrubCounters)
	}
	return s
}

// Scrub scrubs every present device of the filesystem from the start,
// outside any job; see ScrubFrom.
func Scrub(ctx context.Context, uuid string, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	return ScrubFrom(ctx, uuid, "", report)
}

// ScrubFrom scrubs every present device of the filesystem from the start, to
// completion, as job run (empty outside a job), reporting progress on the
// way, and records the outcome and, while it runs, where each device stands.
// A scrub the kernel already runs on the filesystem, its own or a device
// replace's, is refused with ErrScrubRunning and its record left alone.
// Canceling ctx cancels the scrub; the kernel then returns each device's
// partial counters. A device that aborted fails the scrub even then.
func ScrubFrom(ctx context.Context, uuid, run string, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	return scrub(ctx, uuid, run, nil, false, report)
}

// ResumeScrub continues job run's scrub after a reboot, as ScrubFrom runs
// one. A record of run that is still running (a power cut), canceled
// (Suspend) or aborted (a device aborted, then Suspend canceled the rest)
// gives each device's start and the counters so far. Otherwise only the
// devices in starts, the Suspend checkpoint, are scrubbed, each from its
// offset, carrying on the canceled or aborted scrub an older build recorded:
// the outcome keeps its start, counters and abort, so errors its first part
// found stay in the record. Nil starts scrub everything again.
func ResumeScrub(ctx context.Context, uuid, run string, starts map[int]uint64, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	return scrub(ctx, uuid, run, starts, true, report)
}

func scrub(ctx context.Context, uuid, run string, starts map[int]uint64, resume bool, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsScrubStatus{}, err
	}
	// Inventory skips a device it failed to read: the scrub would leave it
	// out, a resume drop its saved state, and the outcome read finished.
	if !fs.HealthKnown {
		return BtrfsScrubStatus{}, fmt.Errorf("could not read the devices of pool %s", uuid)
	}
	rec, have := loadScrubRecord(uuid)
	// Between kernel calls, or waiting for a thaw, a scrub has nothing in the
	// kernel to refuse another by: its record and process tell.
	if have && rec.live() {
		return BtrfsScrubStatus{}, fmt.Errorf("the scrub on record is still running: %w", ErrScrubRunning)
	}
	var seed scrubRecord
	if resume && have && rec.resumable(run) {
		seed, starts = rec.resumePoint(uuid, fs.Devices)
	} else {
		// An older build's record names no run and no devices.
		seed = scrubSeed(rec, have && (rec.Run == run || rec.Run == "" && rec.Devices == nil), starts)
	}
	seed.Run = run
	var previous *scrubRecord
	if have {
		previous = &rec
	}
	return runScrub(ctx, fs, seed, starts, previous, report)
}

// scrubSeed is the outcome a scrub starts from: on a resume (starts set) the
// canceled or aborted scrub on record, ours, keeping its start, counters and
// abort; otherwise a scrub starting now. A record still running means a
// crashed resume, as the inventory also reads it, and counts as aborted.
// Only an older build's record, or one of a scrub outside a job, gets here.
func scrubSeed(rec scrubRecord, ours bool, starts map[int]uint64) scrubRecord {
	if !ours || starts == nil || rec.StartedAt == nil || rec.Status != "canceled" && rec.Status != "aborted" && rec.Status != "running" {
		now := time.Now()
		return scrubRecord{Status: "finished", StartedAt: &now}
	}
	seed := scrubRecord{BtrfsScrubStatus: rec.total()}
	seed.FinishedAt, seed.Percent = nil, 0
	// An abort stays: the resume skips the device that aborted.
	switch rec.Status {
	case "running":
		seed.Status = "aborted"
	case "canceled":
		seed.Status = "finished"
	}
	return seed
}

// resumable reports whether the record holds job run's scrub, stopped by a
// power cut (still running), by Suspend (canceled), or by Suspend after a
// device aborted (aborted).
func (r scrubRecord) resumable(run string) bool {
	return run != "" && r.Run == run && r.StartedAt != nil && (r.Status == "running" || r.Status == "canceled" || r.Status == "aborted")
}

// resumePoint is where the scrub on record continues: its start and every
// counter so far, and each present device from just below its offset. The
// kernel scrubs whole the dev extent holding the start, so the one byte
// back re-reads the extent the offset ends, whose last stripes the kernel
// may not have checked when it reported the offset. A device that finished
// or aborted is skipped, and an abort kept; one the record does not name,
// or names with another UUID, is scrubbed from 0.
func (r scrubRecord) resumePoint(uuid string, devices []BtrfsDevice) (scrubRecord, map[int]uint64) {
	seed := scrubRecord{BtrfsScrubStatus: r.total(), Devices: map[int]scrubDeviceRecord{}}
	seed.Status, seed.FinishedAt, seed.Percent = "finished", nil, 0
	aborted := r.Aborted
	for _, d := range r.Devices {
		aborted = aborted || d.Aborted
	}
	if aborted {
		seed.Status = "aborted"
	}
	starts := map[int]uint64{}
	for _, d := range devices {
		saved, ok := r.Devices[d.ID]
		switch {
		case d.Missing:
		case !ok || saved.UUID != d.UUID:
			starts[d.ID] = 0
			slog.Debug("btrfs scrub device has no saved progress", "uuid", uuid, "devid", d.ID)
		case saved.Done || saved.Aborted:
			seed.Devices[d.ID] = scrubDeviceRecord{UUID: saved.UUID, Offset: saved.Offset, Done: saved.Done, Aborted: saved.Aborted}
		default:
			starts[d.ID] = saved.Offset - min(saved.Offset, 1)
			seed.Devices[d.ID] = scrubDeviceRecord{UUID: saved.UUID, Offset: saved.Offset}
			slog.Debug("btrfs scrub resumed from saved progress", "uuid", uuid, "devid", d.ID, "offset", starts[d.ID])
		}
	}
	return seed, starts
}

// runScrub scrubs the devices in starts (nil: every present device from 0)
// from seed, the outcome so far; previous is the record it replaces.
func runScrub(ctx context.Context, fs BtrfsFilesystem, seed scrubRecord, starts map[int]uint64, previous *scrubRecord, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	mnt := fs.Mountpoints[0].Mountpoint
	// btrfs-progs asks the kernel first too; a refused scrub must not write
	// over the record of the one running.
	if err := refuseRunningScrub(mnt, fs.Devices); err != nil {
		return BtrfsScrubStatus{}, err
	}
	r := newScrubRun(fs, seed)
	r.previous = previous
	r.plan(fs.Devices, starts)
	// The resumed run's record keeps the first part's counters and
	// offsets, so a cut early in the resume loses nothing.
	r.save(ctx)
	for id, start := range r.calls {
		r.issue(id, start)
	}

	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	for len(r.calls) > 0 {
		select {
		case c := <-r.results:
			r.returned(ctx, c)
		case <-ctxDone:
			ctxDone = nil
			r.cancel()
		case <-ticker.C:
			r.tick(ctx, report)
		}
	}
	return r.finish(ctx)
}

// plan tracks the devices to scrub, those in starts (nil: every present
// device from 0).
func (r *scrubRun) plan(devices []BtrfsDevice, starts map[int]uint64) {
	for _, d := range devices {
		start, listed := starts[d.ID]
		switch {
		case d.Missing:
		case starts == nil || listed:
			r.track(d, start)
		default:
			// A resume carries the state of a device it skips; a Suspend
			// checkpoint leaves out the devices it found idle, as finished.
			if _, carried := r.rec.Devices[d.ID]; !carried {
				r.rec.Devices[d.ID] = scrubDeviceRecord{UUID: d.UUID, Done: true}
			}
		}
	}
}

// tick cancels again while canceled, reads progress, reports it and saves
// it when due.
func (r *scrubRun) tick(ctx context.Context, report func(BtrfsScrubStatus)) {
	// A call issued again during a freeze waits for the thaw and starts
	// after the cancel found nothing to cancel.
	if !r.canceled {
		r.canceled = r.cancelRequested()
	}
	if ctx.Err() != nil || r.canceled {
		r.cancel()
	}
	r.poll()
	report(r.status())
	if time.Since(r.savedAt) >= scrubSaveInterval && r.moved() {
		r.save(ctx)
	}
}

// refuseRunningScrub returns ErrScrubRunning when the kernel scrubs any
// device of the filesystem, a missing one included (a replace's source), or
// runs a device replace, during which it refuses every scrub.
func refuseRunningScrub(mnt string, devices []BtrfsDevice) error {
	if state, _, _, err := replaceStatusOf(mnt); err == nil && state == replaceStateStarted {
		return fmt.Errorf("device replace: %w", ErrScrubRunning)
	}
	for _, d := range devices {
		switch _, running, err := deviceScrubProgress(mnt, d.ID); {
		case err != nil:
			return err
		case running:
			return fmt.Errorf("scrub device %d: %w", d.ID, ErrScrubRunning)
		}
	}
	return nil
}

// deviceScrubProgress reads where the kernel's scrub of device id stands;
// running is false when the kernel scrubs nothing there (ENOTCONN).
func deviceScrubProgress(mnt string, id int) (p scrubProgress, running bool, err error) {
	args := scrubArgs{DevID: uint64(id)}
	switch err := fsIoctl(mnt, iocScrubProgress, unsafe.Pointer(&args)); {
	case err == nil:
		return args.Progress, true, nil
	case errors.Is(err, unix.ENOTCONN):
		return scrubProgress{}, false, nil
	default:
		return scrubProgress{}, false, fmt.Errorf("scrub progress of device %d: %w", id, err)
	}
}

// scrubCall is what one kernel scrub call of a device returned.
type scrubCall struct {
	id       int
	progress scrubProgress
	err      error
}

// scrubRun is a running scrub, owned by runScrub's loop. rec is the record
// it saves: its status is the outcome so far with the counters of every
// kernel call that returned, its devices where each stands.
type scrubRun struct {
	mnt, uuid string
	used      uint64
	rec       scrubRecord
	calls     map[int]uint64 // the start of each device's call in flight
	stalls    map[int]int    // interrupted calls in a row that moved nothing
	results   chan scrubCall
	firstErr  error
	warned    bool // a cancel failed for a reason other than finding no scrub
	savedAt   time.Time
	saved     map[int]uint64 // each device's offset when last saved
	previous  *scrubRecord   // the record this scrub replaced
	accepted  bool           // the kernel took a call, rather than refusing it as busy
	refused   bool           // the kernel refused a call: another scrub runs
	canceled  bool           // a call was canceled, or ScrubCancel asked for it
	// The records file as the loop last read it for a cancel request.
	readMod  time.Time
	readSize int64
}

func newScrubRun(fs BtrfsFilesystem, seed scrubRecord) *scrubRun {
	r := &scrubRun{
		mnt: fs.Mountpoints[0].Mountpoint, uuid: fs.UUID, used: fs.UsedBytes, rec: seed,
		calls: map[int]uint64{}, stalls: map[int]int{}, results: make(chan scrubCall), saved: map[int]uint64{},
	}
	r.rec.Aborted = r.rec.Status == "aborted"
	if r.rec.Devices == nil {
		r.rec.Devices = map[int]scrubDeviceRecord{}
	}
	r.rec.writtenByThisProcess()
	return r
}

// track adds a device to scrub from start, keeping the offset a resume
// saved for it.
func (r *scrubRun) track(d BtrfsDevice, start uint64) {
	dev, ok := r.rec.Devices[d.ID]
	if !ok || dev.UUID != d.UUID {
		dev = scrubDeviceRecord{UUID: d.UUID, Offset: start}
	}
	r.rec.Devices[d.ID] = dev
	r.calls[d.ID] = start
}

// issue scrubs device id from start in a goroutine that sends what the
// kernel returned to results, which the loop reads until no call is left.
func (r *scrubRun) issue(id int, start uint64) {
	r.calls[id] = start
	go func() {
		// The kernel scrubs only [Start, End]; up to the device's end, as
		// btrfs-progs asks.
		args := scrubArgs{DevID: uint64(id), Start: start, End: ^uint64(0)}
		err := blockingIoctl(r.mnt, iocScrub, unsafe.Pointer(&args))
		r.results <- scrubCall{id: id, progress: args.Progress, err: err}
	}()
}

// returned folds a call's counters into the outcome. A call a freeze
// interrupted (EINTR, kernel 6.19) while ctx is live is issued again from
// where it stopped; once ctx or any call was canceled it counts as canceled,
// whatever the outcome so far. A call refused because another scrub runs
// (EINPROGRESS) fails the scrub with ErrScrubRunning.
func (r *scrubRun) returned(ctx context.Context, c scrubCall) {
	start := r.calls[c.id]
	delete(r.calls, c.id)
	r.rec.add(c.progress)
	dev := r.rec.Devices[c.id]
	dev.scrubCounters = scrubCounters{}
	dev.Offset = max(dev.Offset, c.progress.LastPhysical)
	err := c.err
	r.canceled = r.canceled || errors.Is(err, unix.ECANCELED)
	if errors.Is(err, unix.EINPROGRESS) {
		r.refused = true
		err = fmt.Errorf("scrub device %d: %w: %w", c.id, ErrScrubRunning, err)
	} else {
		r.accepted = true
	}
	if errors.Is(err, unix.EINTR) {
		if ctx.Err() != nil || r.canceled {
			err = unix.ECANCELED
		} else if next, again := r.again(c, start); again {
			r.rec.Devices[c.id] = dev
			// The save takes over a cancel requested since the last tick: a
			// call issued now would wait for the thaw for nothing.
			r.save(ctx)
			if !r.canceled {
				slog.Debug("btrfs scrub interrupted and issued again", "uuid", r.uuid, "devid", c.id, "offset", next)
				r.issue(c.id, next)
				return
			}
			err = unix.ECANCELED
		}
	}
	switch {
	case err == nil:
		dev.Done = true
	case errors.Is(err, unix.ECANCELED):
	default:
		dev.Aborted = true
	}
	r.rec.Devices[c.id] = dev
	r.firstErr = r.rec.absorb(err, r.firstErr)
	if len(r.calls) > 0 {
		r.save(ctx)
	}
}

// again returns where to issue an interrupted call again: one byte below
// the last_physical it returned, as a resume does, never below its own
// start. It refuses after maxScrubStalls calls in a row that did not get
// past their start: the kernel re-reads the dev extent holding the start,
// reporting positions below it, so only a position above it is progress.
func (r *scrubRun) again(c scrubCall, start uint64) (uint64, bool) {
	p := c.progress
	if p.LastPhysical > start {
		r.stalls[c.id] = 0
	} else {
		r.stalls[c.id]++
	}
	if r.stalls[c.id] >= maxScrubStalls {
		return 0, false
	}
	return max(start, p.LastPhysical-min(p.LastPhysical, 1)), true
}

// cancel asks the kernel to cancel the scrub. While a freeze holds a call
// issued again it finds nothing (ENOTCONN), so the loop sends it again.
func (r *scrubRun) cancel() {
	err := cancelScrub(r.mnt)
	if err != nil && !errors.Is(err, unix.ENOTCONN) && !r.warned {
		r.warned = true
		slog.Warn("btrfs scrub not canceled", "mountpoint", r.mnt, "error", err)
	}
}

// poll reads the progress of each call in flight. A device the kernel
// reports idle keeps what was read last: its call has just returned, or
// waits to start.
func (r *scrubRun) poll() {
	for id := range r.calls {
		p, running, err := deviceScrubProgress(r.mnt, id)
		switch {
		case err != nil:
			slog.Debug("btrfs scrub progress not read", "uuid", r.uuid, "devid", id, "error", err)
		case running:
			dev := r.rec.Devices[id]
			dev.scrubCounters = countersOf(p)
			dev.Offset = max(dev.Offset, p.LastPhysical)
			r.rec.Devices[id] = dev
		}
	}
}

// status is the running scrub as a report shows it.
func (r *scrubRun) status() BtrfsScrubStatus {
	s := r.rec.total()
	s.Status, s.Percent = "running", r.percent(s.BytesScrubbed)
	return s
}

func (r *scrubRun) percent(bytes uint64) float64 {
	if r.used == 0 {
		return 0
	}
	return min(100, float64(bytes)*100/float64(r.used))
}

// moved reports whether a device's offset changed since the last save.
func (r *scrubRun) moved() bool {
	for id, d := range r.rec.Devices {
		if d.Offset != r.saved[id] {
			return true
		}
	}
	return false
}

// save stores the running scrub's record.
func (r *scrubRun) save(ctx context.Context) {
	rec := r.rec
	rec.Status = "running"
	rec.Percent = r.percent(rec.total().BytesScrubbed)
	r.write(ctx, rec)
	r.savedAt = time.Now()
	for id, d := range r.rec.Devices {
		r.saved[id] = d.Offset
	}
}

// write stores rec, keeping a cancel ScrubCancel asked of this run since the
// loop last read the record, and taking it over.
func (r *scrubRun) write(ctx context.Context, rec scrubRecord) {
	err := updateScrubRecords(ctx, func(records map[string]scrubRecord) bool {
		if cur, ok := records[r.uuid]; ok && r.ours(cur) && cur.CancelRequested {
			r.rec.CancelRequested, r.canceled = true, true
		}
		rec.CancelRequested = r.rec.CancelRequested
		records[r.uuid] = rec
		return true
	})
	if err != nil {
		slog.Warn("btrfs scrub record not saved", "uuid", r.uuid, "error", err)
	}
}

// ours reports whether rec is this run's running record, as it saved it.
// A resumed run shares its job and start with the record a power loss left,
// and after a reboot its process may get the same pid and start tick, so
// the boot tells them apart; a run that could not stamp its process owns no
// record.
func (r *scrubRun) ours(rec scrubRecord) bool {
	return r.rec.PID != 0 && rec.Status == "running" && rec.Run == r.rec.Run &&
		rec.PID == r.rec.PID && rec.PIDStart == r.rec.PIDStart && rec.BootID == r.rec.BootID &&
		rec.StartedAt != nil && r.rec.StartedAt != nil && rec.StartedAt.Equal(*r.rec.StartedAt)
}

// cancelRequested reads whether ScrubCancel asked this run to stop. Every
// write replaces the file, so an unchanged time and size mean nothing new.
func (r *scrubRun) cancelRequested() bool {
	info, err := os.Stat(ScrubRecordPath)
	if err != nil || info.ModTime().Equal(r.readMod) && info.Size() == r.readSize {
		return false
	}
	r.readMod, r.readSize = info.ModTime(), info.Size()
	rec, ok := loadScrubRecord(r.uuid)
	if ok && r.ours(rec) && rec.CancelRequested {
		r.rec.CancelRequested = true
		return true
	}
	return false
}

// finish records the outcome with the progress each call returned. A scrub
// the kernel refused on every device, as another started at the same time,
// puts back the record it replaced, unless the other scrub saved its own.
func (r *scrubRun) finish(ctx context.Context) (BtrfsScrubStatus, error) {
	if r.refused && !r.accepted {
		err := updateScrubRecords(ctx, func(records map[string]scrubRecord) bool {
			if !r.ours(records[r.uuid]) {
				return false
			}
			if r.previous != nil {
				records[r.uuid] = *r.previous
			} else {
				delete(records, r.uuid)
			}
			return true
		})
		if err != nil {
			slog.Warn("btrfs scrub record not put back", "uuid", r.uuid, "error", err)
		}
		return BtrfsScrubStatus{}, fmt.Errorf("scrub: %w", r.firstErr)
	}
	end := time.Now()
	r.rec.FinishedAt = &end
	if r.rec.Status == "finished" {
		r.rec.Percent = 100
	} else {
		r.rec.Percent = r.percent(r.rec.BytesScrubbed)
	}
	r.write(ctx, r.rec)
	final := r.rec.BtrfsScrubStatus
	if r.firstErr != nil {
		return final, fmt.Errorf("scrub: %w", r.firstErr)
	}
	if ctx.Err() != nil {
		return final, ctx.Err()
	}
	return final, nil
}

// absorb folds one device's outcome into the status: an abort wins over a
// cancel, since a resume would skip the aborted device, and the first real
// error is kept.
func (s *BtrfsScrubStatus) absorb(err, firstErr error) error {
	switch {
	case err == nil:
	case errors.Is(err, unix.ECANCELED):
		if s.Status == "finished" {
			s.Status = "canceled"
		}
	default:
		s.Status = "aborted"
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ScrubCancel cancels the running scrub. When the kernel has nothing to
// cancel (ENOTCONN), a LinuxIO scrub running in a live process gets the
// request in its record, which it reads every tick: its calls may all be
// issued again after a freeze and wait for the thaw, and the request stops
// them once they start. Only with no such scrub does ENOTCONN mean
// ErrScrubNotRunning. When the kernel did cancel calls, the scrub keeps
// canceling the rest by itself.
func ScrubCancel(ctx context.Context, uuid string) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	switch err := cancelScrub(mnt); {
	case err == nil:
		return nil
	case !errors.Is(err, unix.ENOTCONN):
		return fmt.Errorf("cancel scrub: %w", err)
	}
	// Read first: no scrub needs neither the lock nor a writable file.
	if rec, ok := loadScrubRecord(uuid); !ok || !rec.live() {
		return ErrScrubNotRunning
	}
	requested := false
	if err := updateScrubRecords(ctx, func(records map[string]scrubRecord) bool {
		rec, ok := records[uuid]
		if !ok || !rec.live() {
			return false
		}
		rec.CancelRequested, requested = true, true
		records[uuid] = rec
		return true
	}); err != nil {
		return fmt.Errorf("request the cancel of the scrub: %w", err)
	}
	if !requested {
		return ErrScrubNotRunning
	}
	return nil
}

// cancelScrub cancels the running scrub, unless a device replace runs:
// SCRUB_CANCEL cancels every scrub of the filesystem, the replace's too.
// STATUS may report an earlier replace's end, so only STARTED refuses.
func cancelScrub(mnt string) error {
	if state, _, _, err := replaceStatusOf(mnt); err == nil && state == replaceStateStarted {
		return errors.New("a device replace is running, and canceling the scrub would cancel it too")
	}
	return fsIoctl(mnt, iocScrubCancel, nil)
}

// ScrubPositions returns where each device's running scrub stands, its
// last_physical, for Suspend's checkpoint, which ResumeScrub starts from. A
// device the kernel reports idle (ENOTCONN), whether it finished, failed or
// never started, is left out.
func ScrubPositions(ctx context.Context, uuid string) (map[int]uint64, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return nil, err
	}
	positions := map[int]uint64{}
	for _, d := range fs.Devices {
		if d.Missing {
			continue
		}
		p, running, err := deviceScrubProgress(fs.Mountpoints[0].Mountpoint, d.ID)
		if err != nil {
			return nil, err
		}
		if running {
			positions[d.ID] = p.LastPhysical
		}
	}
	return positions, nil
}

func loadScrubRecords() map[string]scrubRecord {
	records := map[string]scrubRecord{}
	data, err := os.ReadFile(ScrubRecordPath)
	if err != nil {
		return records
	}
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Warn("btrfs scrub records unreadable", "path", ScrubRecordPath, "error", err)
	}
	return records
}

func loadScrubRecord(uuid string) (scrubRecord, bool) {
	rec, ok := loadScrubRecords()[uuid]
	return rec, ok
}

// updateScrubRecords applies fn to the records under their lock, as every
// writer does (the job host and linuxio-worker resync), and writes
// them when fn reports a change.
func updateScrubRecords(ctx context.Context, fn func(map[string]scrubRecord) bool) error {
	// The final record is written after a cancel too.
	return filelock.RunExclusive(context.WithoutCancel(ctx), ScrubRecordPath+".lock", func() error {
		records := loadScrubRecords()
		if !fn(records) {
			return nil
		}
		// The lock created the directory.
		data, err := json.Marshal(records, jsontext.WithIndent("  "), json.Deterministic(true))
		if err != nil {
			return fmt.Errorf("marshal %s: %w", ScrubRecordPath, err)
		}
		if err := atomicfile.Write(ScrubRecordPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", ScrubRecordPath, err)
		}
		return nil
	}, filelock.WithTimeout(rulesLockWait))
}

// lastScrub returns the newer of LinuxIO's own record, rec when have, and
// btrfs-progs' status file for the filesystem.
func lastScrub(uuid string, rec scrubRecord, haveOurs bool) (BtrfsScrubStatus, bool) {
	ours := rec.total()
	theirs, haveTheirs := progsScrubStatus(uuid)
	switch {
	case haveOurs && haveTheirs:
		if theirs.StartedAt != nil && (ours.StartedAt == nil || theirs.StartedAt.After(*ours.StartedAt)) {
			return theirs, true
		}
		return ours, true
	case haveOurs:
		return ours, true
	case haveTheirs:
		return theirs, true
	}
	return BtrfsScrubStatus{}, false
}

// progsScrubStatus reads btrfs-progs' scrub.status.<uuid> file: a version
// line, then one "<uuid>:<devid>|key:value|..." line per device.
func progsScrubStatus(uuid string) (BtrfsScrubStatus, bool) {
	data, err := os.ReadFile(filepath.Join(progsScrubDir, "scrub.status."+uuid))
	if err != nil {
		return BtrfsScrubStatus{}, false
	}
	return parseProgsScrubStatus(string(data))
}

func parseProgsScrubStatus(text string) (BtrfsScrubStatus, bool) {
	s := BtrfsScrubStatus{Status: "finished"}
	var start, duration uint64
	devices := 0
	for line := range strings.Lines(text) {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) < 2 || !strings.Contains(fields[0], ":") || strings.HasPrefix(fields[0], "scrub status") {
			continue
		}
		devices++
		d := parseProgsScrubDevice(fields[1:])
		s.add(d.progress)
		if start == 0 || (d.start > 0 && d.start < start) {
			start = d.start
		}
		duration = max(duration, d.duration)
		switch {
		case d.canceled:
			s.Status = "canceled"
		case !d.finished && s.Status == "finished":
			s.Status = "aborted"
		}
	}
	if devices == 0 {
		return BtrfsScrubStatus{}, false
	}
	if start > 0 {
		t := time.Unix(int64(start), 0)
		s.StartedAt = &t
		end := t.Add(time.Duration(duration) * time.Second)
		s.FinishedAt = &end
	}
	if s.Status == "finished" {
		s.Percent = 100
	}
	return s, true
}

// progsScrubDevice is one device line of btrfs-progs' scrub status file.
type progsScrubDevice struct {
	progress           scrubProgress
	start, duration    uint64
	canceled, finished bool
}

func parseProgsScrubDevice(fields []string) progsScrubDevice {
	var d progsScrubDevice
	counters := map[string]*uint64{
		"data_bytes_scrubbed": &d.progress.DataBytesScrubbed, "tree_bytes_scrubbed": &d.progress.TreeBytesScrubbed,
		"read_errors": &d.progress.ReadErrors, "csum_errors": &d.progress.CsumErrors, "verify_errors": &d.progress.VerifyErrors,
		"super_errors": &d.progress.SuperErrors, "uncorrectable_errors": &d.progress.UncorrectableErrors,
		"corrected_errors": &d.progress.CorrectedErrors, "t_start": &d.start, "duration": &d.duration,
	}
	for _, f := range fields {
		key, val, _ := strings.Cut(f, ":")
		n, _ := strconv.ParseUint(val, 10, 64)
		switch key {
		case "canceled":
			d.canceled = n != 0
		case "finished":
			d.finished = n != 0
		default:
			if p := counters[key]; p != nil {
				*p = n
			}
		}
	}
	return d
}
