package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
	"github.com/mordilloSan/LinuxIO/backend/indexer/systemdunit"
)

const (
	EnvConfigFile     = "INDEXER_CONFIG_FILE"
	DefaultConfigPath = "/etc/linuxio/indexer/config.yaml"

	DefaultSearchLimit     = 100
	DefaultSearchMaxLimit  = 100
	DefaultEntriesLimit    = 200
	DefaultEntriesMaxLimit = 200
)

const (
	IntegrityCheckFull  = "full"
	IntegrityCheckQuick = "quick"
	IntegrityCheckOff   = "off"

	DefaultIntegrityCheck = IntegrityCheckFull
)

// Config is the stable configuration shape exposed by the JSON admin API and
// persisted as YAML on disk.
type Config = api.Config

// Patch is used for config-file loading and PUT /config. Pointer fields allow
// false, 0, and empty strings to be intentional values.
type Patch = api.ConfigPatch

func Defaults() Config {
	dbOpts := storage.DefaultOpenOptions()
	return Config{
		IndexPath:            "/",
		IndexName:            "root",
		ExcludePaths:         []string{"/proc", "/dev"},
		IncludeHidden:        true,
		IncludeNetworkMounts: false,
		FreshIndex:           true,
		FTSSearch:            true,
		KeepIndexes:          1,
		IntegrityCheck:       DefaultIntegrityCheck,
		DBPath:               "/var/lib/linuxio/indexer/indexer.db",
		DBBusyTimeout:        dbOpts.BusyTimeout.String(),
		DBJournalMode:        dbOpts.JournalMode,
		DBSynchronous:        dbOpts.Synchronous,
		DBAutoVacuum:         dbOpts.AutoVacuum,
		DBMaxOpenConns:       dbOpts.MaxOpenConns,
		DBMaxIdleConns:       dbOpts.MaxIdleConns,
		DBConnMaxIdleTime:    dbOpts.ConnMaxIdleTime.String(),
		DBStmtCacheSize:      dbOpts.StmtCacheSize,
		SearchDefaultLimit:   DefaultSearchLimit,
		SearchMaxLimit:       DefaultSearchMaxLimit,
		EntriesDefaultLimit:  DefaultEntriesLimit,
		EntriesMaxLimit:      DefaultEntriesMaxLimit,
		SocketPath:           "/run/linuxio/indexer.sock",
		ListenAddr:           "",
		Interval:             "1h0m0s",
		IdleTimeout:          "2m0s",
	}
}

func DefaultPath() string {
	return DefaultConfigPath
}

func PathFromEnvOrDefault() string {
	if path := strings.TrimSpace(os.Getenv(EnvConfigFile)); path != "" {
		return path
	}
	return DefaultPath()
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return Normalize(cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Normalize(cfg)
		}
		return Config{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	patch, err := DecodePatchYAML(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return ApplyPatch(cfg, patch)
}

func DecodePatchJSON(data []byte) (Patch, error) {
	var patch Patch
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&patch); err != nil {
		return Patch{}, err
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Patch{}, fmt.Errorf("unexpected trailing JSON")
	}
	return patch, nil
}

func DecodePatchYAML(data []byte) (Patch, error) {
	probe := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if err := probe.Decode(&document); err != nil {
		return Patch{}, err
	}
	if document == nil {
		return Patch{}, errors.New("YAML document is empty")
	}
	var extra any
	if err := probe.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Patch{}, fmt.Errorf("unexpected trailing YAML: %w", err)
		}
		return Patch{}, fmt.Errorf("multiple YAML documents are not supported")
	}

	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	var patch Patch
	if err := dec.Decode(&patch); err != nil {
		return Patch{}, err
	}
	return patch, nil
}

func ApplyPatch(cfg Config, patch Patch) (Config, error) {
	cfg = applyIndexPatch(cfg, patch)
	cfg = applyDBPatch(cfg, patch)
	cfg = applyLimitPatch(cfg, patch)
	cfg = applyRuntimePatch(cfg, patch)
	return Normalize(cfg)
}

