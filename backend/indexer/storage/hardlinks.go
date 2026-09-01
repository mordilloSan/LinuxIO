package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
)

type hardlinkIdentity struct {
	device int64
	inode  int64
}

type contributionDelta struct {
	path  string
	delta int64
}

type hardlinkEntry struct {
	path         string
	size         int64
	contribution int64
	lastSeen     int64
}

type storedEntryAccounting struct {
	typ          string
	identity     hardlinkIdentity
	contribution int64
	exists       bool
}

// HardlinkSnapshot preserves contributor ownership while a subtree is
// overwritten by a partial reindex.
type HardlinkSnapshot struct {
	groups map[hardlinkIdentity]string
}

// SnapshotHardlinksUnderPath captures hardlink contributors before a partial
// reindex overwrites the subtree's rows.
func SnapshotHardlinksUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string) (*HardlinkSnapshot, error) {
	groups, err := hardlinkGroupsUnderPath(ctx, db, indexID, relativePath)
	if err != nil {
		return nil, err
	}
	return &HardlinkSnapshot{groups: groups}, nil
}

func hardlinkGroupsUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string) (map[hardlinkIdentity]string, error) {
	lo, childLo, hi := subtreeBounds(relativePath)
	rows, err := db.QueryContext(ensureContext(ctx), `
		WITH touched AS (
			SELECT DISTINCT device, inode
			FROM entries
			WHERE index_id = ? AND type != 'directory' AND inode != 0
			  AND relative_path >= ? AND relative_path < ?
			  AND (relative_path = ? OR relative_path >= ?)
		), touched_groups AS (
			SELECT touched.device, touched.inode
			FROM touched
			JOIN entries e ON e.device = touched.device AND e.inode = touched.inode
			WHERE e.index_id = ? AND e.type != 'directory'
			GROUP BY touched.device, touched.inode
			HAVING COUNT(*) > 1
		)
		SELECT e.device, e.inode, e.relative_path, e.size_contribution
		FROM entries e
		JOIN touched_groups g ON g.device = e.device AND g.inode = e.inode
		WHERE e.index_id = ?
		ORDER BY e.device, e.inode, e.relative_path;
	`, indexID, lo, hi, lo, childLo, indexID, indexID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make(map[hardlinkIdentity]string)
	for rows.Next() {
		var identity hardlinkIdentity
		var path string
		var contribution int64
		if err := rows.Scan(&identity.device, &identity.inode, &path, &contribution); err != nil {
			return nil, err
		}
		if _, exists := groups[identity]; !exists {
			groups[identity] = ""
		}
		if contribution != 0 && groups[identity] == "" {
			groups[identity] = path
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func currentHardlinkContributor(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity) (string, error) {
	if identity.inode == 0 {
		return "", nil
	}
	var path string
	err := db.QueryRowContext(ensureContext(ctx), `
		SELECT relative_path
		FROM entries
		WHERE index_id = ? AND type != 'directory'
		  AND device = ? AND inode = ? AND size_contribution != 0
		ORDER BY relative_path
		LIMIT 1;
	`, indexID, identity.device, identity.inode).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return path, err
}

func loadStoredEntryAccounting(ctx context.Context, db dbExecutor, indexID int64, path string) (storedEntryAccounting, error) {
	var stored storedEntryAccounting
	err := db.QueryRowContext(ensureContext(ctx), `
		SELECT type, device, inode, size_contribution FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, path).Scan(&stored.typ, &stored.identity.device, &stored.identity.inode, &stored.contribution)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, nil
	}
	stored.exists = err == nil
	return stored, err
}

func replaceEntryAccounting(ctx context.Context, db dbExecutor, indexID int64, entry indexing.IndexEntry, old storedEntryAccounting) error {
	newIdentity := hardlinkIdentity{device: int64(entry.Device), inode: int64(entry.Inode)}
	preferredNew, err := currentHardlinkContributor(ctx, db, indexID, newIdentity)
	if err != nil {
		return fmt.Errorf("read hardlink contributor: %w", err)
	}
	preferredOld := ""
	if old.identity != newIdentity {
		preferredOld, err = currentHardlinkContributor(ctx, db, indexID, old.identity)
		if err != nil {
			return fmt.Errorf("read prior hardlink contributor: %w", err)
		}
	}
	if old.contribution != 0 {
		if err := UpdateParentDirectorySizes(ctx, db, indexID, entry.RelativePath, -old.contribution); err != nil {
			return err
		}
	}
	entry.SizeContribution = 0
	if _, err := UpdateEntry(ctx, db, indexID, entry); err != nil {
		return err
	}
	if old.identity != newIdentity {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, old.identity, preferredOld, nil, 0, ""); err != nil {
			return err
		}
	}
	return addEntryContribution(ctx, db, indexID, entry, newIdentity, preferredNew)
}

func addEntryContribution(ctx context.Context, db dbExecutor, indexID int64, entry indexing.IndexEntry, identity hardlinkIdentity, preferred string) error {
	if identity.inode != 0 {
		return reconcileAndApplyHardlink(ctx, db, indexID, identity, preferred, &entry.Size, 0, "")
	}
	if _, err := db.ExecContext(ensureContext(ctx), `
		UPDATE entries SET size_contribution = ?
		WHERE index_id = ? AND relative_path = ?;
	`, entry.Size, indexID, entry.RelativePath); err != nil {
		return err
	}
	return UpdateParentDirectorySizes(ctx, db, indexID, entry.RelativePath, entry.Size)
}

func removedContributionForPath(ctx context.Context, db dbExecutor, indexID int64, path string) (int64, error) {
	var typ string
	var size, contribution int64
	err := db.QueryRowContext(ensureContext(ctx), `
		SELECT type, size, size_contribution FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, path).Scan(&typ, &size, &contribution)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if typ == "directory" {
		return size, nil
	}
	return contribution, nil
}

func promoteHardlinks(ctx context.Context, db dbExecutor, indexID int64, identities []hardlinkIdentity) error {
	for _, identity := range identities {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, "", nil, 0, ""); err != nil {
			return err
		}
	}
	return nil
}

func reconcileHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity, preferred string, knownSize *int64, scanTime int64) ([]contributionDelta, error) {
	if identity.inode == 0 {
		return nil, nil
	}
	entries, err := loadHardlinkGroup(ctx, db, indexID, identity)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	contributor := selectHardlinkContributor(entries, preferred)
	size := hardlinkGroupSize(entries, contributor, knownSize, scanTime)
	return writeHardlinkGroup(ctx, db, indexID, entries, contributor, size)
}

func loadHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity) ([]hardlinkEntry, error) {
	rows, err := db.QueryContext(ensureContext(ctx), `
		SELECT relative_path, size, size_contribution, last_seen
		FROM entries
		WHERE index_id = ? AND type != 'directory' AND device = ? AND inode = ?
		ORDER BY relative_path;
	`, indexID, identity.device, identity.inode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []hardlinkEntry
	for rows.Next() {
		var entry hardlinkEntry
		if err := rows.Scan(&entry.path, &entry.size, &entry.contribution, &entry.lastSeen); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func selectHardlinkContributor(entries []hardlinkEntry, preferred string) int {
	if preferred != "" {
		for i := range entries {
			if entries[i].path == preferred {
				return i
			}
		}
	}
	for i := range entries {
		if entries[i].contribution != 0 {
			return i
		}
	}
	return 0
}

func hardlinkGroupSize(entries []hardlinkEntry, contributor int, knownSize *int64, scanTime int64) int64 {
	size := entries[contributor].size
	if knownSize != nil {
		return *knownSize
	}
	if scanTime != 0 {
		for i := range entries {
			if entries[i].lastSeen == scanTime {
				return entries[i].size
			}
		}
	}
	return size
}

func writeHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, entries []hardlinkEntry, contributor int, size int64) ([]contributionDelta, error) {
	deltas := make([]contributionDelta, 0, 2)
	for i := range entries {
		contribution := int64(0)
		if i == contributor {
			contribution = size
		}
		delta := contribution - entries[i].contribution
		if entries[i].size == size && delta == 0 {
			continue
		}
		if _, err := db.ExecContext(ensureContext(ctx), `
			UPDATE entries SET size = ?, size_contribution = ?
			WHERE index_id = ? AND relative_path = ?;
		`, size, contribution, indexID, entries[i].path); err != nil {
			return nil, err
		}
		if delta != 0 {
			deltas = append(deltas, contributionDelta{path: entries[i].path, delta: delta})
		}
	}
	return deltas, nil
}

func reconcileAndApplyHardlink(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity, preferred string, knownSize *int64, scanTime int64, subtreeRoot string) error {
	deltas, err := reconcileHardlinkGroup(ctx, db, indexID, identity, preferred, knownSize, scanTime)
	if err != nil {
		return err
	}
	return applyContributionDeltas(ctx, db, indexID, deltas, subtreeRoot)
}

func applyContributionDeltas(ctx context.Context, db dbExecutor, indexID int64, deltas []contributionDelta, subtreeRoot string) error {
	for _, delta := range deltas {
		stopPath := ""
		if subtreeRoot != "" && pathInSubtree(delta.path, subtreeRoot) {
			stopPath = subtreeRoot
		}
		if err := updateParentDirectorySizesThrough(ctx, db, indexID, delta.path, delta.delta, stopPath); err != nil {
			return err
		}
	}
	return nil
}

func pathInSubtree(path, root string) bool {
	root = strings.TrimRight(root, "/")
	if root == "" {
		return strings.HasPrefix(path, "/")
	}
	return path == root || strings.HasPrefix(path, root+"/")
}

// ReconcileHardlinksAfterReindex restores one counted path per hardlink group
// after a partial reindex and updates the affected directory aggregates.
func ReconcileHardlinksAfterReindex(ctx context.Context, db dbExecutor, indexID int64, relativePath string, scanTime int64, snapshot *HardlinkSnapshot) error {
	current, err := hardlinkGroupsUnderPath(ctx, db, indexID, relativePath)
	if err != nil {
		return err
	}
	groups := current
	if snapshot != nil {
		for identity, preferred := range snapshot.groups {
			if _, exists := groups[identity]; !exists || preferred != "" {
				groups[identity] = preferred
			}
		}
	}
	for identity, preferred := range groups {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, preferred, nil, scanTime, relativePath); err != nil {
			return fmt.Errorf("reconcile hardlink device %d inode %d: %w", identity.device, identity.inode, err)
		}
	}
	return nil
}

func promotedHardlinksForDelete(ctx context.Context, db dbExecutor, indexID int64, relativePath string) ([]hardlinkIdentity, error) {
	lo, childLo, hi := subtreeBounds(relativePath)
	rows, err := db.QueryContext(ensureContext(ctx), `
		SELECT DISTINCT inside.device, inside.inode
		FROM entries inside
		WHERE inside.index_id = ? AND inside.type != 'directory'
		  AND inside.inode != 0 AND inside.size_contribution != 0
		  AND inside.relative_path >= ? AND inside.relative_path < ?
		  AND (inside.relative_path = ? OR inside.relative_path >= ?)
		  AND EXISTS (
			SELECT 1 FROM entries outside
			WHERE outside.index_id = inside.index_id
			  AND outside.type != 'directory'
			  AND outside.device = inside.device AND outside.inode = inside.inode
			  AND NOT (outside.relative_path >= ? AND outside.relative_path < ?
				AND (outside.relative_path = ? OR outside.relative_path >= ?))
		  );
	`, indexID, lo, hi, lo, childLo, lo, hi, lo, childLo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var identities []hardlinkIdentity
	for rows.Next() {
		var identity hardlinkIdentity
		if err := rows.Scan(&identity.device, &identity.inode); err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}
