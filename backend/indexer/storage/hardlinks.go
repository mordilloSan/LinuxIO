package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
	valid        bool
}

type hardlinkReconcileOptions struct {
	preferred   string
	knownSize   *int64
	scanTime    int64
	subtreeRoot string
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
		return nil, fmt.Errorf("snapshot hardlink groups: %w", err)
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
		SELECT g.device, g.inode, COALESCE(contributor.relative_path, '')
		FROM touched_groups g
		LEFT JOIN entries contributor
		  ON contributor.index_id = ? AND contributor.type != 'directory'
		 AND contributor.device = g.device AND contributor.inode = g.inode
		 AND contributor.size_contribution != 0
		ORDER BY g.device, g.inode, contributor.relative_path;
	`, indexID, lo, hi, lo, childLo, indexID, indexID)
	if err != nil {
		return nil, fmt.Errorf("query hardlink groups under %q: %w", relativePath, err)
	}
	defer rows.Close()

	groups := make(map[hardlinkIdentity]string)
	for rows.Next() {
		var identity hardlinkIdentity
		var path string
		if err := rows.Scan(&identity.device, &identity.inode, &path); err != nil {
			return nil, fmt.Errorf("scan hardlink group under %q: %w", relativePath, err)
		}
		if _, exists := groups[identity]; !exists {
			groups[identity] = ""
		}
		if path != "" && groups[identity] == "" && hardlinkPathMatches(path, identity) {
			groups[identity] = path
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hardlink groups under %q: %w", relativePath, err)
	}
	return groups, nil
}

func currentHardlinkContributor(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity) (string, error) {
	if identity.inode == 0 {
		return "", nil
	}
	rows, err := db.QueryContext(ensureContext(ctx), `
		SELECT relative_path
		FROM entries
		WHERE index_id = ? AND type != 'directory'
		  AND device = ? AND inode = ? AND size_contribution != 0
		ORDER BY relative_path
	`, indexID, identity.device, identity.inode)
	if err != nil {
		return "", fmt.Errorf("query current hardlink contributor: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return "", fmt.Errorf("scan current hardlink contributor: %w", err)
		}
		if hardlinkPathMatches(path, identity) {
			return path, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate current hardlink contributors: %w", err)
	}
	return "", nil
}

func hardlinkPathMatches(path string, identity hardlinkIdentity) bool {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return false
	}
	entry := indexing.EntryFromFileInfo(path, info)
	return entry.Type != "directory" && int64(entry.Device) == identity.device && int64(entry.Inode) == identity.inode
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
	if err != nil {
		return stored, fmt.Errorf("query stored accounting for %q: %w", path, err)
	}
	return stored, nil
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
			return fmt.Errorf("remove prior entry contribution: %w", err)
		}
	}
	entry.SizeContribution = entry.Size
	if preferredNew != "" && preferredNew != entry.RelativePath {
		entry.SizeContribution = 0
	}
	if _, err := UpdateEntry(ctx, db, indexID, entry); err != nil {
		return fmt.Errorf("upsert entry accounting: %w", err)
	}
	if entry.SizeContribution != 0 {
		if err := UpdateParentDirectorySizes(ctx, db, indexID, entry.RelativePath, entry.SizeContribution); err != nil {
			return fmt.Errorf("add entry contribution: %w", err)
		}
	}
	if old.identity != newIdentity {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, old.identity, hardlinkReconcileOptions{preferred: preferredOld}); err != nil {
			return fmt.Errorf("reconcile prior hardlink group: %w", err)
		}
	}
	if newIdentity.inode == 0 {
		return nil
	}
	preferred := preferredNew
	if entry.SizeContribution != 0 {
		preferred = entry.RelativePath
	}
	return reconcileAndApplyHardlink(ctx, db, indexID, newIdentity, hardlinkReconcileOptions{
		preferred: preferred,
		knownSize: &entry.Size,
	})
}

func removedContributionForPath(ctx context.Context, db dbExecutor, indexID int64, path string) (int64, error) {
	var typ string
	var size, contribution int64
	err := db.QueryRowContext(ensureContext(ctx), `
		SELECT type, size, size_contribution FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, path).Scan(&typ, &size, &contribution)
	if errors.Is(err, sql.ErrNoRows) {
		target := indexing.NormalizeIndexPath(path)
		lo, childLo, hi := subtreeBounds(target)
		err = db.QueryRowContext(ensureContext(ctx), `
			SELECT COALESCE(SUM(CASE WHEN entry.type = 'directory' THEN entry.size ELSE entry.size_contribution END), 0)
			FROM entries entry
			WHERE entry.index_id = ?
			  AND entry.relative_path >= ? AND entry.relative_path < ?
			  AND (entry.relative_path = ? OR entry.relative_path >= ?)
			  AND NOT EXISTS (
				SELECT 1 FROM entries parent
				WHERE parent.index_id = entry.index_id AND parent.type = 'directory'
				  AND parent.relative_path >= ? AND parent.relative_path < ?
				  AND (parent.relative_path = ? OR parent.relative_path >= ?)
				  AND parent.relative_path != entry.relative_path
				  AND (parent.relative_path = '/'
					OR substr(entry.relative_path, 1, length(parent.relative_path) + 1) = parent.relative_path || '/')
			  );
		`, indexID, lo, hi, lo, childLo, lo, hi, lo, childLo).Scan(&size)
		if err != nil {
			return 0, fmt.Errorf("sum stored roots below missing path: %w", err)
		}
		return size, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query removed contribution for %q: %w", path, err)
	}
	if typ == "directory" {
		return size, nil
	}
	return contribution, nil
}

