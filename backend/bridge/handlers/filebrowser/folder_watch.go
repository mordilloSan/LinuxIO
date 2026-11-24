package filebrowser

import (
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// routeFolderWatch tells the page that shows a folder when that folder's
// entries change, so its listing updates without being asked. It is one
// inotify watch on the one folder shown, never recursive, in the bridge's one
// instance, held for as long as the page keeps the Channel open. See docs/filebrowser-live-listing.md.
const routeFolderWatch = "filebrowser.watch"

// maxFolderWatches is how many folders one bridge watches at a time.
const maxFolderWatches = 32

// The pacing of change frames: the first change after a quiet time is sent
// at once, then at most one frame a gap; the gap doubles up to the longest
// while changes keep coming without a quiet shortest gap. Variables so tests
// can shorten them.
var (
	folderWatchMinGap = time.Second
	folderWatchMaxGap = 10 * time.Second
)

// folderWatchMask is what the watch reports: entries created, deleted,
// renamed in or out, closed after a write or with new attributes, and the
// folder itself deleted or moved. Not IN_MODIFY: a file being appended to
// would fire it at every write; its new size shows when the writer closes it.
const folderWatchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
	unix.IN_CLOSE_WRITE | unix.IN_ATTRIB | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

var folderWatches = &watchSlots{limit: maxFolderWatches}

// watchSlots counts the folder watches a bridge holds.
type watchSlots struct {
	mu    sync.Mutex
	used  int
	limit int
}

// acquire takes a slot, or refuses when every slot is taken. The release it
// returns gives the slot back once.
func (s *watchSlots) acquire() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used >= s.limit {
		return nil, fmt.Errorf("%w: %d folders are already watched", bridgeipc.ErrConflict, s.limit)
	}
	s.used++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.used--
			s.mu.Unlock()
		})
	}, nil
}

type folderWatchFrame struct {
	Type string `json:"type"`
}

// streamFolderWatch is the Channel: it watches the folder req names until the
// page closes the Channel or the folder is gone.
func streamFolderWatch(parent context.Context, stream net.Conn, req apischema.PathRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()

	release, err := folderWatches.acquire()
	if err != nil {
		return streamrelay.WriteError(stream, 0, err)
	}
	defer release()
	watch, err := openFolderWatch(ctx, req.Path)
	if err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, err)
	}
	defer watch.Close()
	slog.Debug("started folder watch", "component", "filebrowser", "route", routeFolderWatch, "path", watch.path)
	err = serveFolderWatch(ctx, stream, watch)
	slog.Debug("ended folder watch", "component", "filebrowser", "route", routeFolderWatch, "path", watch.path)
	return err
}

// folderWatch is one Channel's watch of one folder: a watch descriptor of
// the bridge's inotify instance, which other Channels on the same folder
// share, and the signals the instance's reader leaves for the Channel.
type folderWatch struct {
	path    string
	wd      int32
	changed chan struct{}    // a change not yet reported; holds at most one
	ended   chan folderEnded // the one last word: gone, or the reader failed
	over    bool             // ended was sent; guarded by folderWatcher.mu
	removed bool             // Close ran; guarded by folderWatcher.life
}

type folderEnded struct {
	gone bool
	err  error
}

// errFolderWatcherFailed ends every watch when the instance cannot be read.
// The Channels close without an error result, so the pages open them again.
var errFolderWatcherFailed = errors.New("folder watcher failed")

// openFolderWatch opens the folder at path as the listing opens it, refusing
// what the listing refuses, and puts the watch on that open folder through
// /proc/self/fd, so what is watched is the folder that was resolved and no
// link is followed a second time.
func openFolderWatch(ctx context.Context, path string) (*folderWatch, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}
	dir, realPath, err := services.OpenDirectory(ctx, path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fsError("watch directory", err)
	}
	// The folder is closed once the watch is on it: an open handle would
	// hold off IN_DELETE_SELF until it is closed.
	defer dir.Close()

	watch := &folderWatch{
		path:    realPath,
		changed: make(chan struct{}, 1),
		ended:   make(chan folderEnded, 1),
	}
	if err := folderWatcher.add(watch, dir); err != nil {
		return nil, folderWatchError(err)
	}
	return watch, nil
}

// Close ends the watch, and with the bridge's last watch its inotify
// instance and reader. A second Close does nothing.
func (w *folderWatch) Close() {
	folderWatcher.remove(w)
}

