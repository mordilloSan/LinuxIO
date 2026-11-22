package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// swapFileTask is the task metadata of the four swap file routes: one
// identity, since one memory change runs at a time, with the operation as
// its action and the swap file as its path.
func swapFileTask(label, action, path string) bridgeipc.TaskMetadata {
	return bridgeipc.TaskMetadata{Identity: []string{"memory", "swap_file"}, Label: label, Action: action, Path: path}
}

// CreateSwapFile makes LinuxIO's swap file at the requested size, adds its
// /etc/fstab line and turns it on. The result is the host's status after it.
func CreateSwapFile(ctx context.Context, req apischema.SwapFileCreateRequest, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).createSwapFileRoute(ctx, req)
}

// ResizeSwapFile changes the size of a swap file in place: it turns the file
// off, changes its length and turns it on, with a temporary swap file beside
// it while the host would otherwise be without swap.
func ResizeSwapFile(ctx context.Context, req apischema.SwapFileResizeRequest, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).resizeSwapFile(ctx, req)
}

// RemoveSwapFile turns a swap file off, removes its /etc/fstab lines and
// deletes it.
func RemoveSwapFile(ctx context.Context, req apischema.SwapFilePathRequest, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).removeSwapFile(ctx, req)
}

// MoveSwapFile makes LinuxIO's swap file at the size of a swap file on btrfs
// and then removes that file, so swap is added before any is removed.
func MoveSwapFile(ctx context.Context, req apischema.SwapFilePathRequest, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).moveSwapFile(ctx, req)
}

// validateSwapFileSize refuses a size that is not a whole number of MiB or
// is below the smallest size, before a change waits for the lock.
func validateSwapFileSize(size uint64) error {
	switch {
	case size&mibMask != 0:
		return fmt.Errorf("%w: a swap file's size must be a whole number of MiB, got %d bytes", bridgeipc.ErrInvalidArgs, size)
	case size < swapFileMinSize:
		return fmt.Errorf("%w: a swap file must be at least %d MiB, got %d MiB", bridgeipc.ErrInvalidArgs, swapFileMinSize>>20, size>>20)
	default:
		return nil
	}
}

// fitsLimit refuses a size above the largest one the disk allows.
func fitsLimit(size, maxSize uint64) error {
	if size > maxSize {
		return fmt.Errorf("%w: the disk has room for a swap file of at most %d MiB, got %d MiB", bridgeipc.ErrInvalidArgs, maxSize>>20, size>>20)
	}
	return nil
}

// stepReporter reports the steps of one operation, numbered as they start.
type stepReporter struct {
	e            *executor
	index, total int
}

func (s *stepReporter) start(step apischema.SwapStep, device string) {
	s.index++
	s.e.report(apischema.SwapProgress{Step: step, Index: s.index, Total: s.total, Device: device})
}

// findRow returns the swap file row with exactly path.
func findRow(status apischema.SwapStatus, path string) (apischema.SwapFileRow, bool) {
	i := slices.IndexFunc(status.SwapFiles, func(r apischema.SwapFileRow) bool { return r.Path == path })
	if i < 0 {
		return apischema.SwapFileRow{}, false
	}
	return status.SwapFiles[i], true
}

// selectRow is the row a request's path selects on a freshly read status:
// ErrNotFound when no row has exactly that path, ErrConflict when the row
// fails the strict check. The path is never used for more than this.
func selectRow(status apischema.SwapStatus, path string) (apischema.SwapFileRow, error) {
	row, ok := findRow(status, path)
	if !ok {
		return apischema.SwapFileRow{}, fmt.Errorf("%w: no swap file %q", bridgeipc.ErrNotFound, path)
	}
	if row.LockedReason != "" {
		return apischema.SwapFileRow{}, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, lockedRefusal(row))
	}
	return row, nil
}

