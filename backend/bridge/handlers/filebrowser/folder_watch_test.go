package filebrowser

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// fastFolderWatchGaps shortens the pacing of change frames for a test.
func fastFolderWatchGaps(t *testing.T, minGap, maxGap time.Duration) {
	t.Helper()
	oldMin, oldMax := folderWatchMinGap, folderWatchMaxGap
	folderWatchMinGap, folderWatchMaxGap = minGap, maxGap
	t.Cleanup(func() { folderWatchMinGap, folderWatchMaxGap = oldMin, oldMax })
}

// watchedFolder is a folder under watch: the watch is in place when it is
// returned, and the page's end of the Channel is client.
type watchedFolder struct {
	client   net.Conn
	cancel   context.CancelFunc
	done     chan error
	finished chan struct{} // closed once the watch is closed too
	watch    *folderWatch
}

// watchFolder starts a Channel on path and reads its first frame, which says
// the watch is on.
func watchFolder(t *testing.T, path string) *watchedFolder {
	t.Helper()
	w := startFolderWatch(t, path)
	expectWatchFrame(t, w.client, "watching")
	return w
}

// startFolderWatch starts a Channel on path and reads nothing.
func startFolderWatch(t *testing.T, path string) *watchedFolder {
	t.Helper()
	watch, err := openFolderWatch(t.Context(), path)
	require.NoError(t, err)
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	w := &watchedFolder{client: client, cancel: cancel, done: make(chan error, 1), finished: make(chan struct{}), watch: watch}
	go func() {
		defer close(w.finished)
		w.done <- serveFolderWatch(ctx, server, watch)
		watch.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_ = client.Close()
		_ = server.Close()
		<-w.finished
	})
	return w
}

// end ends the Channel as the page closing it would, reading what the bridge
// writes as it ends, and waits for its watch to be closed.
func (w *watchedFolder) end(t *testing.T) {
	t.Helper()
	go func() {
		for {
			if _, err := relay.ReadRelayFrame(w.client); err != nil {
				return
			}
		}
	}()
	w.cancel()
	select {
	case <-w.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the Channel did not end")
	}
}

// expectGone reads frames until "gone", then the ok result and the close.
func expectGone(t *testing.T, client net.Conn) {
	t.Helper()
	for {
		opcode, kind := nextWatchFrame(t, client, 2*time.Second)
		if kind == "changed" {
			continue
		}
		require.Equal(t, relay.OpStreamData, opcode)
		require.Equal(t, "gone", kind)
		break
	}
	opcode, _ := nextWatchFrame(t, client, time.Second)
	require.Equal(t, relay.OpStreamResult, opcode)
	opcode, _ = nextWatchFrame(t, client, time.Second)
	require.Equal(t, relay.OpStreamClose, opcode)
}

// watcherState reports whether the bridge holds an inotify instance, and
// the done channel of its reader.
func watcherState() (bool, chan struct{}) {
	folderWatcher.mu.Lock()
	defer folderWatcher.mu.Unlock()
	return folderWatcher.events != nil, folderWatcher.readerDone
}

func requireNoWatcher(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { open, _ := watcherState(); return !open }, 2*time.Second, 10*time.Millisecond, "the bridge still holds an inotify instance")
}

// nextWatchFrame reads the next frame within limit and returns its opcode and,
// for a data frame, the type of the one JSON line it carries.
func nextWatchFrame(t *testing.T, client net.Conn, limit time.Duration) (byte, string) {
	t.Helper()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(limit)))
	defer func() { _ = client.SetReadDeadline(time.Time{}) }()
	frame, err := relay.ReadRelayFrame(client)
	require.NoError(t, err, "no frame within %s", limit)
	if frame.Opcode != relay.OpStreamData {
		return frame.Opcode, ""
	}
	require.True(t, bytes.HasSuffix(frame.Payload, []byte("\n")), "one line: %q", frame.Payload)
	var message struct {
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(bytes.TrimSuffix(frame.Payload, []byte("\n")), &message))
	return frame.Opcode, message.Type
}

// expectWatchFrame reads a data frame of the given type. Watch frames are
// data, not progress: progress counts as the user's activity on the page.
func expectWatchFrame(t *testing.T, client net.Conn, want string) {
	t.Helper()
	opcode, kind := nextWatchFrame(t, client, 2*time.Second)
	require.Equal(t, relay.OpStreamData, opcode)
	require.Equal(t, want, kind)
}