func promoteHardlinks(ctx context.Context, db dbExecutor, indexID int64, identities []hardlinkIdentity) error {
	for _, identity := range identities {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, hardlinkReconcileOptions{}); err != nil {
			return fmt.Errorf("promote hardlink device %d inode %d: %w", identity.device, identity.inode, err)
		}
	}
	return nil
}

func reconcileHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity, opts hardlinkReconcileOptions) ([]contributionDelta, error) {
	if identity.inode == 0 {
		return nil, nil
	}
	entries, err := loadHardlinkGroup(ctx, db, indexID, identity)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	contributor := selectHardlinkContributor(entries, opts.preferred)
	size := hardlinkGroupSize(entries, contributor, opts.knownSize, opts.scanTime)
	return writeHardlinkGroup(ctx, db, indexID, identity, entries, contributor, size)
}

func loadHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity) ([]hardlinkEntry, error) {
	rows, err := db.QueryContext(ensureContext(ctx), `
		SELECT relative_path, size, size_contribution, last_seen
		FROM entries
		WHERE index_id = ? AND type != 'directory' AND device = ? AND inode = ?
		ORDER BY relative_path;
	`, indexID, identity.device, identity.inode)
	if err != nil {
		return nil, fmt.Errorf("query hardlink group: %w", err)
	}
	defer rows.Close()

	var entries []hardlinkEntry
	for rows.Next() {
		var entry hardlinkEntry
		if err := rows.Scan(&entry.path, &entry.size, &entry.contribution, &entry.lastSeen); err != nil {
			return nil, fmt.Errorf("scan hardlink group: %w", err)
		}
		entry.valid = hardlinkPathMatches(entry.path, identity)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hardlink group: %w", err)
	}
	return entries, nil
}

func selectHardlinkContributor(entries []hardlinkEntry, preferred string) int {
	if preferred != "" {
		for i := range entries {
			if entries[i].valid && entries[i].path == preferred {
				return i
			}
		}
	}
	for i := range entries {
		if entries[i].valid && entries[i].contribution != 0 {
			return i
		}
	}
	for i := range entries {
		if entries[i].valid {
			return i
		}
	}
	return -1
}

func hardlinkGroupSize(entries []hardlinkEntry, contributor int, knownSize *int64, scanTime int64) int64 {
	if contributor < 0 {
		return 0
	}
	if knownSize != nil {
		return *knownSize
	}
	if scanTime != 0 {
		for i := range entries {
			if entries[i].valid && entries[i].lastSeen == scanTime {
				return entries[i].size
			}
		}
	}
	return entries[contributor].size
}

