package storage

import (
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// Fixed storage policy. None of these values is an operator setting.
const (
	// DefaultDBPath is the installed cache location. There is no setting or
	// flag for it; tests pass temporary paths straight to Open.
	DefaultDBPath = version.DataDir + "/indexer/indexer.db"

	// defaultDBPath is the relative fallback used when Open gets an empty path.
	defaultDBPath = "indexer.db"

	// schemaVersion is stored in PRAGMA user_version. The cache is rebuildable,
	// so a mismatch is quarantined and recreated rather than migrated.
	schemaVersion = 2
	schemaTimeout = 30 * time.Second

	// The streaming writer flushes a batch at batchSize rows or batchTimeout,
	// whichever comes first. defaultWriterBuffer sizes its entry channel.
	batchSize           = 500
	batchTimeout        = time.Second
	defaultWriterBuffer = 1000

	// defaultSearchLimit applies when a caller passes no positive limit.
	defaultSearchLimit = 100

	// ftsMinTermRunes is the trigram size; shorter terms cannot match in FTS.
	ftsMinTermRunes = 3

	// ftsPruneBypassEntries is the prune size above which the per-row FTS
	// delete triggers are bypassed in favour of one rebuild.
	ftsPruneBypassEntries = 50000
)