func applyIndexPatch(cfg Config, patch Patch) Config {
	if patch.IndexPath != nil {
		cfg.IndexPath = *patch.IndexPath
	}
	if patch.IndexName != nil {
		cfg.IndexName = *patch.IndexName
	}
	if patch.ExcludePaths != nil {
		cfg.ExcludePaths = append([]string(nil), (*patch.ExcludePaths)...)
	}
	if patch.IncludeHidden != nil {
		cfg.IncludeHidden = *patch.IncludeHidden
	}
	if patch.IncludeNetworkMounts != nil {
		cfg.IncludeNetworkMounts = *patch.IncludeNetworkMounts
	}
	if patch.FreshIndex != nil {
		cfg.FreshIndex = *patch.FreshIndex
	}
	if patch.FTSSearch != nil {
		cfg.FTSSearch = *patch.FTSSearch
	}
	if patch.KeepIndexes != nil {
		cfg.KeepIndexes = *patch.KeepIndexes
	}
	if patch.IntegrityCheck != nil {
		cfg.IntegrityCheck = *patch.IntegrityCheck
	}
	return cfg
}

func applyDBPatch(cfg Config, patch Patch) Config {
	if patch.DBPath != nil {
		cfg.DBPath = *patch.DBPath
	}
	if patch.DBBusyTimeout != nil {
		cfg.DBBusyTimeout = *patch.DBBusyTimeout
	}
	if patch.DBJournalMode != nil {
		cfg.DBJournalMode = *patch.DBJournalMode
	}
	if patch.DBSynchronous != nil {
		cfg.DBSynchronous = *patch.DBSynchronous
	}
	if patch.DBAutoVacuum != nil {
		cfg.DBAutoVacuum = *patch.DBAutoVacuum
	}
	if patch.DBMaxOpenConns != nil {
		cfg.DBMaxOpenConns = *patch.DBMaxOpenConns
	}
	if patch.DBMaxIdleConns != nil {
		cfg.DBMaxIdleConns = *patch.DBMaxIdleConns
	}
	if patch.DBConnMaxIdleTime != nil {
		cfg.DBConnMaxIdleTime = *patch.DBConnMaxIdleTime
	}
	if patch.DBStmtCacheSize != nil {
		cfg.DBStmtCacheSize = *patch.DBStmtCacheSize
	}
	return cfg
}

func applyLimitPatch(cfg Config, patch Patch) Config {
	if patch.SearchDefaultLimit != nil {
		cfg.SearchDefaultLimit = *patch.SearchDefaultLimit
	}
	if patch.SearchMaxLimit != nil {
		cfg.SearchMaxLimit = *patch.SearchMaxLimit
	}
	if patch.EntriesDefaultLimit != nil {
		cfg.EntriesDefaultLimit = *patch.EntriesDefaultLimit
	}
	if patch.EntriesMaxLimit != nil {
		cfg.EntriesMaxLimit = *patch.EntriesMaxLimit
	}
	return cfg
}

func applyRuntimePatch(cfg Config, patch Patch) Config {
	if patch.SocketPath != nil {
		cfg.SocketPath = *patch.SocketPath
	}
	if patch.ListenAddr != nil {
		cfg.ListenAddr = *patch.ListenAddr
	}
	if patch.Interval != nil {
		cfg.Interval = *patch.Interval
	}
	if patch.IdleTimeout != nil {
		cfg.IdleTimeout = *patch.IdleTimeout
	}
	return cfg
}