// folderWatcher is the bridge's one inotify instance, shared by every folder
// watch: instances are few per user (fs.inotify.max_user_instances, shared by
// every process of the uid, root's on a session with sudo), watches many.
var folderWatcher = &watchHolder{}

// watchHolder holds the instance while anything is watched and nothing
// otherwise. Its reader hands each event to the watches of its descriptor
// without ever waiting on one, so a Channel that is slow to write delays no
// other.
type watchHolder struct {
	// life orders adding and removing watches, so the instance of the last
	// watch is closed, and its reader has returned, before another is made.
	life sync.Mutex

	// mu guards what the reader also touches.
	mu         sync.Mutex
	events     *os.File // nil while nothing is watched
	fd         int
	readerDone chan struct{}
	failed     bool // the reader stopped on an error; the next add replaces the instance
	entries    map[int32]*watchEntry
	held       int // watches added and not yet removed
}

// watchEntry is one kernel watch and the Channels on it. A dead entry's watch
// is gone from the kernel, or going, and its IN_IGNORED is still to be read.
type watchEntry struct {
	watches map[*folderWatch]struct{}
	dead    bool
	// staleIgnored counts IN_IGNORED events still to come for an earlier
	// watch the kernel gave this descriptor's number to. (An IN_IGNORED lost
	// to a full queue would make the entry skip a later one, which matters
	// only after the folder's own delete or move already said gone.)
	staleIgnored int
}

func (h *watchHolder) add(w *folderWatch, dir *os.File) error {
	h.life.Lock()
	defer h.life.Unlock()
	// The reader can fail at any time; whether it has is read under h.mu
	// together with the registration, so a watch never joins a failed
	// instance, whose reader is gone. A failed instance is replaced, once.
	for range 2 {
		h.mu.Lock()
		if h.events != nil && !h.failed {
			err := h.addLocked(w, dir)
			last := h.held == 0
			h.mu.Unlock()
			if err != nil && last {
				h.closeInstance()
			}
			return err
		}
		stale := h.events != nil
		h.mu.Unlock()
		if stale {
			h.closeInstance()
		}
		if err := h.openInstance(); err != nil {
			return err
		}
	}
	// The instance failed again as soon as it was replaced. The one just
	// opened is not left behind with nothing on it.
	h.mu.Lock()
	unused := h.events != nil && h.held == 0
	h.mu.Unlock()
	if unused {
		h.closeInstance()
	}
	return errFolderWatcherFailed
}

// addLocked adds the kernel watch and registers w; h.mu is held.
func (h *watchHolder) addLocked(w *folderWatch, dir *os.File) error {
	wd, err := addFolderWatch(h.fd, dir)
	if err != nil {
		return err
	}
	h.register(wd, w)
	h.held++
	return nil
}

// register puts w on the entry of wd; h.mu is held.
func (h *watchHolder) register(wd int32, w *folderWatch) {
	entry := h.entries[wd]
	switch {
	case entry == nil:
		entry = &watchEntry{watches: map[*folderWatch]struct{}{}}
		h.entries[wd] = entry
	case entry.dead:
		// The kernel reused the number of a watch that ended; that watch's
		// IN_IGNORED was queued before the number was free.
		entry.dead = false
		entry.staleIgnored++
	}
	entry.watches[w] = struct{}{}
	w.wd = wd
}

func (h *watchHolder) remove(w *folderWatch) {
	h.life.Lock()
	defer h.life.Unlock()
	if w.removed {
		return
	}
	w.removed = true
	h.mu.Lock()
	// An entry without w is a later watch that reuses the number, or the
	// watch already ended; neither is w's to remove.
	if entry := h.entries[w.wd]; entry != nil {
		if _, ok := entry.watches[w]; ok {
			delete(entry.watches, w)
			if len(entry.watches) == 0 && !entry.dead {
				// Its IN_IGNORED drops the entry.
				_, _ = unix.InotifyRmWatch(h.fd, uint32(w.wd))
				entry.dead = true
			}
		}
	}
	h.held--
	last := h.held == 0 && h.events != nil
	h.mu.Unlock()
	if last {
		h.closeInstance()
	}
}

// openInstance creates the instance and starts its reader; h.life is held.
func (h *watchHolder) openInstance() error {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return fmt.Errorf("create inotify instance: %w", err)
	}
	// Non-blocking, so reads go through the runtime poller and Close ends a
	// read in progress.
	events := os.NewFile(uintptr(fd), "inotify")
	done := make(chan struct{})
	h.mu.Lock()
	h.events, h.fd, h.readerDone = events, fd, done
	h.failed = false
	h.entries = map[int32]*watchEntry{}
	h.mu.Unlock()
	go h.read(events, done)
	return nil
}