func expectNoWatchFrame(t *testing.T, client net.Conn, quiet time.Duration) {
	t.Helper()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(quiet)))
	defer func() { _ = client.SetReadDeadline(time.Time{}) }()
	frame, err := relay.ReadRelayFrame(client)
	if err == nil {
		t.Fatalf("got a frame (opcode 0x%02x, %s), want none", frame.Opcode, frame.Payload)
	}
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "read: %v", err)
}

func TestFolderWatchReportsChangesToTheFolderEntries(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	elsewhere := t.TempDir()
	cases := map[string]func(t *testing.T, dir string){
		"create": func(t *testing.T, dir string) { seedFile(t, filepath.Join(dir, "new.txt"), "x") },
		"delete": func(t *testing.T, dir string) { require.NoError(t, os.Remove(filepath.Join(dir, "old.txt"))) },
		"rename in": func(t *testing.T, dir string) {
			from := seedFile(t, filepath.Join(elsewhere, "in.txt"), "x")
			require.NoError(t, os.Rename(from, filepath.Join(dir, "in.txt")))
		},
		"rename out": func(t *testing.T, dir string) {
			require.NoError(t, os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(elsewhere, "out.txt")))
		},
		"chmod": func(t *testing.T, dir string) { require.NoError(t, os.Chmod(filepath.Join(dir, "old.txt"), 0o600)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			seedFile(t, filepath.Join(dir, "old.txt"), "old")
			w := watchFolder(t, dir)
			change(t, dir)
			expectWatchFrame(t, w.client, "changed")
		})
	}
}

func TestFolderWatchIgnoresSubfoldersAndOpenAppends(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	log := seedFile(t, filepath.Join(dir, "app.log"), "start\n")
	w := watchFolder(t, dir)

	seedFile(t, filepath.Join(sub, "deep.txt"), "x")
	expectNoWatchFrame(t, w.client, 200*time.Millisecond)

	file, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	for range 5 {
		_, err = file.WriteString("line\n")
		require.NoError(t, err)
	}
	expectNoWatchFrame(t, w.client, 200*time.Millisecond)
	require.NoError(t, file.Close())
	expectWatchFrame(t, w.client, "changed")
}

// A burst is one frame at once and one more when the gap ends, not a frame
// a change. The changes are left as the reader leaves them, so the test does
// not hang on how fast the machine makes files.
func TestFolderWatchCoalescesABurst(t *testing.T) {
	fastFolderWatchGaps(t, 150*time.Millisecond, time.Second)
	watch := &folderWatch{changed: make(chan struct{}, 1), ended: make(chan folderEnded, 1)}
	signal := func() {
		select {
		case watch.changed <- struct{}{}:
		default:
		}
	}
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = serveFolderWatch(ctx, server, watch)
	}()
	t.Cleanup(func() {
		cancel()
		_ = client.Close()
		_ = server.Close()
		<-finished
	})

	expectWatchFrame(t, client, "watching")
	signal()
	expectWatchFrame(t, client, "changed")
	start := time.Now()
	for range 20 {
		signal()
	}
	expectWatchFrame(t, client, "changed")
	require.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond, "the second frame waits for the gap")
	expectNoWatchFrame(t, client, 600*time.Millisecond)
}

// pacerBase is the time 0 of the pacer tests; pacerAt is n ms after it.
var pacerBase = time.Unix(1_000_000, 0)

func pacerAt(n int) time.Time { return pacerBase.Add(time.Duration(n) * time.Millisecond) }

// everyMs is the times from, from+step, ... up to to, in ms.
func everyMs(from, step, to int) []int {
	var times []int
	for now := from; now <= to; now += step {
		times = append(times, now)
	}
	return times
}

// pacedFrames runs a pacer over changes at the given milliseconds, firing a
// due frame before a change at or after its time, and returns when the frames
// went out. Every change has a frame at or after it, and every frame a change
// since the one before.
func pacedFrames(t *testing.T, pacer *changePacer, changes []int) []int {
	t.Helper()
	var frames []int
	var due time.Time
	fire := func() {
		pacer.fired(due)
		frames = append(frames, int(due.Sub(pacerBase)/time.Millisecond))
		due = time.Time{}
	}
	for _, change := range changes {
		if !due.IsZero() && !pacerAt(change).Before(due) {
			fire()
		}
		send, wait := pacer.noteChange(pacerAt(change))
		if send {
			frames = append(frames, change)
		}
		if wait > 0 {
			due = pacerAt(change).Add(wait)
		}
	}
	if !due.IsZero() {
		fire()
	}
	require.GreaterOrEqual(t, frames[len(frames)-1], changes[len(changes)-1], "a change is never lost")
	requireFramesCarryChanges(t, frames, changes)
	return frames
}