// lockedRefusal says why LinuxIO does not change a swap file.
func lockedRefusal(row apischema.SwapFileRow) string {
	switch row.LockedReason {
	case apischema.SwapFileNotInFstab:
		return fmt.Sprintf("no /etc/fstab line names %s, so something other than /etc/fstab turns it on", row.Path)
	case apischema.SwapFileNotRootOnly:
		return fmt.Sprintf("a folder on the path of %s is a link or can be changed by users other than root, or it is not a regular file root owns", row.Path)
	case apischema.SwapFileHibernation:
		return fmt.Sprintf("the host resumes from the filesystem of %s at a file offset, so it may be the hibernation file", row.Path)
	default:
		return fmt.Sprintf("%s is locked: %s", row.Path, row.LockedReason)
	}
}

// createRefusal says why LinuxIO cannot make its swap file.
func createRefusal(reason apischema.SwapFileCreateReason) string {
	switch reason {
	case apischema.SwapFileCreateExists:
		return ownSwapFilePath + " is there already"
	case apischema.SwapFileCreateUnsupportedFilesystem:
		return "LinuxIO makes swap files only on ext4, xfs and btrfs"
	case apischema.SwapFileCreateReadOnly:
		return "the filesystem of " + ownSwapDir + " is read-only"
	case apischema.SwapFileCreateBtrfsLayout:
		return "the btrfs filesystem of " + ownSwapDir + " has more than one device or data that is not single"
	case apischema.SwapFileCreateNotEnoughSpace:
		return "the filesystem of " + ownSwapDir + " has no room for a swap file beside the space it keeps free"
	case apischema.SwapFileCreateFolderNotRootOnly:
		return "a folder on the path of " + ownSwapFilePath + " is a link or can be changed by users other than root"
	case apischema.SwapFileCreatePathTaken:
		return "something that is not LinuxIO's swap file is at " + ownSwapFilePath + " or in its folder"
	default:
		return "LinuxIO cannot make its swap file: " + string(reason)
	}
}

// resizeRefusal says why a swap file cannot be resized.
func resizeRefusal(row apischema.SwapFileRow) string {
	switch row.ResizeBlockedReason {
	case apischema.SwapFileResizeMissing:
		return row.Path + " is missing"
	case apischema.SwapFileResizeNoHeader:
		return row.Path + " is not a swap file"
	case apischema.SwapFileResizeOff:
		return row.Path + " does not swap, and LinuxIO resizes only a swap file that does"
	case apischema.SwapFileResizeUnsupportedFilesystem:
		return "LinuxIO resizes swap files only on ext4, xfs and btrfs"
	case apischema.SwapFileResizeReadOnly:
		return "the filesystem of " + row.Path + " is read-only"
	case apischema.SwapFileResizeBtrfsLayout:
		return "the btrfs filesystem of " + row.Path + " has more than one device or data that is not single"
	default:
		return row.Path + " cannot be resized: " + string(row.ResizeBlockedReason)
	}
}

// errNoRoom is the room check refusing to turn a swap device off.
var errNoRoom = errors.New("not enough free memory and free swap to take back the data parked on it")

// turnOff takes the active disk swap device at path away, after the room
// check on a freshly read status. It reports false, doing nothing, for a
// device that is not active. A refusal of the room check wraps errNoRoom and
// ErrConflict.
func (e *executor) turnOff(ctx context.Context, path string) (bool, error) {
	status, _, err := e.sys.status(ctx)
	if err != nil {
		return false, err
	}
	if !slices.ContainsFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Path == path }) {
		return false, nil
	}
	if !roomToTurnOff(status, path) {
		return false, fmt.Errorf("%w: %s: %w", bridgeipc.ErrConflict, path, errNoRoom)
	}
	return true, e.swapOffDevice(ctx, path)
}

