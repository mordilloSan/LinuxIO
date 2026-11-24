package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/searchquery"
)

// A search inside a folder also reads that folder on disk. The index has no
// watcher, so what was made outside LinuxIO is in it only once its folder is
// opened or a scan runs; reading the folder finds it now. The read writes
// nothing and queues nothing: it does not wait for the work queue, and runs
// while a scan holds it. See docs/indexer.md, "Search inside a folder".

// liveSearchBudget is how long a search reads its folder on disk before it
// answers with what it has. A variable for tests.
var liveSearchBudget = 2 * time.Second

// liveSearchGrace is how long past its budget a search waits for its read to
// return. A read held in a call to the kernel (a failing disk, a mount that
// does not answer) sees its deadline only when the call returns; the search
// does not wait for that, and answers from the index. A variable for tests.
var liveSearchGrace = 500 * time.Millisecond

// liveSearchSlotWait is how long a search waits for a free slot before it
// answers from the index. The page cancels the search of a query that was
// typed on, and the slot of that search frees a moment later. A variable for
// tests.
var liveSearchSlotWait = 500 * time.Millisecond

// maxLiveSearches is how many searches read the disk at once.
const maxLiveSearches = 2

// walkTree reads a folder's tree on disk. A variable so that a test can hold
// a read as a call to the kernel holds it.
var walkTree = func(ctx context.Context, index *indexing.Index, folder string, match func(string) bool, deadline time.Time, keep int) (indexing.TreeSearch, error) {
	return index.SearchTree(ctx, folder, match, deadline, keep)
}

// searchSlots holds one token for each search that reads the disk.
func (d *daemon) searchSlots() chan struct{} {
	d.liveSearchesOnce.Do(func() { d.liveSearches = make(chan struct{}, maxLiveSearches) })
	return d.liveSearches
}

// takeLiveSearchSlot takes a slot, waiting for one for liveSearchSlotWait at
// most, and less when ctx ends.
func (d *daemon) takeLiveSearchSlot(ctx context.Context) bool {
	slots := d.searchSlots()
	select {
	case slots <- struct{}{}:
		return true
	default:
	}
	wait := time.NewTimer(liveSearchSlotWait)
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		return true
	case <-wait.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (d *daemon) releaseLiveSearchSlot() {
	<-d.searchSlots()
}

// diskRead is what the read of a folder's tree came to.
type diskRead struct {
	walk indexing.TreeSearch
	// stillThere are the index's results that a complete read did not
	// return and that are still on disk.
	stillThere []api.EntryResult
	err        error
}

// searchOnDisk reads the tree below basePath for the names query matches and
// makes the answer from what it found and what the index found (indexed).
// Its only error is that of a request that has ended.
func (d *daemon) searchOnDisk(ctx context.Context, query, basePath string, indexed []api.EntryResult) (api.SearchResponse, error) {
	partial := api.SearchResponse{Results: indexed, Walk: api.SearchWalkPartial}
	matcher := searchquery.Parse(query).Matcher()
	if matcher.Empty() {
		return api.SearchResponse{Results: indexed}, nil
	}
	cfg := d.settings.config()
	index := indexing.Initialize(
		"/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)),
	)
	// Taken last, so that nothing between here and the goroutine that gives
	// it back can fail.
	if !d.takeLiveSearchSlot(ctx) {
		if ctx.Err() != nil {
			return api.SearchResponse{}, ctx.Err()
		}
		return partial, nil
	}
	deadline := time.Now().Add(liveSearchBudget)
	done := make(chan diskRead, 1)
	// The read owns the slot and gives it back when it returns. It ends at
	// its deadline or when the request ends (net/http ends the request's
	// context when the handler returns), both of which it sees between two
	// calls to the kernel; one call can hold it longer, so the search does
	// not wait for it past the grace, and Close does not wait for it either.
	// It writes nothing: it reads the disk and indexed, which nothing
	// changes, and sends on a channel nobody needs to receive from.
	go func() {
		defer d.releaseLiveSearchSlot()
		done <- readOnDisk(ctx, index, basePath, matcher.Match, deadline, indexed)
	}()
	grace := time.NewTimer(time.Until(deadline) + liveSearchGrace)
	defer grace.Stop()
	var read diskRead
	select {
	case read = <-done:
	case <-grace.C:
		slog.Debug("search folder read did not return in time", "path", basePath)
		return partial, nil
	case <-ctx.Done():
		return api.SearchResponse{}, ctx.Err()
	}
	return d.answerFromDisk(ctx, basePath, indexed, read)
}

// readOnDisk reads the tree below basePath and, when the read is complete,
// looks at the index's results it did not find. It runs outside the HTTP
// recovery, so a panic here is recovered and becomes the read's error: the
// search then answers from the index.
func readOnDisk(ctx context.Context, index *indexing.Index, basePath string, match func(string) bool, deadline time.Time, indexed []api.EntryResult) (read diskRead) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("search disk read panicked", "path", basePath, "panic", recovered, "stack", string(debug.Stack()))
			read = diskRead{err: fmt.Errorf("search disk read panicked: %v", recovered)}
		}
	}()
	read.walk, read.err = walkTree(ctx, index, basePath, match, deadline, searchResultLimit)
	if read.err == nil && read.walk.Complete {
		var looked bool
		read.stillThere, looked = stillOnDisk(ctx, index, indexed, read.walk.Found, deadline)
		// Out of time before every result was looked at: not read whole.
		read.walk.Complete = looked
	}
	return read
}

