package daemon

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

func getSearch(t *testing.T, d *daemon, query, base string) api.SearchResponse {
	t.Helper()
	rr := searchRequest(d, query, base)
	if rr.Code != http.StatusOK {
		t.Fatalf("search answered %d %q, want 200", rr.Code, rr.Body.String())
	}
	var resp api.SearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search answer %q: %v", rr.Body.String(), err)
	}
	return resp
}

func searchRequest(d *daemon, query, base string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	d.handleSearch(rr, httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(query)+"&base="+url.QueryEscape(base), nil))
	return rr
}

func resultAt(resp api.SearchResponse, path string) (api.EntryResult, bool) {
	for _, result := range resp.Results {
		if result.Path == path {
			return result, true
		}
	}
	return api.EntryResult{}, false
}

func setLiveSearchBudget(t *testing.T, budget time.Duration) {
	t.Helper()
	previous := liveSearchBudget
	liveSearchBudget = budget
	t.Cleanup(func() { liveSearchBudget = previous })
}

func TestASearchInAFolderFindsAFileTheIndexHasNotSeen(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	fresh := filepath.Join(folder, "sub", "never-opened", "fresh-report.txt")
	writeFile(t, fresh)

	resp := getSearch(t, d, "fresh-report", folder)

	if resp.Walk != api.SearchWalkComplete {
		t.Fatalf("walk = %q, want complete", resp.Walk)
	}
	if _, ok := resultAt(resp, fresh); !ok {
		t.Fatalf("a file made outside LinuxIO was not found: %+v", resp.Results)
	}
	if len(paths()) != 4 {
		t.Fatalf("the search wrote the index: %v", paths())
	}
	if d.busy() {
		t.Fatal("the search queued index work")
	}
}

func TestAnEntryGoneFromTheDiskIsNotFoundWhenTheFolderWasReadWhole(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	if err := os.Remove(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	resp := getSearch(t, d, "a.txt", folder)

	if resp.Walk != api.SearchWalkComplete {
		t.Fatalf("walk = %q, want complete", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "a.txt")); ok {
		t.Fatal("a file removed from the disk was found from the index")
	}
}

func TestASearchCutShortAnswersWithTheIndexsResults(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	setLiveSearchBudget(t, 0)

	resp := getSearch(t, d, "b.txt", folder)

	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("the index's result is missing: %+v", resp.Results)
	}
}

func TestASearchFromTheRootReadsTheIndexOnly(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	fresh := filepath.Join(folder, "fresh-report.txt")
	writeFile(t, fresh)

	resp := getSearch(t, d, "fresh-report", "/")

	if resp.Walk != "" {
		t.Fatalf("walk = %q for a search from /, want none", resp.Walk)
	}
	if _, ok := resultAt(resp, fresh); ok {
		t.Fatal("a search from / read the disk")
	}
}

func TestASearchInAFolderRunsWhileTheQueueIsHeld(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	release := holdQueue(t, d)
	defer release()
	fresh := filepath.Join(folder, "fresh-report.txt")
	writeFile(t, fresh)

	resp := getSearch(t, d, "fresh-report", folder)

	if _, ok := resultAt(resp, fresh); !ok || resp.Walk != api.SearchWalkComplete {
		t.Fatalf("a search behind held work answered %+v", resp)
	}
}

// holdSlots takes every slot a search reads the disk with, and gives back
// a function that frees one.
func holdSlots(t *testing.T, d *daemon) (freeOne func()) {
	t.Helper()
	for range maxLiveSearches {
		if !d.takeLiveSearchSlot(context.Background()) {
			t.Fatal("a free slot was refused")
		}
	}
	held := maxLiveSearches
	t.Cleanup(func() {
		for ; held > 0; held-- {
			d.releaseLiveSearchSlot()
		}
	})
	return func() {
		held--
		d.releaseLiveSearchSlot()
	}
}

func setSlotWait(t *testing.T, wait time.Duration) {
	t.Helper()
	previous := liveSearchSlotWait
	liveSearchSlotWait = wait
	t.Cleanup(func() { liveSearchSlotWait = previous })
}