// closeInstance closes the instance, which drops its watches and ends its
// reader's read, and returns once the reader has; h.life is held, h.mu not.
func (h *watchHolder) closeInstance() {
	h.mu.Lock()
	events, done := h.events, h.readerDone
	h.events, h.entries, h.failed = nil, nil, false
	h.mu.Unlock()
	_ = events.Close()
	<-done
}

func addFolderWatch(inotifyFD int, dir *os.File) (int32, error) {
	conn, err := dir.SyscallConn()
	if err != nil {
		return 0, err
	}
	var wd int
	var addErr error
	err = conn.Control(func(fd uintptr) {
		wd, addErr = unix.InotifyAddWatch(inotifyFD, "/proc/self/fd/"+strconv.FormatUint(uint64(fd), 10), folderWatchMask)
	})
	if err != nil {
		return 0, err
	}
	if addErr != nil {
		return 0, &os.PathError{Op: "inotify_add_watch", Path: dir.Name(), Err: addErr}
	}
	return int32(wd), nil
}

// read hands the instance's events to the watches. It returns when the
// instance is closed, or ends every watch when it cannot be read.
func (h *watchHolder) read(events *os.File, done chan struct{}) {
	defer close(done)
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		n, err := events.Read(buf)
		if err != nil {
			if !errors.Is(err, os.ErrClosed) {
				h.fail(fmt.Errorf("%w: %w", errFolderWatcherFailed, err))
			}
			return
		}
		h.dispatch(buf[:n])
	}
}

// dispatch hands one read's events to the watches of their descriptors.
// Each event is struct inotify_event: wd, mask, cookie and the length of the
// name that follows it.
func (h *watchHolder) dispatch(buf []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for offset := 0; offset+unix.SizeofInotifyEvent <= len(buf); {
		wd := int32(binary.NativeEndian.Uint32(buf[offset:]))
		mask := binary.NativeEndian.Uint32(buf[offset+4:])
		offset += unix.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(buf[offset+12:]))
		if mask&unix.IN_Q_OVERFLOW != 0 {
			// Events were lost: a change for every watch.
			for _, entry := range h.entries {
				entry.notifyChanged()
			}
			continue
		}
		if entry := h.entries[wd]; entry != nil {
			h.dispatchEvent(wd, entry, mask)
		}
	}
}

// dispatchEvent applies one event to its entry; h.mu is held.
func (h *watchHolder) dispatchEvent(wd int32, entry *watchEntry, mask uint32) {
	switch {
	case mask&unix.IN_IGNORED != 0:
		if entry.staleIgnored > 0 {
			entry.staleIgnored--
			return
		}
		// The kernel dropped the watch; for a live entry that is the folder
		// gone where the page shows it.
		entry.end(folderEnded{gone: true})
		delete(h.entries, wd)
	case mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_UNMOUNT) != 0:
		if entry.dead {
			return
		}
		entry.end(folderEnded{gone: true})
		entry.dead = true
		if mask&unix.IN_MOVE_SELF != 0 {
			// A moved folder keeps its watch; its IN_IGNORED drops the entry.
			_, _ = unix.InotifyRmWatch(h.fd, uint32(wd))
		}
	case mask&folderWatchMask != 0 && !entry.dead:
		entry.notifyChanged()
	}
}

// fail ends every watch with err; the next add makes a new instance.
func (h *watchHolder) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, entry := range h.entries {
		entry.end(folderEnded{err: err})
	}
	h.entries = map[int32]*watchEntry{}
	h.failed = true
}

// notifyChanged leaves a change for every watch of the entry, without
// waiting: one already left covers this one.
func (e *watchEntry) notifyChanged() {
	for w := range e.watches {
		select {
		case w.changed <- struct{}{}:
		default:
		}
	}
}

// end gives every watch of the entry its last word, once, and takes them
// off the entry.
func (e *watchEntry) end(ended folderEnded) {
	for w := range e.watches {
		if !w.over {
			w.over = true
			w.ended <- ended // one slot, sent once: never waits
		}
	}
	e.watches = map[*folderWatch]struct{}{}
}

