package configfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IndexPath != "/" || cfg.IndexName != "root" || !slices.Equal(cfg.ExcludePaths, []string{"/proc", "/dev"}) || cfg.IdleTimeout != "2m0s" || cfg.KeepIndexes != 1 || cfg.IntegrityCheck != IntegrityCheckFull {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestLoadAppliesPartialConfigWithFalseValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "indexer.yaml")
	content := `index_path: /srv/files
index_name: ""
include_hidden: false
include_network_mounts: true
exclude_paths: [/proc/, /srv/cache, /srv/cache]
fresh_index: false
keep_indexes: 2
search_default_limit: 150
search_max_limit: 300
entries_default_limit: 500
entries_max_limit: 1000
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IndexPath != "/srv/files" || cfg.IndexName != "srv_files" {
		t.Fatalf("unexpected identity fields: %#v", cfg)
	}
	if cfg.IncludeHidden || !cfg.IncludeNetworkMounts || cfg.FreshIndex {
		t.Fatalf("false values were not preserved: %#v", cfg)
	}
	if !slices.Equal(cfg.ExcludePaths, []string{"/proc", "/srv/cache"}) {
		t.Fatalf("unexpected exclude_paths: %#v", cfg.ExcludePaths)
	}
	if !cfg.FTSSearch {
		t.Fatalf("fts_search should default to true when absent: %#v", cfg)
	}
	if cfg.KeepIndexes != 2 {
		t.Fatalf("unexpected normalized fields: %#v", cfg)
	}
	if cfg.IntegrityCheck != IntegrityCheckFull {
		t.Fatalf("integrity_check should default to full when absent: %#v", cfg)
	}
	if cfg.SearchDefaultLimit != 150 || cfg.SearchMaxLimit != 300 || cfg.EntriesDefaultLimit != 500 || cfg.EntriesMaxLimit != 1000 {
		t.Fatalf("unexpected limit fields: %#v", cfg)
	}
}

func TestSaveWritesNormalizedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "indexer.yaml")
	cfg := Defaults()
	cfg.IdleTimeout = "1m"

	if saveErr := Save(path, cfg); saveErr != nil {
		t.Fatalf("Save: %v", saveErr)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(content), "idle_timeout: 1m0s") {
		t.Fatalf("expected normalized idle timeout, got:\n%s", content)
	}
}

func TestLoadRejectsUnknownAndMultipleYAMLDocuments(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":      "unknown: true\n",
		"multiple documents": "index_path: /data\n---\nindex_path: /other\n",
		"empty document":     "",
		"null document":      "null\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "indexer.yaml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load accepted invalid YAML")
			}
		})
	}
}

func TestDecodePatchJSONRejectsUnknownAndTrailingData(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":          `{"unknown":true}`,
		"systemd listener field": `{"listen_addr":":8080"}`,
		"trailing object":        `{"index_path":"/data"}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePatchJSON([]byte(content)); err == nil {
				t.Fatal("DecodePatchJSON accepted invalid JSON")
			}
		})
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	env := map[string]string{
		"INDEXER_PATH":                   "/data",
		"INDEXER_INCLUDE_HIDDEN":         "false",
		"INDEXER_INCLUDE_NETWORK_MOUNTS": "yes",
		"INDEXER_KEEP_INDEXES":           "3",
		"INDEXER_INTEGRITY_CHECK":        "quick",
		"INDEXER_DB_SYNCHRONOUS":         "normal",
		"INDEXER_DB_MAX_OPEN_CONNS":      "7",
		"INDEXER_SEARCH_DEFAULT_LIMIT":   "25",
		"INDEXER_SEARCH_MAX_LIMIT":       "250",
		"INDEXER_ENTRIES_DEFAULT_LIMIT":  "75",
		"INDEXER_ENTRIES_MAX_LIMIT":      "750",
	}
	cfg, err := ApplyEnvOverrides(Defaults(), func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	})
	if err != nil {
		t.Fatalf("ApplyEnvOverrides: %v", err)
	}
	if cfg.IndexPath != "/data" || cfg.IncludeHidden || !cfg.IncludeNetworkMounts || cfg.KeepIndexes != 3 || cfg.IntegrityCheck != IntegrityCheckQuick || cfg.DBSynchronous != "NORMAL" || cfg.DBMaxOpenConns != 7 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.SearchDefaultLimit != 25 || cfg.SearchMaxLimit != 250 || cfg.EntriesDefaultLimit != 75 || cfg.EntriesMaxLimit != 750 {
		t.Fatalf("unexpected limit config: %#v", cfg)
	}
}

func TestDBOpenOptionsValidation(t *testing.T) {
	cfg := Defaults()
	cfg.DBJournalMode = "WAL; DROP TABLE entries"
	if _, err := Normalize(cfg); err == nil {
		t.Fatalf("Normalize accepted invalid journal mode")
	}
}

func TestNormalizeRejectsNegativeIdleTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.IdleTimeout = "-1s"
	if _, err := Normalize(cfg); err == nil {
		t.Fatal("Normalize accepted a negative idle timeout")
	}
}

func TestNormalizeRejectsInvalidExcludePaths(t *testing.T) {
	for _, path := range []string{"relative", "/"} {
		cfg := Defaults()
		cfg.ExcludePaths = []string{path}
		if _, err := Normalize(cfg); err == nil {
			t.Fatalf("Normalize accepted exclude path %q", path)
		}
	}
}

func TestNormalizeIntegrityCheck(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty uses safe default", raw: "", want: IntegrityCheckFull},
		{name: "full", raw: " full ", want: IntegrityCheckFull},
		{name: "quick case insensitive", raw: "QUICK", want: IntegrityCheckQuick},
		{name: "off", raw: "off", want: IntegrityCheckOff},
		{name: "invalid", raw: "fast", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeIntegrityCheck(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizeIntegrityCheck(%q) returned nil error", test.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeIntegrityCheck(%q): %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("NormalizeIntegrityCheck(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