// turnOn turns the swap file at path on with the options of its fstab line
// and waits, within the swapOn bound, for it in /proc/swaps. A swapon that
// fails with the file listed all the same counts as done.
func (e *executor) turnOn(ctx context.Context, path, options string) error {
	if err := e.sys.swapOn(ctx, path, options); err != nil {
		if on, readErr := e.deviceIs(path, true)(); readErr == nil && on {
			return nil
		}
		return fmt.Errorf("turn on %s: %w", path, err)
	}
	if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, e.deviceIs(path, true)); err != nil {
		return fmt.Errorf("wait for %s in %s: %w", path, procSwaps, err)
	}
	return nil
}

// addFstabLine adds the swap line of path to /etc/fstab and reloads systemd,
// so the unit its generator makes for the line follows.
func (e *executor) addFstabLine(ctx context.Context, path string) error {
	if err := e.sys.addFstabSwap(ctx, path); err != nil {
		return fmt.Errorf("add the /etc/fstab line of %s: %w", path, err)
	}
	return e.sys.reload(ctx)
}

// removeFstabLines removes the swap lines of path from /etc/fstab and
// reloads systemd.
func (e *executor) removeFstabLines(ctx context.Context, path string) error {
	if err := e.sys.removeFstabSwap(ctx, path); err != nil {
		return fmt.Errorf("remove the /etc/fstab lines of %s: %w", path, err)
	}
	return e.sys.reload(ctx)
}

// fstabOptions is the options field of the first /etc/fstab line naming
// path, or empty.
func fstabOptions(fstab []fstabSwap, path string) string {
	if i := fstabIndex(fstab, path); i >= 0 {
		return fstab[i].options
	}
	return ""
}

// cleanupContext is a context for undoing or finishing what a step left
// once a swap device is off or a file is made: it outlives a cancelled
// request, within a bound.
func (e *executor) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout+e.waits.swapOn)
}

func (e *executor) createSwapFileRoute(ctx context.Context, req apischema.SwapFileCreateRequest) (apischema.SwapStatus, error) {
	if err := validateSwapFileSize(req.Size); err != nil {
		return apischema.SwapStatus{}, err
	}
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	steps := &stepReporter{e: e, total: 1}
	steps.start(apischema.SwapStepCreateSwapFile, ownSwapFilePath)
	if err := e.createSwapFile(ctx, view.status, req.Size); err != nil {
		return apischema.SwapStatus{}, err
	}
	return e.statusAfterChange(ctx)
}

// createSwapFile makes LinuxIO's swap file of size bytes on a host whose
// status is status: the file, then its /etc/fstab line, then swapon. A swap
// file a cut-short Create left at the path is deleted first. A failure
// before the file swaps takes the line and the file away again, the line
// first; once it swaps it stays, since added swap is never undone.
func (e *executor) createSwapFile(ctx context.Context, status apischema.SwapStatus, size uint64) error {
	if reason := status.Create.BlockedReason; reason != "" {
		return fmt.Errorf("%w: %s", bridgeipc.ErrConflict, createRefusal(reason))
	}
	if err := fitsLimit(size, status.Create.MaxSize); err != nil {
		return err
	}
	if e.root.leftoverOwnSwapFile() {
		id, err := e.root.fileIDOf(ownSwapFilePath)
		if err != nil {
			return err
		}
		if err := e.sys.deleteSwapFile(ctx, ownSwapFilePath, id); err != nil {
			return fmt.Errorf("delete the swap file a cut-short create left at %s: %w", ownSwapFilePath, err)
		}
	}
	id, err := e.sys.makeSwapFile(ctx, ownSwapFilePath, size)
	if err != nil {
		return err
	}
	// The file is made: its line, its start or its removal is not cancelled
	// with the request.
	ctx, cancel := e.cleanupContext(ctx)
	defer cancel()
	if err := e.addFstabLine(ctx, ownSwapFilePath); err != nil {
		return errors.Join(err, e.undoCreate(ctx, id))
	}
	// Another program can rewrite /etc/fstab without the lock (udisks2,
	// cloud-init, an installer): a line that did not stay is a failed add,
	// since nothing would turn the file on at boot.
	if !e.root.fstabNames(ownSwapFilePath) {
		return errors.Join(fmt.Errorf("the /etc/fstab line of %s was gone right after it was added: another program rewrote /etc/fstab", ownSwapFilePath), e.undoCreate(ctx, id))
	}
	if err := e.turnOn(ctx, ownSwapFilePath, fstabSwapOptions); err != nil {
		return errors.Join(err, e.undoCreate(ctx, id))
	}
	slog.Info("swap file created", "path", ownSwapFilePath, "size", size)
	return nil
}

