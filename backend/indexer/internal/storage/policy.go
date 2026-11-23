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
	schemaVersion = 3
	schemaTimeout = 30 * time.Second

	// batchSize is how many rows a scan writes in one statement.
	batchSize = 500

	// defaultSearchLimit applies when a caller passes no positive limit.
	defaultSearchLimit = 100

	// ftsMinTermRunes is the trigram size; shorter terms cannot match in FTS.
	ftsMinTermRunes = 3
)