func requireFramesCarryChanges(t *testing.T, frames, changes []int) {
	t.Helper()
	previous := -1
	for _, frame := range frames {
		news := false
		for _, change := range changes {
			news = news || (change > previous && change <= frame)
		}
		require.True(t, news, "the frame at %d ms has a change since the one before", frame)
		previous = frame
	}
}

func TestChangePacer(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	at := pacerAt
	paced := func(pacer *changePacer, changes []int) []int { return pacedFrames(t, pacer, changes) }
	every := everyMs

	// The first change goes at once, the next one a gap later.
	pacer := newChangePacer(time.Second, 10*time.Second)
	send, wait := pacer.noteChange(at(0))
	require.True(t, send, "the first change is sent at once")
	require.Zero(t, wait)
	send, wait = pacer.noteChange(at(100))
	require.False(t, send)
	require.Equal(t, ms(900), wait, "then at most one frame a second")
	send, wait = pacer.noteChange(at(200))
	require.False(t, send)
	require.Zero(t, wait, "a frame is already due")

	// Changes 200 ms apart: the gap doubles after every frame, up to 10 s.
	frames := paced(newChangePacer(time.Second, 10*time.Second), every(0, 200, 40000))
	require.Equal(t, []int{0, 1000, 3000, 7000, 15000, 25000, 35000, 45000}, frames)

	// Changes 1.5 s apart, each past the gap before it, back off all the
	// same, to one frame in 10 s.
	frames = paced(newChangePacer(time.Second, 10*time.Second), every(0, 1500, 90000))
	require.Equal(t, []int{0, 1500, 3500, 7500, 15500, 25500, 35500, 45500, 55500, 65500, 75500, 85500, 95500}, frames)

	// A change right at a gap's edge goes then.
	pacer = newChangePacer(time.Second, 10*time.Second)
	require.Equal(t, []int{0, 1000}, paced(pacer, []int{0, 1000}))

	// Changes 9 s apart are each sent at once, for ever: two gaps passed.
	nine := every(0, 9000, 180000)
	require.Equal(t, nine, paced(newChangePacer(time.Second, 10*time.Second), nine))

	// Changes 5 s apart after a burst stay at one frame in 10 s.
	pacer = newChangePacer(time.Second, 10*time.Second)
	require.Equal(t, []int{0, 1000, 3000}, paced(pacer, every(0, 100, 2900)))
	frames = paced(pacer, every(8000, 5000, 78000))
	require.Equal(t, []int{8000, 16000, 26000, 36000, 46000, 56000, 66000, 76000, 86000}, frames)

	// Twice the gap after the last frame, 20 s once it grew to 10 s, resets
	// it: the next change goes at once and the one after waits one second.
	// 19 s does not.
	long := every(0, 200, 40000) // the last frame at 45 s, the gap 10 s
	pacer = newChangePacer(time.Second, 10*time.Second)
	paced(pacer, long)
	send, _ = pacer.noteChange(at(65000))
	require.True(t, send, "the first change after twice the gap is sent at once")
	_, wait = pacer.noteChange(at(65100))
	require.Equal(t, ms(900), wait, "the gap is back to 1 s")

	pacer = newChangePacer(time.Second, 10*time.Second)
	paced(pacer, long)
	send, _ = pacer.noteChange(at(64000))
	require.True(t, send, "past the gap")
	_, wait = pacer.noteChange(at(64100))
	require.Equal(t, ms(9900), wait, "the gap stays 10 s")
}

func TestFolderWatchReportsTheFolderGoneAndEnds(t *testing.T) {
	for name, remove := range map[string]func(dir string) error{
		"deleted": os.Remove,
		"moved":   func(dir string) error { return os.Rename(dir, dir+"-moved") },
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "shown")
			require.NoError(t, os.Mkdir(dir, 0o755))
			w := watchFolder(t, dir)
			require.NoError(t, remove(dir))

			expectGone(t, w.client)
			select {
			case err := <-w.done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("the Channel did not end")
			}
		})
	}
}

