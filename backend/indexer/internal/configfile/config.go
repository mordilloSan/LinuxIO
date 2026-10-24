package configfile

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

const DefaultConfigPath = version.ConfigDir + "/indexer/config.yaml"

// mandatoryExcludePaths are never indexed and cannot be removed through the
// file or the API, so virtual filesystems and the database cannot index
// themselves.
var mandatoryExcludePaths = []string{"/proc", "/dev", "/sys", version.DataDir + "/indexer", "/mnt/c"}

// Config and Patch are the operator settings. The on-disk YAML and the /config
// wire body deliberately share one shape, so they are the api types rather
// than a second struct with a converter.
type Config = api.IndexerConfig
type Patch = api.IndexerConfigPatch

func Defaults() Config {
	return Config{
		IncludeNetworkMounts: false,
	}
}

func DefaultPath() string { return DefaultConfigPath }

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
	if err := json.Unmarshal(data, &patch, json.RejectUnknownMembers(true)); err != nil {
		return Patch{}, err
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
	if patch.ExcludePaths != nil {
		cfg.ExcludePaths = append([]string(nil), (*patch.ExcludePaths)...)
	}
	if patch.IncludeNetworkMounts != nil {
		cfg.IncludeNetworkMounts = *patch.IncludeNetworkMounts
	}
	return Normalize(cfg)
}

func Normalize(cfg Config) (Config, error) {
	excludePaths := make([]string, 0, len(cfg.ExcludePaths))
	seen := make(map[string]struct{}, len(cfg.ExcludePaths))
	for _, rawPath := range cfg.ExcludePaths {
		path := filepath.Clean(strings.TrimSpace(rawPath))
		if !filepath.IsAbs(path) {
			return Config{}, fmt.Errorf("exclude_paths entry %q must be absolute", rawPath)
		}
		if slices.Contains(mandatoryExcludePaths, path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		excludePaths = append(excludePaths, path)
	}
	cfg.ExcludePaths = excludePaths
	return cfg, nil
}

// EffectiveExcludePaths combines operator exclusions with paths that must
// never be indexed. The latter are intentionally not persisted in Config.
func EffectiveExcludePaths(cfg Config) []string {
	paths := append([]string(nil), mandatoryExcludePaths...)
	paths = append(paths, cfg.ExcludePaths...)
	return paths
}

func Save(path string, cfg Config) error {
	data, err := Format(cfg)
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return fmt.Errorf("config file path is disabled")
	}
	if err := atomicfile.Write(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
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