func TestASearchWithoutAFreeSlotAnswersFromTheIndexAfterAShortWait(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	holdSlots(t, d)
	setSlotWait(t, 50*time.Millisecond)
	fresh := filepath.Join(folder, "fresh-b.txt")
	writeFile(t, fresh)

	start := time.Now()
	resp := getSearch(t, d, "b.txt", folder)

	if waited := time.Since(start); waited < 50*time.Millisecond {
		t.Fatalf("the search answered after %v, before its wait for a slot was over", waited)
	}
	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	if _, ok := resultAt(resp, fresh); ok {
		t.Fatal("a search with no free slot read the disk")
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("the index's result is missing: %+v", resp.Results)
	}
}

func TestASearchGetsASlotThatFreesWhileItWaits(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	freeOne := holdSlots(t, d)
	setSlotWait(t, 5*time.Second)
	fresh := filepath.Join(folder, "fresh-b.txt")
	writeFile(t, fresh)

	answered := make(chan api.SearchResponse, 1)
	go func() {
		rr := searchRequest(d, "b.txt", folder)
		var resp api.SearchResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		answered <- resp
	}()
	time.Sleep(100 * time.Millisecond)
	freeOne()

	resp := <-answered
	if _, ok := resultAt(resp, fresh); !ok || resp.Walk != api.SearchWalkComplete {
		t.Fatalf("a search that got a slot while it waited answered %+v", resp)
	}
}

// A walk can be held in a call to the kernel that neither its deadline nor
// its request can interrupt, on a failing disk or a mount that does not
// answer.
func TestASearchWhoseReadDoesNotReturnAnswersFromTheIndex(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	setLiveSearchBudget(t, 50*time.Millisecond)
	previousGrace := liveSearchGrace
	liveSearchGrace = 50 * time.Millisecond
	t.Cleanup(func() { liveSearchGrace = previousGrace })
	release := make(chan struct{})
	returned := make(chan struct{})
	previousWalk := walkTree
	walkTree = func(context.Context, *indexing.Index, string, func(string) bool, time.Time, int) (indexing.TreeSearch, error) {
		<-release
		close(returned)
		return indexing.TreeSearch{Complete: true}, nil
	}
	t.Cleanup(func() { walkTree = previousWalk })
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})

	start := time.Now()
	resp := getSearch(t, d, "b.txt", folder)
	waited := time.Since(start)

	if waited < 100*time.Millisecond || waited > 2*time.Second {
		t.Fatalf("the search answered after %v, want the budget and grace, 100ms", waited)
	}
	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("the index's result is missing: %+v", resp.Results)
	}
	if held := len(d.searchSlots()); held != 1 {
		t.Fatalf("%d slots held while the walk has not returned, want 1", held)
	}

	released = true
	close(release)
	<-returned
	waitUntil(t, "the walk frees its slot", func() bool { return len(d.searchSlots()) == 0 })
}

// The walk's matching is not trusted to be the index's: an index result
// below the folder that the walk did not return is looked at, and kept when
// it is still on disk.
func TestACompleteReadKeepsAnIndexResultThatIsStillOnDisk(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	if err := os.Remove(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	previousWalk := walkTree
	walkTree = func(ctx context.Context, index *indexing.Index, base string, _ func(string) bool, deadline time.Time, keep int) (indexing.TreeSearch, error) {
		// A matcher stricter than the index's: it accepts nothing.
		return previousWalk(ctx, index, base, func(string) bool { return false }, deadline, keep)
	}
	t.Cleanup(func() { walkTree = previousWalk })

	resp := getSearch(t, d, ".txt", folder)

	if resp.Walk != api.SearchWalkComplete {
		t.Fatalf("walk = %q, want complete", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("an indexed file still on disk was dropped: %+v", resp.Results)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "a.txt")); ok {
		t.Fatal("an indexed file gone from disk was kept")
	}
}

func TestACompleteReadDropsAnIndexResultTheIndexNowLeavesOut(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	d.settings.cfg.ExcludePaths = []string{filepath.Join(folder, "sub")}

	resp := getSearch(t, d, "b.txt", folder)

	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); ok || resp.Walk != api.SearchWalkComplete {
		t.Fatalf("an index result below an excluded folder was answered: %+v", resp)
	}
}