// undoCreate takes away a swap file Create made that does not swap: its
// fstab line, which may or may not have been written, then the file. A line
// that cannot be removed keeps the file, which is a whole swap file, so the
// line never names a missing one.
func (e *executor) undoCreate(ctx context.Context, id fileID) error {
	if err := e.removeFstabLines(ctx, ownSwapFilePath); err != nil {
		return fmt.Errorf("%s stays: %w", ownSwapFilePath, err)
	}
	if err := e.sys.deleteSwapFile(ctx, ownSwapFilePath, id); err != nil {
		return fmt.Errorf("delete %s: %w", ownSwapFilePath, err)
	}
	return nil
}

func (e *executor) resizeSwapFile(ctx context.Context, req apischema.SwapFileResizeRequest) (apischema.SwapStatus, error) {
	if err := validateSwapFileSize(req.Size); err != nil {
		return apischema.SwapStatus{}, err
	}
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	row, err := selectRow(view.status, req.Path)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	switch {
	case row.Temporary:
		return apischema.SwapStatus{}, fmt.Errorf("%w: %s is a temporary swap file, which is removed and not resized", bridgeipc.ErrConflict, row.Path)
	case row.ResizeBlockedReason != "":
		return apischema.SwapStatus{}, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, resizeRefusal(row))
	}
	if err = fitsLimit(req.Size, row.MaxSize); err != nil {
		return apischema.SwapStatus{}, err
	}
	run, err := e.planResize(view, row)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	if req.Size == run.length {
		return apischema.SwapStatus{}, fmt.Errorf("%w: %s is %d MiB already", bridgeipc.ErrInvalidArgs, row.Path, req.Size>>20)
	}
	if err := run.do(ctx, req.Size); err != nil {
		return apischema.SwapStatus{}, err
	}
	return e.statusAfterChange(ctx)
}

// resizeRun is one resize of the swap file row: the file's identity and
// length, the options its fstab line turns it on with, and the temporary
// swap file beside it, made by the run (makeTemporary) or found swapping
// from an earlier one (temporaryOn).
type resizeRun struct {
	*executor
	row         apischema.SwapFileRow
	id          fileID
	length      uint64
	options     string
	temporary   string
	tempID      fileID
	tempSize    uint64
	temporaryOn bool
	makeTemp    bool
	// leftover is a temporary swap file an earlier resize left off, with
	// leftoverID its identity, which the run deletes before it starts.
	leftover   bool
	leftoverID fileID
	steps      *stepReporter
}

// planResize reads what the resize acts on and decides its refusals before
// anything changes: fstab options swapon would not take, which would leave
// the file off once it is turned off, and something at the temporary file's
// name that is not one. A temporary file beside the row that does not swap,
// as a crash leaves it, is deleted when the run starts; one that swaps
// stands in for the file, so no other is made, and is removed at the end.
func (e *executor) planResize(view swapView, row apischema.SwapFileRow) (*resizeRun, error) {
	run := &resizeRun{executor: e, row: row, options: fstabOptions(view.fstab, row.Path), temporary: row.Path + resizeSuffix}
	if _, err := swapOnFlags(row.Path, run.options); err != nil {
		return nil, err
	}
	info, err := os.Lstat(e.root.path(row.Path))
	if err != nil {
		return nil, err
	}
	run.length = uint64(info.Size())
	if run.id, err = e.root.fileIDOf(row.Path); err != nil {
		return nil, err
	}
	if temp, ok := findRow(view.status, run.temporary); ok && temp.Active {
		if temp.LockedReason != "" {
			return nil, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, lockedRefusal(temp))
		}
		run.temporaryOn = true
		if run.tempID, err = e.root.fileIDOf(run.temporary); err != nil {
			return nil, err
		}
	} else {
		if run.leftoverID, run.leftover, err = e.leftoverTemporary(run.temporary); err != nil {
			return nil, err
		}
		if resizeNeedsTemporary(view.status, row.Path) {
			run.makeTemp, run.tempSize = true, resizeTemporarySize(row.Used, run.length)
		}
	}
	run.steps = &stepReporter{e: e, total: 1}
	if run.makeTemp {
		run.steps.total++
	}
	if run.makeTemp || run.temporaryOn {
		run.steps.total++
	}
	return run, nil
}

