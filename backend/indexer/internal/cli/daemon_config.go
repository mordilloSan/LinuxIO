package cli

import (
	"flag"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

// indexFlagValues contains the private index worker configuration flags.
type indexFlagValues struct {
	configPath           *string
	indexPath            *string
	indexName            *string
	excludePaths         *[]string
	includeHidden        *bool
	includeNetworkMounts *bool
	freshIndex           *bool
	ftsSearch            *bool
	keepIndexes          *int
	integrityCheck       *string
	dbPath               *string
	dbBusyTimeout        *string
	dbJournalMode        *string
	dbSynchronous        *string
	dbAutoVacuum         *string
	dbMaxOpenConns       *int
	dbMaxIdleConns       *int
	dbConnMaxIdleTime    *string
	dbStmtCacheSize      *int
}

func registerIndexFlags(fs *flag.FlagSet) indexFlagValues {
	defaults := configfile.Defaults()
	excludePaths := append([]string(nil), defaults.ExcludePaths...)
	excludePathsSet := false
	fs.Func("exclude-path", "Absolute path to exclude (repeatable; use an empty value to clear)", func(path string) error {
		if !excludePathsSet {
			excludePaths = nil
			excludePathsSet = true
		}
		if path != "" {
			excludePaths = append(excludePaths, path)
		}
		return nil
	})
	values := indexFlagValues{
		configPath:           fs.String("config-file", configfile.PathFromEnvOrDefault(), "YAML config file path"),
		indexPath:            fs.String("path", "", "Path to index"),
		indexName:            fs.String("name", "", "Index name (defaults to sanitized path)"),
		excludePaths:         &excludePaths,
		includeHidden:        fs.Bool("include-hidden", false, "Include hidden files and directories"),
		includeNetworkMounts: fs.Bool("include-network-mounts", false, "Include network/external mounts such as NFS, SMB, and CIFS"),
		freshIndex:           fs.Bool("fresh", true, "Rebuild the index from scratch in a new generation on each full index"),
		ftsSearch:            fs.Bool("fts-search", defaults.FTSSearch, "Maintain the FTS5 search index (fast substring search; slows full scans; needs a binary built with sqlite_fts5)"),
		keepIndexes:          fs.Int("keep-indexes", defaults.KeepIndexes, "Most recent index generations to keep automatically after indexing (0 disables automatic pruning)"),
		integrityCheck:       fs.String("integrity-check", string(defaults.IntegrityCheck), "SQLite integrity check before indexing an existing database (full, quick, off)"),
		dbPath:               fs.String("db-path", "", "SQLite database path"),
		dbBusyTimeout:        fs.String("db-busy-timeout", defaults.DBBusyTimeout, "SQLite busy timeout"),
		dbJournalMode:        fs.String("db-journal-mode", defaults.DBJournalMode, "SQLite journal mode"),
		dbSynchronous:        fs.String("db-synchronous", defaults.DBSynchronous, "SQLite synchronous setting"),
		dbAutoVacuum:         fs.String("db-auto-vacuum", defaults.DBAutoVacuum, "SQLite auto_vacuum setting"),
		dbMaxOpenConns:       fs.Int("db-max-open-conns", defaults.DBMaxOpenConns, "SQLite max open connections"),
		dbMaxIdleConns:       fs.Int("db-max-idle-conns", defaults.DBMaxIdleConns, "SQLite max idle connections"),
		dbConnMaxIdleTime:    fs.String("db-conn-max-idle-time", defaults.DBConnMaxIdleTime, "SQLite connection max idle time"),
		dbStmtCacheSize:      fs.Int("db-stmt-cache-size", defaults.DBStmtCacheSize, "SQLite prepared statements cached per connection (0 disables)"),
	}
	return values
}

func applyIndexFlagOverrides(fs *flag.FlagSet, cfg configfile.Config, values indexFlagValues) (configfile.Config, error) {
	var patch configfile.Patch
	collectIndexFlagOverrides(fs, values, &patch)
	return configfile.ApplyPatch(cfg, patch)
}

func collectIndexFlagOverrides(fs *flag.FlagSet, values indexFlagValues, patch *configfile.Patch) {
	if flagWasSet(fs, "path") {
		patch.IndexPath = values.indexPath
	}
	if flagWasSet(fs, "name") {
		patch.IndexName = values.indexName
	}
	if flagWasSet(fs, "exclude-path") {
		paths := append([]string(nil), (*values.excludePaths)...)
		patch.ExcludePaths = &paths
	}
	if flagWasSet(fs, "include-hidden") {
		patch.IncludeHidden = values.includeHidden
	}
	if flagWasSet(fs, "include-network-mounts") {
		patch.IncludeNetworkMounts = values.includeNetworkMounts
	}
	if flagWasSet(fs, "fresh") {
		patch.FreshIndex = values.freshIndex
	}
	if flagWasSet(fs, "fts-search") {
		patch.FTSSearch = values.ftsSearch
	}
	if flagWasSet(fs, "keep-indexes") {
		patch.KeepIndexes = values.keepIndexes
	}
	if flagWasSet(fs, "integrity-check") {
		patch.IntegrityCheck = indexerIntegrityCheck(values.integrityCheck)
	}
	if flagWasSet(fs, "db-path") {
		patch.DBPath = values.dbPath
	}
	if flagWasSet(fs, "db-busy-timeout") {
		patch.DBBusyTimeout = values.dbBusyTimeout
	}
	if flagWasSet(fs, "db-journal-mode") {
		patch.DBJournalMode = values.dbJournalMode
	}
	if flagWasSet(fs, "db-synchronous") {
		patch.DBSynchronous = values.dbSynchronous
	}
	if flagWasSet(fs, "db-auto-vacuum") {
		patch.DBAutoVacuum = values.dbAutoVacuum
	}
	if flagWasSet(fs, "db-max-open-conns") {
		patch.DBMaxOpenConns = values.dbMaxOpenConns
	}
	if flagWasSet(fs, "db-max-idle-conns") {
		patch.DBMaxIdleConns = values.dbMaxIdleConns
	}
	if flagWasSet(fs, "db-conn-max-idle-time") {
		patch.DBConnMaxIdleTime = values.dbConnMaxIdleTime
	}
	if flagWasSet(fs, "db-stmt-cache-size") {
		patch.DBStmtCacheSize = values.dbStmtCacheSize
	}
}

func indexerIntegrityCheck(value *string) *api.IndexerIntegrityCheck {
	if value == nil {
		return nil
	}
	converted := api.IndexerIntegrityCheck(*value)
	return &converted
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