// A walk cut short that found entries: one result per path, the disk's entry
// winning, and what only the index holds kept.
func TestAPartialReadJoinsWhatItFoundWithTheIndex(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder whatever its mode")
	}
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "report-1.txt"))
	writeFile(t, filepath.Join(folder, "closed", "report-2.txt"))
	postReindex(d, folder)
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 4 && !d.busy() })
	later := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(folder, "report-1.txt"), later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	writeFile(t, filepath.Join(folder, "report-3.txt"))
	closed := filepath.Join(folder, "closed")
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	resp := getSearch(t, d, "report-", folder)

	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	seen := map[string]int{}
	for _, result := range resp.Results {
		seen[result.Path]++
	}
	for _, path := range []string{"report-1.txt", "report-3.txt", filepath.Join("closed", "report-2.txt")} {
		if seen[filepath.Join(folder, path)] != 1 {
			t.Fatalf("%s answered %d times: %+v", path, seen[filepath.Join(folder, path)], resp.Results)
		}
	}
	if first, _ := resultAt(resp, filepath.Join(folder, "report-1.txt")); !first.ModTime.Equal(later) {
		t.Fatalf("report-1.txt answered with %v, want the disk's %v", first.ModTime, later)
	}
}

func TestAFolderFoundOnlyOnDiskHasNoSize(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "report-old", "a.txt"))
	postReindex(d, folder)
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 3 && !d.busy() })
	writeFile(t, filepath.Join(folder, "report-new", "b.txt"))

	resp := getSearch(t, d, "report-", folder)

	indexed, ok := resultAt(resp, filepath.Join(folder, "report-old"))
	if !ok || indexed.SizeUnknown || indexed.Size != folderSize(t, db, filepath.Join(folder, "report-old")) {
		t.Fatalf("the indexed folder was answered as %+v, want the index's size %d", indexed, folderSize(t, db, filepath.Join(folder, "report-old")))
	}
	found, ok := resultAt(resp, filepath.Join(folder, "report-new"))
	if !ok || !found.SizeUnknown || found.Type != "folder" {
		t.Fatalf("the folder found on disk was answered as %+v, want a folder of unknown size", found)
	}
}

func TestASearchInAFolderBeforeTheFirstIndexIsUnavailable(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "report.txt"))

	if rr := searchRequest(d, "report", folder); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("search answered %d %q, want 503", rr.Code, rr.Body.String())
	}
}

// The read runs on a goroutine of its own, outside the HTTP recovery: a
// panic there must not end the daemon.
func TestAReadThatPanicsAnswersFromTheIndex(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	previousWalk := walkTree
	walkTree = func(context.Context, *indexing.Index, string, func(string) bool, time.Time, int) (indexing.TreeSearch, error) {
		panic("read failed")
	}
	t.Cleanup(func() { walkTree = previousWalk })

	resp := getSearch(t, d, "b.txt", folder)

	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("the index's result is missing: %+v", resp.Results)
	}
	waitUntil(t, "the read frees its slot", func() bool { return len(d.searchSlots()) == 0 })

	walkTree = previousWalk
	if again := getSearch(t, d, "b.txt", folder); again.Walk != api.SearchWalkComplete {
		t.Fatalf("the next search answered %+v, want a complete read", again)
	}
}

// Looking at the index's results one by one takes time on a slow disk; the
// read stops at its deadline and keeps those it has not looked at.
func TestACompleteReadThatRunsOutOfTimeCheckingTheIndexIsPartial(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	setLiveSearchBudget(t, 50*time.Millisecond)
	if err := os.Remove(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	previousWalk := walkTree
	walkTree = func(ctx context.Context, index *indexing.Index, base string, _ func(string) bool, deadline time.Time, keep int) (indexing.TreeSearch, error) {
		search, err := previousWalk(ctx, index, base, func(string) bool { return false }, deadline, keep)
		time.Sleep(time.Until(deadline) + 10*time.Millisecond)
		return search, err
	}
	t.Cleanup(func() { walkTree = previousWalk })

	resp := getSearch(t, d, ".txt", folder)

	if resp.Walk != api.SearchWalkPartial {
		t.Fatalf("walk = %q, want partial", resp.Walk)
	}
	if _, ok := resultAt(resp, filepath.Join(folder, "sub", "b.txt")); !ok {
		t.Fatalf("an index result not looked at was dropped: %+v", resp.Results)
	}
}

func TestStillOnDiskPastItsDeadlineKeepsWhatItHasNotLookedAt(t *testing.T) {
	gone := api.EntryResult{Path: filepath.Join(t.TempDir(), "gone.txt")}
	kept, done := stillOnDisk(context.Background(), indexing.Initialize("/"), []api.EntryResult{gone}, nil, time.Now().Add(-time.Second))
	if done || len(kept) != 1 || kept[0].Path != gone.Path {
		t.Fatalf("stillOnDisk past its deadline = %+v, %v; want the result kept and not done", kept, done)
	}
}