// deleteLeftoverTemporary deletes a temporary swap file a resize left beside
// its file that does not swap (leftoverTemporary), and refuses anything
// else at that name.
func (e *executor) deleteLeftoverTemporary(ctx context.Context, path string) error {
	id, ok, err := e.leftoverTemporary(path)
	if err != nil || !ok {
		return err
	}
	return e.deleteLeftover(ctx, path, id)
}

// leftoverTemporary reports, with its identity, a temporary swap file a
// resize left at path that does not swap: a regular file root owns that
// holds a swap signature. Anything else at that name, which an admin or a
// root service may have put there, is refused, so that it is never deleted.
func (e *executor) leftoverTemporary(path string) (fileID, bool, error) {
	info, err := os.Lstat(e.root.path(path))
	if errors.Is(err, fs.ErrNotExist) {
		return fileID{}, false, nil
	}
	if err != nil {
		return fileID{}, false, err
	}
	notOne := fmt.Errorf("%w: %s is in the way of the temporary swap file and is not one", bridgeipc.ErrConflict, path)
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || st.Uid != e.root.owner {
		return fileID{}, false, notOne
	}
	swap, err := e.root.holdsSwap(path)
	if err != nil {
		return fileID{}, false, err
	}
	if !swap {
		return fileID{}, false, notOne
	}
	id, err := e.root.fileIDOf(path)
	return id, err == nil, err
}

// deleteLeftover deletes the leftover temporary swap file at path while it
// is still the file id names.
func (e *executor) deleteLeftover(ctx context.Context, path string, id fileID) error {
	if err := e.sys.deleteSwapFile(ctx, path, id); err != nil {
		return fmt.Errorf("delete the temporary swap file %s an earlier resize left: %w", path, err)
	}
	slog.Info("leftover temporary swap file deleted", "path", path)
	return nil
}