func writeHardlinkGroup(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity, entries []hardlinkEntry, contributor int, size int64) ([]contributionDelta, error) {
	deltas := make([]contributionDelta, 0, 2)
	contributorPath := ""
	if contributor >= 0 && entries[contributor].valid {
		contributorPath = entries[contributor].path
	}
	changed := false
	validPaths := make([]string, 0, len(entries))
	for i := range entries {
		if entries[i].valid {
			validPaths = append(validPaths, entries[i].path)
		}
		contribution := int64(0)
		if entries[i].path == contributorPath {
			contribution = size
		}
		delta := contribution - entries[i].contribution
		desiredSize := entries[i].size
		if entries[i].valid {
			desiredSize = size
		}
		changed = changed || entries[i].size != desiredSize || delta != 0
		if delta != 0 {
			deltas = append(deltas, contributionDelta{path: entries[i].path, delta: delta})
		}
	}
	if !changed {
		return deltas, nil
	}
	sizeExpression := "size"
	args := make([]any, 0, len(validPaths)+6)
	if len(validPaths) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(validPaths)), ",")
		sizeExpression = fmt.Sprintf("CASE WHEN relative_path IN (%s) THEN ? ELSE size END", placeholders)
		for _, path := range validPaths {
			args = append(args, path)
		}
		args = append(args, size)
	}
	args = append(args, contributorPath, size, indexID, identity.device, identity.inode)
	query := fmt.Sprintf(`
		UPDATE entries
		SET size = %s,
			size_contribution = CASE WHEN relative_path = ? THEN ? ELSE 0 END
		WHERE index_id = ? AND type != 'directory' AND device = ? AND inode = ?;
	`, sizeExpression)
	if _, err := db.ExecContext(ensureContext(ctx), query, args...); err != nil {
		return nil, fmt.Errorf("update hardlink group: %w", err)
	}
	return deltas, nil
}

func reconcileAndApplyHardlink(ctx context.Context, db dbExecutor, indexID int64, identity hardlinkIdentity, opts hardlinkReconcileOptions) error {
	deltas, err := reconcileHardlinkGroup(ctx, db, indexID, identity, opts)
	if err != nil {
		return fmt.Errorf("reconcile hardlink group: %w", err)
	}
	if err := applyContributionDeltas(ctx, db, indexID, deltas, opts.subtreeRoot); err != nil {
		return fmt.Errorf("apply hardlink contribution deltas: %w", err)
	}
	return nil
}

func applyContributionDeltas(ctx context.Context, db dbExecutor, indexID int64, deltas []contributionDelta, subtreeRoot string) error {
	for _, delta := range deltas {
		stopPath := ""
		if subtreeRoot != "" && pathInSubtree(delta.path, subtreeRoot) {
			stopPath = subtreeRoot
		}
		if err := updateParentDirectorySizesThrough(ctx, db, indexID, delta.path, delta.delta, stopPath); err != nil {
			return fmt.Errorf("update parent sizes for %q: %w", delta.path, err)
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
	groups, err := hardlinkGroupsUnderPath(ctx, db, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("load current hardlink groups: %w", err)
	}
	if snapshot != nil {
		for identity, preferred := range snapshot.groups {
			if _, exists := groups[identity]; !exists || preferred != "" {
				groups[identity] = preferred
			}
		}
	}
	for identity, preferred := range groups {
		if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, hardlinkReconcileOptions{
			preferred:   preferred,
			scanTime:    scanTime,
			subtreeRoot: relativePath,
		}); err != nil {
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
		return nil, fmt.Errorf("query hardlinks promoted by deleting %q: %w", relativePath, err)
	}
	defer rows.Close()

	var identities []hardlinkIdentity
	for rows.Next() {
		var identity hardlinkIdentity
		if err := rows.Scan(&identity.device, &identity.inode); err != nil {
			return nil, fmt.Errorf("scan promoted hardlink identity: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate promoted hardlink identities: %w", err)
	}
	return identities, nil
}