// "gone" on a folder two Channels watch reaches both, and a folder made
// again under the same name is watched afresh.
func TestFolderWatchGoneReachesEveryChannelOfTheFolder(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	dir := filepath.Join(t.TempDir(), "shown")
	require.NoError(t, os.Mkdir(dir, 0o755))
	first, second := watchFolder(t, dir), watchFolder(t, dir)
	require.NoError(t, os.Remove(dir))
	expectGone(t, first.client)
	expectGone(t, second.client)
	<-first.finished
	<-second.finished

	require.NoError(t, os.Mkdir(dir, 0o755))
	again := watchFolder(t, dir)
	seedFile(t, filepath.Join(dir, "new.txt"), "x")
	expectWatchFrame(t, again.client, "changed")
}

// The bridge holds one inotify instance while anything is watched and none
// after: ending the last Channel closes it and its reader returns, and a
// later watch gets a new one.
func TestFolderWatchesShareOneInstance(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	requireNoWatcher(t)
	before := inotifyInstances(t)

	first, second := watchFolder(t, t.TempDir()), watchFolder(t, t.TempDir())
	require.Equal(t, before+1, inotifyInstances(t), "two watches, one instance")
	open, readerDone := watcherState()
	require.True(t, open)

	first.end(t)
	require.NoError(t, <-first.done)
	open, _ = watcherState()
	require.True(t, open, "the instance stays while a watch remains")
	second.end(t)
	require.NoError(t, <-second.done)

	open, _ = watcherState()
	require.False(t, open)
	select {
	case <-readerDone:
	default:
		t.Fatal("the reader goroutine did not return")
	}
	require.Equal(t, before, inotifyInstances(t))

	dir := t.TempDir()
	later := watchFolder(t, dir)
	seedFile(t, filepath.Join(dir, "new.txt"), "x")
	expectWatchFrame(t, later.client, "changed")
}

// Two tabs on one folder share its kernel watch: both hear a change, and the
// one left still hears after the other ended.
func TestFolderWatchTwoChannelsOnOneFolder(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	dir := t.TempDir()
	first, second := watchFolder(t, dir), watchFolder(t, dir)
	require.Equal(t, first.watch.wd, second.watch.wd)

	// A folder made is one event, so one frame: a file written is two.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "one"), 0o755))
	expectWatchFrame(t, first.client, "changed")
	expectWatchFrame(t, second.client, "changed")

	first.end(t)
	time.Sleep(100 * time.Millisecond) // past twice the gap
	require.NoError(t, os.Mkdir(filepath.Join(dir, "two"), 0o755))
	expectWatchFrame(t, second.client, "changed")
}

func TestFolderWatchChannelsHearOnlyTheirOwnFolder(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	dirA, dirB := t.TempDir(), t.TempDir()
	a, b := watchFolder(t, dirA), watchFolder(t, dirB)

	// A folder made is one event, so one frame: a file written is two.
	require.NoError(t, os.Mkdir(filepath.Join(dirA, "a"), 0o755))
	expectWatchFrame(t, a.client, "changed")
	expectNoWatchFrame(t, b.client, 200*time.Millisecond)

	require.NoError(t, os.Mkdir(filepath.Join(dirB, "b"), 0o755))
	expectWatchFrame(t, b.client, "changed")
	expectNoWatchFrame(t, a.client, 200*time.Millisecond)
}

// A Channel whose page reads nothing holds up no other Channel.
func TestFolderWatchAStuckChannelDelaysNoOther(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	dir := t.TempDir()
	startFolderWatch(t, dir) // never read: its first frame blocks its write
	reader := watchFolder(t, dir)

	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		seedFile(t, filepath.Join(dir, name), "x")
		expectWatchFrame(t, reader.client, "changed")
		time.Sleep(100 * time.Millisecond) // past the gap
	}
}

// A full event queue is a change for every Channel.
func TestFolderWatchOverflowReachesEveryChannel(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	a, b := watchFolder(t, t.TempDir()), watchFolder(t, t.TempDir())

	overflow := make([]byte, unix.SizeofInotifyEvent)
	binary.NativeEndian.PutUint32(overflow[0:], ^uint32(0)) // wd -1
	binary.NativeEndian.PutUint32(overflow[4:], unix.IN_Q_OVERFLOW)
	folderWatcher.dispatch(overflow)

	expectWatchFrame(t, a.client, "changed")
	expectWatchFrame(t, b.client, "changed")
}