// do runs the resize: the temporary file when needed, the file off, its new
// size, the file on, and the temporary file off and deleted. What each
// failure leaves follows the rule that added swap is never undone: a
// temporary file that is the host's only swap stays on.
func (r *resizeRun) do(ctx context.Context, size uint64) error {
	if r.leftover {
		if err := r.deleteLeftover(ctx, r.temporary, r.leftoverID); err != nil {
			return err
		}
	}
	if r.makeTemp {
		r.steps.start(apischema.SwapStepMakeTemporarySwap, r.temporary)
		if err := r.makeTemporary(ctx); err != nil {
			return err
		}
	}
	r.steps.start(apischema.SwapStepResizeSwapFile, r.row.Path)
	off, err := r.turnOff(ctx, r.row.Path)
	if err == nil && !off {
		// A file that went off since the status was read is not resized.
		err = fmt.Errorf("%w: %s does not swap, and LinuxIO resizes only a swap file that does", bridgeipc.ErrConflict, r.row.Path)
	}
	if err != nil {
		cleanup, cancel := r.cleanupContext(ctx)
		defer cancel()
		return errors.Join(err, r.removeTemporary(cleanup))
	}
	// The file is off: bringing it back is not cancelled with the request,
	// and the turn-on has a bound of its own, whatever the resize took.
	back, cancel := r.cleanupContext(ctx)
	resizeErr := r.sys.resizeSwapFile(back, r.row.Path, r.id, size)
	cancel()
	on, cancelOn := r.cleanupContext(ctx)
	defer cancelOn()
	// The file is turned on after every failure too: one left at its old
	// length, or grown with a mkswap that failed, still has its old header
	// and swaps at its old size, and swapon refuses a file whose header no
	// longer fits it without changing anything.
	if err := r.turnOn(on, r.row.Path, r.options); err != nil {
		return errors.Join(resizeErr, fmt.Errorf("%s is off: %w%s", r.row.Path, err, r.swapsOnTemporary()))
	}
	if resizeErr == nil {
		slog.Info("swap file resized", "path", r.row.Path, "size", size, "previous_size", r.length)
	}
	// Taking the temporary file away is a swapoff like any other: the
	// request can cancel it, and the file then stays on.
	if r.makeTemp || r.temporaryOn {
		r.steps.start(apischema.SwapStepRemoveTemporarySwap, r.temporary)
		if err := r.removeTemporary(ctx); err != nil {
			if resizeErr == nil {
				return fmt.Errorf("%s was resized, and the temporary swap file %s stays on: %w", r.row.Path, r.temporary, err)
			}
			resizeErr = errors.Join(resizeErr, err)
		}
	}
	if resizeErr != nil {
		return fmt.Errorf("%s swaps at its old size: %w", r.row.Path, resizeErr)
	}
	return nil
}

// swapsOnTemporary says, for an error that leaves the file off, that the
// host swaps on the temporary file, when there is one.
func (r *resizeRun) swapsOnTemporary() string {
	if !r.makeTemp && !r.temporaryOn {
		return ""
	}
	return "; the host swaps on the temporary swap file " + r.temporary
}

// makeTemporary makes the temporary swap file and turns it on. One that
// does not come on is deleted again: nothing changed.
func (r *resizeRun) makeTemporary(ctx context.Context) error {
	id, err := r.sys.makeSwapFile(ctx, r.temporary, r.tempSize)
	if err != nil {
		return fmt.Errorf("make the temporary swap file %s: %w", r.temporary, err)
	}
	r.tempID = id
	if err := r.turnOn(ctx, r.temporary, ""); err != nil {
		cleanup, cancel := r.cleanupContext(ctx)
		defer cancel()
		if delErr := r.sys.deleteSwapFile(cleanup, r.temporary, id); delErr != nil {
			err = errors.Join(err, fmt.Errorf("delete %s: %w", r.temporary, delErr))
		}
		return err
	}
	slog.Info("temporary swap file added", "path", r.temporary, "size", r.tempSize, "for", r.row.Path)
	return nil
}

// removeTemporary turns the temporary swap file off, after its own room
// check, and deletes it. Without one it does nothing.
func (r *resizeRun) removeTemporary(ctx context.Context) error {
	if !r.makeTemp && !r.temporaryOn {
		return nil
	}
	if _, err := r.turnOff(ctx, r.temporary); err != nil {
		return err
	}
	if err := r.sys.deleteSwapFile(ctx, r.temporary, r.tempID); err != nil {
		return fmt.Errorf("delete %s: %w", r.temporary, err)
	}
	slog.Info("temporary swap file removed", "path", r.temporary)
	return nil
}

func (e *executor) removeSwapFile(ctx context.Context, req apischema.SwapFilePathRequest) (apischema.SwapStatus, error) {
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	row, err := selectRow(view.status, req.Path)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	steps := &stepReporter{e: e, total: 1}
	steps.start(apischema.SwapStepRemoveSwapFile, row.Path)
	if err := e.removeRow(ctx, view.status, row); err != nil {
		return apischema.SwapStatus{}, err
	}
	return e.statusAfterChange(ctx)
}