func Normalize(cfg Config) (Config, error) {
	cfg.IndexPath = strings.TrimSpace(cfg.IndexPath)
	if cfg.IndexPath == "" {
		return Config{}, fmt.Errorf("index_path cannot be empty")
	}

	cfg.IndexName = strings.TrimSpace(cfg.IndexName)
	if cfg.IndexName == "" {
		cfg.IndexName = DeriveIndexName(cfg.IndexPath)
	}

	excludePaths := make([]string, 0, len(cfg.ExcludePaths))
	seenExcludePaths := make(map[string]struct{}, len(cfg.ExcludePaths))
	for _, rawPath := range cfg.ExcludePaths {
		path := filepath.Clean(strings.TrimSpace(rawPath))
		if !filepath.IsAbs(path) {
			return Config{}, fmt.Errorf("exclude_paths entry %q must be absolute", rawPath)
		}
		if path == "/" {
			return Config{}, fmt.Errorf("exclude_paths cannot contain the index root /")
		}
		if _, exists := seenExcludePaths[path]; exists {
			continue
		}
		seenExcludePaths[path] = struct{}{}
		excludePaths = append(excludePaths, path)
	}
	cfg.ExcludePaths = excludePaths

	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	if cfg.DBPath == "" {
		return Config{}, fmt.Errorf("db_path cannot be empty")
	}
	dbOpts, err := DBOpenOptions(cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.DBBusyTimeout = dbOpts.BusyTimeout.String()
	cfg.DBJournalMode = dbOpts.JournalMode
	cfg.DBSynchronous = dbOpts.Synchronous
	cfg.DBAutoVacuum = dbOpts.AutoVacuum
	cfg.DBMaxOpenConns = dbOpts.MaxOpenConns
	cfg.DBMaxIdleConns = dbOpts.MaxIdleConns
	cfg.DBConnMaxIdleTime = dbOpts.ConnMaxIdleTime.String()
	cfg.DBStmtCacheSize = dbOpts.StmtCacheSize

	// "-" is kept as-is: it is the explicit "no unix socket" sentinel, which
	// NewDaemon distinguishes from "" (unset, use the default path).
	// Normalizing it to "" here used to silently re-enable the default socket.
	cfg.SocketPath = strings.TrimSpace(cfg.SocketPath)
	cfg.ListenAddr, err = systemdunit.NormalizeTCPListenAddress(cfg.ListenAddr)
	if err != nil {
		return Config{}, err
	}

	if cfg.KeepIndexes < 0 {
		return Config{}, fmt.Errorf("keep_indexes must be non-negative")
	}
	integrityCheck, err := NormalizeIntegrityCheck(string(cfg.IntegrityCheck))
	if err != nil {
		return Config{}, err
	}
	cfg.IntegrityCheck = api.IndexerIntegrityCheck(integrityCheck)
	if validationErr := validateLimitPair("search", cfg.SearchDefaultLimit, cfg.SearchMaxLimit); validationErr != nil {
		return Config{}, validationErr
	}
	if validationErr := validateLimitPair("entries", cfg.EntriesDefaultLimit, cfg.EntriesMaxLimit); validationErr != nil {
		return Config{}, validationErr
	}

	interval, _, err := NormalizeInterval(cfg.Interval)
	if err != nil {
		return Config{}, err
	}
	cfg.Interval = interval
	idleTimeout, _, err := NormalizeIdleTimeout(cfg.IdleTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.IdleTimeout = idleTimeout
	return cfg, nil
}

// NormalizeIntegrityCheck validates the database check performed before an
// existing database is indexed. An empty value preserves compatibility with
// callers that construct Config values directly and means the safe default.
func NormalizeIntegrityCheck(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return DefaultIntegrityCheck, nil
	}
	switch mode {
	case IntegrityCheckFull, IntegrityCheckQuick, IntegrityCheckOff:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid integrity_check %q: must be full, quick, or off", raw)
	}
}

func validateLimitPair(name string, defaultLimit, maxLimit int) error {
	if defaultLimit < 1 {
		return fmt.Errorf("%s_default_limit must be positive", name)
	}
	if maxLimit < 1 {
		return fmt.Errorf("%s_max_limit must be positive", name)
	}
	if defaultLimit > maxLimit {
		return fmt.Errorf("%s_default_limit cannot exceed %s_max_limit", name, name)
	}
	return nil
}

func DeriveIndexName(path string) string {
	name := strings.ReplaceAll(path, "/", "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "root"
	}
	return name
}

func NormalizeInterval(raw string) (string, time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return "0s", 0, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return "", 0, fmt.Errorf("invalid interval %q: %w", raw, err)
	}
	if interval < 0 {
		return "", 0, fmt.Errorf("interval must be non-negative")
	}
	return interval.String(), interval, nil
}