// A failed read of the instance ends every watch without an error result, so
// the pages open theirs again, and the next watch gets a new instance.
func TestFolderWatchReadFailureEndsEveryWatch(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	a, b := watchFolder(t, t.TempDir()), watchFolder(t, t.TempDir())

	folderWatcher.fail(fmt.Errorf("%w: %w", errFolderWatcherFailed, syscall.EIO))
	for _, w := range []*watchedFolder{a, b} {
		opcode, _ := nextWatchFrame(t, w.client, 2*time.Second)
		require.Equal(t, relay.OpStreamClose, opcode, "a close, not an error result")
		require.ErrorIs(t, <-w.done, errFolderWatcherFailed, "returned for the router to log")
	}

	dir := t.TempDir()
	later := watchFolder(t, dir)
	seedFile(t, filepath.Join(dir, "new.txt"), "x")
	expectWatchFrame(t, later.client, "changed")
}

// A watch added after the reader failed, while watches of the failed
// instance are still held, gets a working instance, not the failed one.
func TestFolderWatchAddedAfterAFailureIsNotOrphaned(t *testing.T) {
	fastFolderWatchGaps(t, 20*time.Millisecond, 40*time.Millisecond)
	held, err := openFolderWatch(t.Context(), t.TempDir()) // no Channel: stays held
	require.NoError(t, err)
	t.Cleanup(held.Close)
	_, failedReader := watcherState()

	folderWatcher.fail(fmt.Errorf("%w: %w", errFolderWatcherFailed, syscall.EIO))
	require.ErrorIs(t, (<-held.ended).err, errFolderWatcherFailed)

	dir := t.TempDir()
	later := watchFolder(t, dir)
	open, reader := watcherState()
	require.True(t, open)
	require.NotEqual(t, failedReader, reader, "the failed instance was replaced")
	select {
	case <-failedReader:
	default:
		t.Fatal("the failed instance's reader did not return")
	}
	held.Close() // the failed instance's watch leaves; the new one stays
	require.NoError(t, os.Mkdir(filepath.Join(dir, "new"), 0o755))
	expectWatchFrame(t, later.client, "changed")
}

// Ending the Channel's context ends the watch, and with the last watch the
// reader goroutine returns and the inotify instance is closed.
func TestFolderWatchEndsWithItsContext(t *testing.T) {
	requireNoWatcher(t)
	before := inotifyInstances(t)
	w := watchFolder(t, t.TempDir())
	require.Equal(t, before+1, inotifyInstances(t))
	_, readerDone := watcherState()

	w.end(t)
	require.NoError(t, <-w.done)
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the reader goroutine did not return")
	}
	require.Equal(t, before, inotifyInstances(t))
}

func inotifyInstances(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && target == "anon_inode:inotify" {
			count++
		}
	}
	return count
}

// refusedFolderWatch runs the route's handler on path, which must refuse it
// at once with an error result carrying the status of the error it returns
// for the router to log.
func refusedFolderWatch(t *testing.T, path string) error {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	done := make(chan error, 1)
	go func() { done <- streamFolderWatch(t.Context(), server, apischema.PathRequest{Path: path}) }()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	frame, err := relay.ReadRelayFrame(client)
	require.NoError(t, err)
	require.Equal(t, relay.OpStreamResult, frame.Opcode)
	var result relay.ResultFrame
	require.NoError(t, json.Unmarshal(frame.Payload, &result))
	closing, err := relay.ReadRelayFrame(client)
	require.NoError(t, err)
	require.Equal(t, relay.OpStreamClose, closing.Opcode)
	returned := <-done
	require.Error(t, returned)
	require.Equal(t, "error", result.Status)
	require.Equal(t, bridgeipc.StatusCode(returned), result.Code)
	return returned
}

