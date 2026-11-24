package storage

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// A cache the planner has no statistics for (a new one, or one PRAGMA optimize
// has not analysed) must still find a hardlink group through its index: without
// it SQLite read the whole index in path order for every group, which made a
// reindex quadratic in the number of hardlinked files.
func TestHardlinkGroupQueriesUseHardlinkIndex(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	for name, query := range map[string]string{
		"load group":          loadHardlinkGroupQuery,
		"current contributor": currentHardlinkContributorQuery,
	} {
		const want = "SEARCH entries USING INDEX idx_entries_hardlinks (index_id=? AND device=? AND inode=?)"
		if plan := queryPlan(t, ctx, db, query, indexID, 1, 2); !strings.Contains(plan, want) {
			t.Errorf("%s plan = %q, want %q", name, plan, want)
		}
	}
}

// queryPlan returns the steps of SQLite's plan for query, one per line.
func queryPlan(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close plan: %v", closeErr)
		}
	}()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if scanErr := rows.Scan(&id, &parent, &unused, &detail); scanErr != nil {
			t.Fatalf("scan plan: %v", scanErr)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return strings.Join(plan, "\n")
}

// reconcileHardlinksAfterReindexPerFile is reconcileHardlinksAfterReindex as it
// was before the size changes were added up per folder: every changed
// contribution walked its own folders, one update per folder per file. It is
// kept to prove the two leave every row the same.
func reconcileHardlinksAfterReindexPerFile(ctx context.Context, db dbExecutor, indexID int64, relativePath string, scanTime int64, snapshot *hardlinkSnapshot, sameFile SameFileFunc) error {
	groups, err := hardlinkGroupsUnderPath(ctx, db, indexID, relativePath, sameFile)
	if err != nil {
		return err
	}
	if snapshot != nil {
		for identity, preferred := range snapshot.groups {
			if _, exists := groups[identity]; !exists || preferred != "" {
				groups[identity] = preferred
			}
		}
	}
	for identity, preferred := range groups {
		deltas, err := reconcileHardlinkGroup(ctx, db, indexID, identity, hardlinkReconcileOptions{
			preferred: preferred,
			scanTime:  scanTime,
			sameFile:  sameFile,
		})
		if err != nil {
			return err
		}
		for _, delta := range deltas {
			stopPath := ""
			if relativePath != "" && pathInSubtree(delta.path, relativePath) {
				stopPath = relativePath
			}
			if err := updateParentDirectorySizesThrough(ctx, db, indexID, delta.path, delta.delta, stopPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// updateParentDirectorySizesThrough is updateParentDirectorySizes as the
// per-file reconcile used it: it stops after the folder at stopPath.
func updateParentDirectorySizesThrough(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64, stopPath string) error {
	if sizeDelta == 0 {
		return nil
	}
	if stopPath != "" {
		stopPath = indexerapi.NormalizePath(stopPath)
	}
	for currentPath := parentDirKey(childPath); currentPath != ""; currentPath = parentDirKey(currentPath) {
		if _, err := db.ExecContext(ctx, `
			UPDATE entries
			SET size = size + ?
			WHERE index_id = ? AND relative_path = ? AND type = 1;
		`, sizeDelta, indexID, currentPath); err != nil {
			return err
		}
		if currentPath == "/" || currentPath == stopPath {
			break
		}
	}
	return nil
}

// fakeFS is the filesystem a randomized reindex runs against: folders, and
// the identity each file path names.
type fakeFS struct {
	rng     *rand.Rand
	folders []string
	files   map[string]hardlinkIdentity
	sizes   map[hardlinkIdentity]int64
	inode   int64
	name    int
}

func newFakeFS(seed uint64) *fakeFS {
	fs := &fakeFS{
		rng:     rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		folders: []string{"/", "/t"},
		files:   map[string]hardlinkIdentity{},
		sizes:   map[hardlinkIdentity]int64{},
	}
	for top := range 2 + fs.rng.IntN(3) {
		dir := fmt.Sprintf("/t/d%d", top)
		fs.folders = append(fs.folders, dir)
		for sub := range fs.rng.IntN(3) {
			subDir := fmt.Sprintf("%s/s%d", dir, sub)
			fs.folders = append(fs.folders, subDir)
			if fs.rng.IntN(2) == 0 {
				fs.folders = append(fs.folders, subDir+"/k")
			}
		}
	}
	// Siblings named after "/t/d1": "/t/d1.x" sorts inside the range
	// subtreeBounds("/t/d1") seeks and "/t/d1x" after it.
	fs.folders = append(fs.folders, "/t/d1.x", "/t/d1x")
	for range 15 + fs.rng.IntN(40) {
		if len(fs.files) > 0 && fs.rng.IntN(3) == 0 {
			fs.addLink(fs.fileFolder())
		} else {
			fs.addFile()
		}
	}
	// The same inode twice in one folder, and once across folders.
	same := fs.anyFile()
	fs.files[fs.newPath(parentDirKey(same))] = fs.files[same]
	fs.addLink(fs.fileFolder())
	return fs
}

// fileFolder picks a folder for a file, "/" included.
func (fs *fakeFS) fileFolder() string { return fs.folders[fs.rng.IntN(len(fs.folders))] }

func (fs *fakeFS) newPath(folder string) string {
	fs.name++
	return fmt.Sprintf("%s/f%d", strings.TrimSuffix(folder, "/"), fs.name)
}

func (fs *fakeFS) sortedFiles() []string { return slices.Sorted(maps.Keys(fs.files)) }

func (fs *fakeFS) anyFile() string {
	paths := fs.sortedFiles()
	return paths[fs.rng.IntN(len(paths))]
}

func (fs *fakeFS) addFile() {
	fs.inode++
	// A second device reuses inode numbers: a different file, not a link.
	identity := hardlinkIdentity{device: int64(1 + fs.rng.IntN(2)), inode: 1 + fs.inode%40}
	if _, taken := fs.sizes[identity]; taken {
		identity = hardlinkIdentity{device: 3, inode: fs.inode}
	}
	fs.sizes[identity] = int64(1 + fs.rng.IntN(64)*512)
	fs.files[fs.newPath(fs.fileFolder())] = identity
}

func (fs *fakeFS) addLink(folder string) {
	fs.files[fs.newPath(folder)] = fs.files[fs.anyFile()]
}

func (fs *fakeFS) links(identity hardlinkIdentity) int {
	count := 0
	for _, other := range fs.files {
		if other == identity {
			count++
		}
	}
	return count
}

// mutate changes the tree the way a user would between two scans.
func (fs *fakeFS) mutate() {
	for range 3 + fs.rng.IntN(8) {
		if len(fs.files) < 2 {
			fs.addFile()
			continue
		}
		path := fs.anyFile()
		identity := fs.files[path]
		switch fs.rng.IntN(7) {
		case 0: // move
			delete(fs.files, path)
			fs.files[fs.newPath(fs.fileFolder())] = identity
		case 1: // remove a file or one of its links
			delete(fs.files, path)
		case 2:
			fs.addFile()
		case 3: // a link across folders
			fs.addLink(fs.fileFolder())
		case 4: // a link beside the file
			fs.files[fs.newPath(parentDirKey(path))] = identity
		case 5: // the file grows or shrinks, under every name
			fs.sizes[identity] = int64(1 + fs.rng.IntN(64)*512)
		case 6: // the name now holds another file
			fs.inode++
			replaced := hardlinkIdentity{device: 3, inode: fs.inode}
			fs.sizes[replaced] = int64(1 + fs.rng.IntN(64)*512)
			fs.files[path] = replaced
		}
	}
}

func (fs *fakeFS) sameFile(path string, device, inode int64) bool {
	identity, ok := fs.files[path]
	return ok && identity == hardlinkIdentity{device: device, inode: inode}
}

// scan returns the rows a scan of root writes, as the scanner computes them:
// in walk order the first link of a file with several links contributes, and
// every folder holds the contributions below it.
func (fs *fakeFS) scan(root string) []indexing.IndexEntry {
	var files []string
	for _, path := range fs.sortedFiles() {
		if pathInSubtree(path, root) {
			files = append(files, path)
		}
	}
	fs.rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	folderSizes := map[string]int64{}
	for _, folder := range fs.folders {
		if pathInSubtree(folder, root) {
			folderSizes[folder] = 0
		}
	}
	seen := map[hardlinkIdentity]bool{}
	entries := make([]indexing.IndexEntry, 0, len(files)+len(folderSizes))
	for _, path := range files {
		identity := fs.files[path]
		size := fs.sizes[identity]
		contribution := size
		if fs.links(identity) > 1 {
			if seen[identity] {
				contribution = 0
			}
			seen[identity] = true
		}
		entries = append(entries, indexing.IndexEntry{
			RelativePath: path, Name: path[strings.LastIndex(path, "/")+1:], Size: size,
			ModTime: time.Unix(1, 0), Type: "file",
			Device: uint64(identity.device), Inode: uint64(identity.inode), Nlink: 2, SizeContribution: contribution,
		})
		for dir := parentDirKey(path); pathInSubtree(dir, root); dir = parentDirKey(dir) {
			folderSizes[dir] += contribution
			if dir == "/" {
				break
			}
		}
	}
	for folder, size := range folderSizes {
		entries = append(entries, indexing.IndexEntry{
			RelativePath: folder, Name: folder[strings.LastIndex(folder, "/")+1:], Size: size,
			ModTime: time.Unix(1, 0), Type: "directory",
		})
	}
	return entries
}

type storedAccounting struct {
	size, contribution int64
}

func readAccounting(t *testing.T, ctx context.Context, tx *sql.Tx, indexID int64) map[string]storedAccounting {
	t.Helper()
	rows, err := tx.QueryContext(ctx, `SELECT relative_path, size, size_contribution FROM entries WHERE index_id = ?`, indexID)
	if err != nil {
		t.Fatalf("read accounting: %v", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close accounting rows: %v", closeErr)
		}
	}()
	rowsByPath := map[string]storedAccounting{}
	for rows.Next() {
		var path string
		var row storedAccounting
		if err := rows.Scan(&path, &row.size, &row.contribution); err != nil {
			t.Fatalf("scan accounting: %v", err)
		}
		rowsByPath[path] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate accounting: %v", err)
	}
	return rowsByPath
}

// reconcileRun is one randomized reindex reconciled both ways from the same
// state: the rows before the reconcile, after the per-file one and after the
// per-folder one.
type reconcileRun struct {
	fs                         *fakeFS
	root                       string
	before, perFile, perFolder map[string]storedAccounting
}

// reconcileBothWays writes a first scan of a random tree, changes the tree,
// rescans one of its folders as a scoped reindex does, then runs the per-file
// and the per-folder reconcile on that same state.
func reconcileBothWays(t *testing.T, ctx context.Context, db *sql.DB, seed uint64) reconcileRun {
	t.Helper()
	indexID := insertTestIndex(t, db)
	run := reconcileRun{fs: newFakeFS(seed)}
	if err := insertEntriesBatch(ctx, db, indexID, 1, run.fs.scan("/")); err != nil {
		t.Fatalf("seed %d: write first scan: %v", seed, err)
	}
	run.fs.mutate()
	run.root = run.fs.folders[run.fs.rng.IntN(len(run.fs.folders))]

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("seed %d: begin: %v", seed, err)
	}
	defer rollbackTx(tx, "hardlink reconcile comparison")
	snapshot, err := snapshotHardlinksUnderPath(ctx, tx, indexID, run.root, run.fs.sameFile)
	if err != nil {
		t.Fatalf("seed %d: snapshot: %v", seed, err)
	}
	if err := insertEntriesBatch(ctx, tx, indexID, 2, run.fs.scan(run.root)); err != nil {
		t.Fatalf("seed %d: write rescan of %s: %v", seed, run.root, err)
	}
	if _, err := cleanupDeletedEntriesUnderPath(ctx, tx, indexID, run.root, 2); err != nil {
		t.Fatalf("seed %d: cleanup: %v", seed, err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT reconcile`); err != nil {
		t.Fatalf("seed %d: savepoint: %v", seed, err)
	}
	run.before = readAccounting(t, ctx, tx, indexID)
	if err := reconcileHardlinksAfterReindexPerFile(ctx, tx, indexID, run.root, 2, snapshot, run.fs.sameFile); err != nil {
		t.Fatalf("seed %d: per-file reconcile: %v", seed, err)
	}
	run.perFile = readAccounting(t, ctx, tx, indexID)
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO reconcile`); err != nil {
		t.Fatalf("seed %d: rollback to savepoint: %v", seed, err)
	}
	// The reconcile is idempotent, so the comparison would pass trivially if
	// the second one started from the rows the first one left.
	if restored := readAccounting(t, ctx, tx, indexID); !maps.Equal(restored, run.before) {
		t.Fatalf("seed %d: rollback to savepoint did not restore the rows the per-file reconcile changed", seed)
	}
	if err := reconcileHardlinksAfterReindex(ctx, tx, indexID, run.root, 2, snapshot, run.fs.sameFile); err != nil {
		t.Fatalf("seed %d: per-folder reconcile: %v", seed, err)
	}
	run.perFolder = readAccounting(t, ctx, tx, indexID)
	return run
}

// paths lists every row either reconcile left, sorted.
func (run reconcileRun) paths() []string {
	paths := slices.Collect(maps.Keys(run.perFile))
	for path := range run.perFolder {
		if _, ok := run.perFile[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// compare fails the test on every row the two reconciles left differently,
// and returns how many folder sizes the reconcile changed.
func (run reconcileRun) compare(t *testing.T, seed uint64) (foldersMoved int) {
	t.Helper()
	for _, path := range run.paths() {
		if run.perFile[path] != run.perFolder[path] {
			t.Errorf("seed %d, reindex of %s: %s = %+v per folder, %+v per file", seed, run.root, path, run.perFolder[path], run.perFile[path])
		}
		if slices.Contains(run.fs.folders, path) && run.before[path] != run.perFile[path] {
			foldersMoved++
		}
	}
	return foldersMoved
}

// boundaryChanges reports, for a reindex below "/", whether the reconcile
// moved the reindexed folder but not its parent, which only a walk that stops
// at the reindexed folder does, and whether it moved "/", which only a change
// outside the reindexed folder can reach.
func (run reconcileRun) boundaryChanges() (stoppedAtRoot, reachedSlash bool) {
	if run.root == "/" {
		return false, false
	}
	changed := func(folder string) bool { return run.before[folder] != run.perFile[folder] }
	return changed(run.root) && !changed(parentDirKey(run.root)), changed("/")
}

// The per-folder reconcile must leave every row, folder sizes above all, as
// the per-file one did, on trees where files moved, were removed and added,
// gained and lost links (across folders, across the reindexed folder's
// boundary, and twice in one folder) and changed size or identity. Files sit
// in "/" too, and two folders are named after a sibling ("/t/d1x", "/t/d1.x").
func TestReconcileHardlinksAfterReindexMatchesPerFile(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	const seeds = 150
	foldersMoved, stoppedAtRoot, reachedSlash, filesInSlash := 0, 0, 0, 0
	for seed := range uint64(seeds) {
		run := reconcileBothWays(t, ctx, db, seed)
		foldersMoved += run.compare(t, seed)
		stopped, reached := run.boundaryChanges()
		stoppedAtRoot += boolToInt(stopped)
		reachedSlash += boolToInt(reached)
		filesInSlash += boolToInt(slices.ContainsFunc(run.fs.sortedFiles(), func(path string) bool { return parentDirKey(path) == "/" }))
	}
	t.Logf("over %d seeds: %d folder sizes changed, %d reindexes stopped at the reindexed folder, %d reached /, %d trees had files in /",
		seeds, foldersMoved, stoppedAtRoot, reachedSlash, filesInSlash)
	// Prove the trees exercised the folder updates and both walks at all.
	if foldersMoved < seeds || stoppedAtRoot == 0 || reachedSlash == 0 || filesInSlash == 0 {
		t.Fatalf("coverage too thin: %d folder sizes changed (want at least %d), %d stops at the reindexed folder, %d changes reaching /, %d trees with files in / (want at least 1 each)",
			foldersMoved, seeds, stoppedAtRoot, reachedSlash, filesInSlash)
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// cleanupDeletedEntriesUnderPath removes the rows below a path that the scan
// stamped scanTime did not write. These tests write every row of a rescan, so
// it stands in for the reconciler's removal of what was not seen.
func cleanupDeletedEntriesUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string, scanTime int64) (int64, error) {
	lo, childLo, hi := subtreeBounds(relativePath)
	result, err := db.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ?
		  AND last_seen < ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, scanTime, lo, hi, lo, childLo)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