func ParseInterval(raw string) (time.Duration, error) {
	_, interval, err := NormalizeInterval(raw)
	return interval, err
}

func NormalizeIdleTimeout(raw string) (string, time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return "0s", 0, nil
	}
	idleTimeout, err := time.ParseDuration(raw)
	if err != nil {
		return "", 0, fmt.Errorf("invalid idle_timeout %q: %w", raw, err)
	}
	if idleTimeout < 0 {
		return "", 0, fmt.Errorf("idle_timeout must be non-negative")
	}
	return idleTimeout.String(), idleTimeout, nil
}

func ParseIdleTimeout(raw string) (time.Duration, error) {
	_, idleTimeout, err := NormalizeIdleTimeout(raw)
	return idleTimeout, err
}

func DBOpenOptions(cfg Config) (storage.OpenOptions, error) {
	defaults := storage.DefaultOpenOptions()
	busyTimeout, err := parseDurationWithDefault(cfg.DBBusyTimeout, defaults.BusyTimeout, "db_busy_timeout")
	if err != nil {
		return storage.OpenOptions{}, err
	}
	connMaxIdleTime, err := parseDurationWithDefault(cfg.DBConnMaxIdleTime, defaults.ConnMaxIdleTime, "db_conn_max_idle_time")
	if err != nil {
		return storage.OpenOptions{}, err
	}

	opts := storage.OpenOptions{
		BusyTimeout:     busyTimeout,
		JournalMode:     cfg.DBJournalMode,
		Synchronous:     cfg.DBSynchronous,
		AutoVacuum:      cfg.DBAutoVacuum,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxIdleTime: connMaxIdleTime,
		StmtCacheSize:   cfg.DBStmtCacheSize,
		DisableFTS:      !cfg.FTSSearch,
	}
	return storage.NormalizeOpenOptions(opts)
}

func parseDurationWithDefault(raw string, fallback time.Duration, field string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", field, raw, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	return duration, nil
}

func ApplyEnvOverrides(cfg Config, lookup func(string) (string, bool)) (Config, error) {
	var patch Patch
	applyStringEnvOverrides(&patch, lookup)
	if err := applyBoolEnvOverrides(&patch, lookup); err != nil {
		return Config{}, err
	}
	if err := applyIntEnvOverrides(&patch, lookup); err != nil {
		return Config{}, err
	}
	return ApplyPatch(cfg, patch)
}

type stringEnvOverride struct {
	key string
	set func(*Patch, *string)
}

func applyStringEnvOverrides(patch *Patch, lookup func(string) (string, bool)) {
	overrides := []stringEnvOverride{
		{"INDEXER_PATH", func(p *Patch, v *string) { p.IndexPath = v }},
		{"INDEXER_NAME", func(p *Patch, v *string) { p.IndexName = v }},
		{"INDEXER_INTEGRITY_CHECK", func(p *Patch, v *string) {
			value := api.IndexerIntegrityCheck(*v)
			p.IntegrityCheck = &value
		}},
		{"INDEXER_DB_PATH", func(p *Patch, v *string) { p.DBPath = v }},
		{"INDEXER_DB_BUSY_TIMEOUT", func(p *Patch, v *string) { p.DBBusyTimeout = v }},
		{"INDEXER_DB_JOURNAL_MODE", func(p *Patch, v *string) { p.DBJournalMode = v }},
		{"INDEXER_DB_SYNCHRONOUS", func(p *Patch, v *string) { p.DBSynchronous = v }},
		{"INDEXER_DB_AUTO_VACUUM", func(p *Patch, v *string) { p.DBAutoVacuum = v }},
		{"INDEXER_DB_CONN_MAX_IDLE_TIME", func(p *Patch, v *string) { p.DBConnMaxIdleTime = v }},
		{"INDEXER_SOCKET", func(p *Patch, v *string) { p.SocketPath = v }},
		{"INDEXER_INTERVAL", func(p *Patch, v *string) { p.Interval = v }},
		{"INDEXER_IDLE_TIMEOUT", func(p *Patch, v *string) { p.IdleTimeout = v }},
		{"INDEXER_LISTEN_ADDR", func(p *Patch, v *string) { p.ListenAddr = v }},
	}
	for _, override := range overrides {
		if value, ok := lookup(override.key); ok {
			override.set(patch, &value)
		}
	}
}