// answerFromDisk makes the answer of a search from the read of its folder
// and the index's results.
func (d *daemon) answerFromDisk(ctx context.Context, basePath string, indexed []api.EntryResult, read diskRead) (api.SearchResponse, error) {
	partial := api.SearchResponse{Results: indexed, Walk: api.SearchWalkPartial}
	switch err := read.err; {
	case ctx.Err() != nil:
		return api.SearchResponse{}, ctx.Err()
	case errors.Is(err, indexing.ErrNotIndexed), indexing.NotThere(err):
		// Nothing to read: the index answers as it did before.
		slog.Debug("search folder not read on disk", "path", basePath, "error", err)
		return api.SearchResponse{Results: indexed}, nil
	case err != nil:
		slog.Debug("search folder could not be read on disk", "path", basePath, "error", err)
		return partial, nil
	}

	walk := read.walk
	sizes, err := d.store.FolderSizes(ctx, foundFolders(walk.Found))
	if err != nil {
		if ctx.Err() != nil {
			return api.SearchResponse{}, ctx.Err()
		}
		slog.Warn("search answered from the index alone", "path", basePath, "error", err)
		return partial, nil
	}
	found := make([]api.EntryResult, 0, len(walk.Found))
	for _, entry := range walk.Found {
		found = append(found, searchResultFromDisk(entry, sizes))
	}
	if walk.Complete {
		// The index's results that the read did not find and that are gone
		// from the disk are left out.
		return api.SearchResponse{Results: newestResults(append(found, read.stillThere...)), Walk: api.SearchWalkComplete}, nil
	}
	return api.SearchResponse{Results: newestResults(mergeResults(indexed, found)), Walk: api.SearchWalkPartial}, nil
}

// stillOnDisk returns the index's results that a complete read did not
// return and that are still on disk. The read's matching is not the index's
// rune for rune, so a result it did not return is looked at before it is
// taken for gone; one the index now leaves out is not looked at. One that
// cannot be looked at is kept. Each look opens every folder from "/", so it
// stops at deadline, or when ctx ends, keeps those not looked at yet, and
// reports that it did not look at them all.
func stillOnDisk(ctx context.Context, index *indexing.Index, indexed []api.EntryResult, found []indexing.IndexEntry, deadline time.Time) ([]api.EntryResult, bool) {
	read := make(map[string]struct{}, len(found))
	for _, entry := range found {
		read[entry.RelativePath] = struct{}{}
	}
	var kept []api.EntryResult
	for i, result := range indexed {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			for _, rest := range indexed[i:] {
				if _, ok := read[rest.Path]; !ok {
					kept = append(kept, rest)
				}
			}
			return kept, false
		}
		if _, ok := read[result.Path]; ok || index.LeavesOut(result.Path) {
			continue
		}
		if _, err := indexing.Lstat(result.Path); !indexing.NotThere(err) {
			kept = append(kept, result)
		}
	}
	return kept, true
}

func foundFolders(entries []indexing.IndexEntry) []string {
	var folders []string
	for _, entry := range entries {
		if entry.Type == "directory" {
			folders = append(folders, entry.RelativePath)
		}
	}
	return folders
}

// searchResultFromDisk is the answer for an entry read on disk. A folder's
// size is the one the index holds for it, and unknown when it holds none: a
// folder's own entry is not the size of what is below it.
func searchResultFromDisk(entry indexing.IndexEntry, folderSizes map[string]int64) api.EntryResult {
	result := api.EntryResult{
		Path:    entry.RelativePath,
		Name:    entry.Name,
		Type:    "file",
		Size:    entry.Size,
		ModTime: entry.ModTime,
		Inode:   entry.Inode,
	}
	if entry.Type == "directory" {
		result.Type = "folder"
		size, known := folderSizes[entry.RelativePath]
		result.Size, result.SizeUnknown = size, !known
	}
	return result
}

// mergeResults is the index's results and those read on disk, one per path;
// what was read on disk is the newer of the two.
func mergeResults(indexed, found []api.EntryResult) []api.EntryResult {
	onDisk := make(map[string]struct{}, len(found))
	for _, result := range found {
		onDisk[result.Path] = struct{}{}
	}
	merged := slices.Clone(found)
	for _, result := range indexed {
		if _, ok := onDisk[result.Path]; !ok {
			merged = append(merged, result)
		}
	}
	return merged
}

// newestResults orders results newest first, as the index does, and keeps
// as many as a search answers.
func newestResults(results []api.EntryResult) []api.EntryResult {
	slices.SortFunc(results, func(a, b api.EntryResult) int {
		return cmp.Or(b.ModTime.Compare(a.ModTime), strings.Compare(a.Path, b.Path))
	})
	if len(results) > searchResultLimit {
		results = results[:searchResultLimit]
	}
	return results
}