// serveFolderWatch sends the watch's changes, paced, until ctx ends or the
// folder is gone. Writes are synchronous; a change that comes while a frame
// waits for its gap is reported by that frame.
func serveFolderWatch(ctx context.Context, stream net.Conn, watch *folderWatch) error {
	// The page reads the listing again after a watch it lost only once the
	// new one is on: a refused watch sends no "watching".
	gone, err := false, writeFolderWatchFrame(stream, "watching")
	if err == nil {
		gone, err = forwardFolderChanges(ctx, stream, watch)
	}
	switch {
	case gone:
		if writeErr := writeFolderWatchFrame(stream, "gone"); writeErr != nil {
			return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, writeErr)
		}
		return relay.WriteResultOKAndClose(stream, 0, nil)
	case streamrelay.Canceled(ctx, err):
		_ = relay.WriteStreamClose(stream, 0)
		return nil
	case errors.Is(err, errFolderWatcherFailed):
		// Closed without an error result, so the page opens it again; the
		// router logs the failure.
		_ = relay.WriteStreamClose(stream, 0)
		return err
	default:
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, err)
	}
}

// writeFolderWatchFrame sends one JSON line as a data frame, as the log
// Channels send theirs. Not a progress frame: the page counts progress as the
// user's activity, which would keep an idle session alive while a watched
// folder changes.
func writeFolderWatchFrame(stream net.Conn, kind string) error {
	line, err := json.Marshal(folderWatchFrame{Type: kind})
	if err != nil {
		return fmt.Errorf("marshal folder watch frame: %w", err)
	}
	return streamrelay.WriteData(stream, append(line, '\n'))
}

func forwardFolderChanges(ctx context.Context, stream net.Conn, watch *folderWatch) (bool, error) {
	pacer := newChangePacer(folderWatchMinGap, folderWatchMaxGap)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var due <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case end := <-watch.ended:
			return end.gone, end.err
		case <-watch.changed:
			send, wait := pacer.noteChange(time.Now())
			if wait > 0 {
				timer.Reset(wait)
				due = timer.C
			}
			if !send {
				continue
			}
		case <-due:
			due = nil
			pacer.fired(time.Now())
		}
		if err := writeFolderWatchFrame(stream, "changed"); err != nil {
			return false, err
		}
	}
}

// changePacer decides when a change is reported. A change that comes when at
// least twice the gap has passed since the last frame is sent at once and
// sets the gap back to minGap, so an occasional change shows at once. Any
// other change is sent at once if the gap has passed, else when it ends, and
// every frame sent so doubles the gap, up to maxGap: a folder that changes
// steadily, even every few seconds, settles at one frame a maxGap. A change
// inside a gap is reported when the gap ends.
type changePacer struct {
	minGap, maxGap time.Duration
	gap            time.Duration
	lastSent       time.Time
	pending        bool
}

func newChangePacer(minGap, maxGap time.Duration) *changePacer {
	return &changePacer{minGap: minGap, maxGap: maxGap, gap: minGap}
}

// noteChange records a change at now. It says to send a frame at once, or
// how long to wait before the frame that reports it; neither when a frame is
// already due.
func (p *changePacer) noteChange(now time.Time) (bool, time.Duration) {
	if p.pending {
		return false, 0
	}
	if p.lastSent.IsZero() || now.Sub(p.lastSent) >= 2*p.gap {
		p.gap = p.minGap
		p.lastSent = now
		return true, 0
	}
	due := p.lastSent.Add(p.gap)
	if now.Before(due) {
		p.pending = true
		return false, due.Sub(now)
	}
	p.sent(now)
	return true, 0
}

// fired records the frame sent when a gap ended.
func (p *changePacer) fired(now time.Time) {
	p.pending = false
	p.sent(now)
}

// sent records a frame sent while changes keep coming.
func (p *changePacer) sent(now time.Time) {
	p.lastSent = now
	p.gap = min(p.gap*2, p.maxGap)
}

// folderWatchError maps a failure to put the watch on. An inotify limit is a
// refusal asking again will not change, as the cap is, so the page stops
// instead of retrying, reading the listing and logging an error each time.
func folderWatchError(err error) error {
	var limit string
	switch {
	case errors.Is(err, unix.ENOSPC):
		limit = "the inotify watch limit (fs.inotify.max_user_watches)"
	case errors.Is(err, unix.EMFILE):
		limit = "the inotify instance limit (fs.inotify.max_user_instances) or the open file limit"
	case errors.Is(err, unix.ENFILE):
		limit = "the system's limit of open files"
	default:
		return fsError("watch directory", err)
	}
	return fmt.Errorf("%w: %s is reached: %w", bridgeipc.ErrConflict, limit, err)
}