type boolEnvOverride struct {
	key string
	set func(*Patch, *bool)
}

func applyBoolEnvOverrides(patch *Patch, lookup func(string) (string, bool)) error {
	overrides := []boolEnvOverride{
		{"INDEXER_INCLUDE_HIDDEN", func(p *Patch, v *bool) { p.IncludeHidden = v }},
		{"INDEXER_INCLUDE_NETWORK_MOUNTS", func(p *Patch, v *bool) { p.IncludeNetworkMounts = v }},
		{"INDEXER_FRESH", func(p *Patch, v *bool) { p.FreshIndex = v }},
		{"INDEXER_FTS_SEARCH", func(p *Patch, v *bool) { p.FTSSearch = v }},
	}
	for _, override := range overrides {
		if err := applyBoolEnvOverride(patch, lookup, override); err != nil {
			return err
		}
	}
	return nil
}

func applyBoolEnvOverride(patch *Patch, lookup func(string) (string, bool), override boolEnvOverride) error {
	value, ok := lookup(override.key)
	if !ok {
		return nil
	}
	parsed, err := parseBoolEnv(override.key, value)
	if err != nil {
		return err
	}
	override.set(patch, &parsed)
	return nil
}

type intEnvOverride struct {
	key string
	set func(*Patch, *int)
}

func applyIntEnvOverrides(patch *Patch, lookup func(string) (string, bool)) error {
	overrides := []intEnvOverride{
		{"INDEXER_KEEP_INDEXES", func(p *Patch, v *int) { p.KeepIndexes = v }},
		{"INDEXER_DB_MAX_OPEN_CONNS", func(p *Patch, v *int) { p.DBMaxOpenConns = v }},
		{"INDEXER_DB_MAX_IDLE_CONNS", func(p *Patch, v *int) { p.DBMaxIdleConns = v }},
		{"INDEXER_DB_STMT_CACHE_SIZE", func(p *Patch, v *int) { p.DBStmtCacheSize = v }},
		{"INDEXER_SEARCH_DEFAULT_LIMIT", func(p *Patch, v *int) { p.SearchDefaultLimit = v }},
		{"INDEXER_SEARCH_MAX_LIMIT", func(p *Patch, v *int) { p.SearchMaxLimit = v }},
		{"INDEXER_ENTRIES_DEFAULT_LIMIT", func(p *Patch, v *int) { p.EntriesDefaultLimit = v }},
		{"INDEXER_ENTRIES_MAX_LIMIT", func(p *Patch, v *int) { p.EntriesMaxLimit = v }},
	}
	for _, override := range overrides {
		value, ok := lookup(override.key)
		if !ok {
			continue
		}
		parsed, err := parseNonNegativeIntEnv(override.key, value)
		if err != nil {
			return err
		}
		override.set(patch, &parsed)
	}
	return nil
}

func parseNonNegativeIntEnv(key, raw string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a non-negative integer", key, raw)
	}
	return parsed, nil
}

func Save(path string, cfg Config) error {
	cfg, err := Normalize(cfg)
	if err != nil {
		return err
	}
	data, err := Format(cfg)
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return fmt.Errorf("config file path is disabled")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return atomicfile.WriteFile(path, data, 0o644)
}

func Format(cfg Config) ([]byte, error) {
	cfg, err := Normalize(cfg)
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func parseBoolEnv(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s %q: must be true or false", name, raw)
	}
}