// The watch resolves its path as the listing does and refuses what the
// listing refuses, with the same status.
func TestFolderWatchRefusesWhatTheListingRefuses(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	a, b := filepath.Join(tree.home, "a"), filepath.Join(tree.home, "b")
	require.NoError(t, os.Symlink(b, a))
	require.NoError(t, os.Symlink(a, b))
	gone := filepath.Join(tree.home, "gone")
	require.NoError(t, os.Symlink(filepath.Join(tree.base, "nowhere"), gone))
	file := tree.through("sub/notes.txt")

	for name, path := range map[string]string{
		"link loop":     filepath.Join(a, "x"),
		"dangling link": filepath.Join(gone, "x"),
		"file":          file,
		"empty":         "",
	} {
		t.Run(name, func(t *testing.T) {
			_, listErr := listOpenedDirectory(t.Context(), apischema.PathRequest{Path: path}, sessionOwner{})
			require.Error(t, listErr)
			watchErr := refusedFolderWatch(t, path)
			require.Equal(t, bridgeipc.StatusCode(listErr), bridgeipc.StatusCode(watchErr))
			for _, sentinel := range []error{syscall.ELOOP, bridgeipc.ErrNotFound, bridgeipc.ErrInvalidArgs} {
				require.Equal(t, errors.Is(listErr, sentinel), errors.Is(watchErr, sentinel), "%v", sentinel)
			}
		})
	}
	require.ErrorIs(t, refusedFolderWatch(t, file), bridgeipc.ErrInvalidArgs, "a file is not a folder")

	// Through a link the listing follows, the folder behind it is watched.
	w := watchFolder(t, tree.through("sub"))
	seedFile(t, tree.target("sub/new.txt"), "x")
	expectWatchFrame(t, w.client, "changed")
}

func TestFolderWatchesAreCappedPerBridge(t *testing.T) {
	require.Equal(t, 32, maxFolderWatches)
	var releases []func()
	for range maxFolderWatches {
		release, err := folderWatches.acquire()
		require.NoError(t, err)
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})

	require.ErrorIs(t, refusedFolderWatch(t, t.TempDir()), bridgeipc.ErrConflict)

	releases[0]()
	releases = releases[1:]
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	done := make(chan error, 1)
	go func() { done <- streamFolderWatch(t.Context(), server, apischema.PathRequest{Path: t.TempDir()}) }()
	expectWatchFrame(t, client, "watching") // the freed slot takes a watch
	_ = client.Close()
	select {
	case err := <-done:
		require.NoError(t, err, "the freed slot takes a watch")
	case <-time.After(2 * time.Second):
		t.Fatal("the Channel did not end")
	}
	// Its slot is free again once it ended.
	release, err := folderWatches.acquire()
	require.NoError(t, err)
	release()
}

func TestFolderWatchRouteIsAnUnprivilegedChannel(t *testing.T) {
	for _, route := range Routes {
		if route.Route != routeFolderWatch {
			continue
		}
		require.Equal(t, bridgeipc.ModeDuplex, route.Mode)
		require.False(t, route.Privileged, "a watch is a read, like the listing")
		return
	}
	t.Fatalf("route %s is not bound", routeFolderWatch)
}

// An inotify limit is a refusal that asking again will not change, as the
// cap is, and names the limit.
func TestFolderWatchLimitsAreRefusals(t *testing.T) {
	for errno, limit := range map[syscall.Errno]string{
		syscall.ENOSPC: "max_user_watches",
		syscall.EMFILE: "max_user_instances",
		syscall.ENFILE: "open files",
	} {
		err := folderWatchError(&os.PathError{Op: "inotify_add_watch", Path: "/srv", Err: errno})
		require.ErrorIs(t, err, bridgeipc.ErrConflict, "%v", errno)
		require.Contains(t, err.Error(), limit)
		err = folderWatchError(fmt.Errorf("create inotify instance: %w", errno))
		require.ErrorIs(t, err, bridgeipc.ErrConflict, "%v", errno)
	}
	require.ErrorIs(t, folderWatchError(&os.PathError{Op: "inotify_add_watch", Path: "/srv", Err: syscall.EACCES}), bridgeipc.ErrForbidden)
	require.NotErrorIs(t, folderWatchError(errFolderWatcherFailed), bridgeipc.ErrConflict)
}

// The first frame says the watch is on; a refused watch never says it (see
// refusedFolderWatch, whose first frame is the error result).
func TestFolderWatchSaysWhenItIsOn(t *testing.T) {
	w := startFolderWatch(t, t.TempDir())
	expectWatchFrame(t, w.client, "watching")
}