// removeRow removes the swap file row of status, which passes the strict
// check: it turns the file off after the room check, removes its fstab
// lines, and deletes it, in that order, so a line never names a missing
// file. A temporary swap file has no line. A file that is already off skips
// the first, and one that is missing the last. A temporary file beside the
// row that does not swap is deleted first; one that swaps is refused, since
// without the row it stood in for it would read as not in fstab.
func (e *executor) removeRow(ctx context.Context, status apischema.SwapStatus, row apischema.SwapFileRow) error {
	id, err := e.root.fileIDOf(row.Path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	if !row.Temporary {
		temporary := row.Path + resizeSuffix
		if temp, ok := findRow(status, temporary); ok && temp.Active {
			return fmt.Errorf("%w: the temporary swap file %s swaps beside %s; remove it first", bridgeipc.ErrConflict, temporary, row.Path)
		}
		if err := e.deleteLeftoverTemporary(ctx, temporary); err != nil {
			return err
		}
	}
	if _, err := e.turnOff(ctx, row.Path); err != nil {
		return err
	}
	// The file is off: removing it is not cancelled with the request.
	ctx, cancel := e.cleanupContext(ctx)
	defer cancel()
	if !row.Temporary {
		if err := e.removeFstabLines(ctx, row.Path); err != nil {
			return fmt.Errorf("%s is off: %w", row.Path, err)
		}
		// Another program can rewrite /etc/fstab without the lock: a line
		// that came back would name a missing file at every boot, so the
		// file is kept.
		fstab, err := e.root.fstabSwaps()
		if err != nil {
			return fmt.Errorf("%s is off and kept: read /etc/fstab again: %w", row.Path, err)
		}
		if fstabIndex(fstab, row.Path) >= 0 {
			return fmt.Errorf("%w: %s is off and kept: a line naming it came back to /etc/fstab after LinuxIO removed it, as another program rewrote the file", bridgeipc.ErrConflict, row.Path)
		}
	}
	if !missing {
		if err := e.sys.deleteSwapFile(ctx, row.Path, id); err != nil {
			return fmt.Errorf("%s is off and has no /etc/fstab line: delete it: %w", row.Path, err)
		}
	}
	slog.Info("swap file removed", "path", row.Path, "size", row.Size, "was_active", row.Active)
	return nil
}

func (e *executor) moveSwapFile(ctx context.Context, req apischema.SwapFilePathRequest) (apischema.SwapStatus, error) {
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	row, err := selectRow(view.status, req.Path)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	if !row.CanMove {
		return apischema.SwapStatus{}, fmt.Errorf("%w: LinuxIO moves only an active swap file on btrfs, on the filesystem of %s, while %s is free and has room for it", bridgeipc.ErrConflict, ownSwapDir, ownSwapFilePath)
	}
	info, err := os.Lstat(e.root.path(row.Path))
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	steps := &stepReporter{e: e, total: 2}
	steps.start(apischema.SwapStepCreateSwapFile, ownSwapFilePath)
	if err := e.createSwapFile(ctx, view.status, moveSize(uint64(info.Size()))); err != nil {
		return apischema.SwapStatus{}, err
	}
	steps.start(apischema.SwapStepRemoveSwapFile, row.Path)
	if err := e.removeMoved(ctx, row.Path); err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("%s swaps, and %s was not removed: %w", ownSwapFilePath, row.Path, err)
	}
	return e.statusAfterChange(ctx)
}

// removeMoved removes the file Move made LinuxIO's file in place of, as a
// fresh status shows it; one that has gone meanwhile is left.
func (e *executor) removeMoved(ctx context.Context, path string) error {
	status, _, err := e.sys.status(ctx)
	if err != nil {
		return err
	}
	row, ok := findRow(status, path)
	if !ok {
		return nil
	}
	if row.LockedReason != "" {
		return fmt.Errorf("%w: %s", bridgeipc.ErrConflict, lockedRefusal(row))
	}
	return e.removeRow(ctx, status, row)
}
