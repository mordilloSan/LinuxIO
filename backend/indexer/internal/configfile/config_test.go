package configfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadAndSaveReducedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Config{ExcludePaths: []string{"/srv/cache", "/srv/cache"}, IncludeNetworkMounts: true}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(loaded.ExcludePaths, []string{"/srv/cache"}) || !loaded.IncludeNetworkMounts {
		t.Fatalf("loaded config = %+v", loaded)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, removed := range []string{"index_name", "index_path", "db_path", "fresh_index", "keep_indexes", "idle_timeout", "include_hidden"} {
		if strings.Contains(string(data), removed) {
			t.Fatalf("saved config contains removed field %q: %s", removed, data)
		}
	}
}

func TestDecodePatchRejectsUnknownFields(t *testing.T) {
	if _, err := DecodePatchJSON([]byte(`{"db_path":"/tmp/index.db"}`)); err == nil {
		t.Fatal("DecodePatchJSON accepted removed field")
	}
	if _, err := DecodePatchYAML([]byte("listen_addr: :8080\n")); err == nil {
		t.Fatal("DecodePatchYAML accepted removed field")
	}
}

func TestNormalizeRejectsUnsafePaths(t *testing.T) {
	if _, err := Normalize(Config{ExcludePaths: []string{"relative"}}); err == nil {
		t.Fatal("Normalize accepted relative exclusion")
	}
}

func TestEffectiveExcludePathsAlwaysProtectsManagedPaths(t *testing.T) {
	cfg, err := Normalize(Config{ExcludePaths: []string{"/srv/cache", "/proc"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/proc", "/dev", "/sys", "/var/lib/linuxio/indexer", "/srv/cache"} {
		if !slices.Contains(EffectiveExcludePaths(cfg), path) {
			t.Fatalf("effective exclusions missing %q", path)
		}
	}
}

func TestSaveRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := Save(link, Defaults()); err == nil {
		t.Fatal("Save accepted symlink destination")
	}
}
