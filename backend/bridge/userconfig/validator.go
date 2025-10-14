package userconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
)

// repairConfig loads cfgPath, validates keys/values, and rewrites only if needed.
// If the YAML cannot be parsed at all, it rewrites full defaults using base.
func repairConfig(cfgPath, base string) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	// 1) Attempt to parse into struct (lenient; we’ll detect unknown keys separately)
	var cfg Settings
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		logger.Warnf("config parse failed, rewriting defaults: %v", err)
		return writeConfig(cfgPath, base)
	}

	// 2) Detect unknown keys (top-level + nested) based on actual struct tags
	unknown := detectUnknownKeys(raw)

	// 3) Sanitize fields (repair invalid/missing values to defaults)
	changed, fixes := sanitizeSettings(&cfg, base)

	// 4) If anything changed or unknown keys exist, rewrite repaired config
	if changed || len(unknown) > 0 {
		if len(unknown) > 0 {
			logger.Warnf("unknown config keys will be removed on rewrite: %s", strings.Join(unknown, ", "))
		}
		if len(fixes) > 0 {
			logger.Warnf("config repairs applied: %s", strings.Join(fixes, "; "))
		}
		return writeConfigFrom(cfgPath, cfg)
	}

	// Nothing to change
	return nil
}

// sanitizeSettings fixes invalid/missing fields in-place and returns whether anything changed.
func sanitizeSettings(cfg *Settings, base string) (changed bool, fixes []string) {
	// Defaults used for repair
	def := DefaultSettings(base)

	// Theme: LIGHT or DARK
	t := strings.ToUpper(strings.TrimSpace(cfg.AppSettings.Theme))
	if t != "LIGHT" && t != "DARK" {
		cfg.AppSettings.Theme = def.AppSettings.Theme
		changed = true
		fixes = append(fixes, "appSettings.theme=default("+def.AppSettings.Theme+")")
	}

	// PrimaryColor: must be valid CSS color
	if !IsValidCSSColor(cfg.AppSettings.PrimaryColor) {
		cfg.AppSettings.PrimaryColor = def.AppSettings.PrimaryColor
		changed = true
		fixes = append(fixes, "appSettings.primaryColor=default("+def.AppSettings.PrimaryColor+")")
	}

	// SidebarCollapsed is bool; zero-value is fine (no repair).

	// Docker.Folder: must be absolute; if empty/invalid, set default <base>/docker
	if p := strings.TrimSpace(cfg.Docker.Folder); p == "" || !filepath.IsAbs(p) {
		cfg.Docker.Folder = def.Docker.Folder
		changed = true
		fixes = append(fixes, "docker.folder=default("+def.Docker.Folder+")")
	} else {
		// normalize for consistency
		cleaned := filepath.Clean(p)
		if cleaned != cfg.Docker.Folder {
			cfg.Docker.Folder = cleaned
			changed = true
			fixes = append(fixes, "docker.folder normalized")
		}
	}

	// Optional strictness: if Docker.Folder exists AND is a file, repair
	if fi, err := os.Stat(cfg.Docker.Folder); err == nil && !fi.IsDir() {
		cfg.Docker.Folder = def.Docker.Folder
		changed = true
		fixes = append(fixes, "docker.folder existed as file → default("+def.Docker.Folder+")")
	}

	return changed, fixes
}

// detectUnknownKeys returns a list of unknown keys (top-level and nested)
// by comparing the YAML against the struct schema (yaml tags) in types.go.
func detectUnknownKeys(raw []byte) []string {
	type anyMap = map[string]any

	var m anyMap
	if err := yaml.Unmarshal(raw, &m); err != nil {
		// parsing already handled by caller; return no unknowns here
		return nil
	}

	// Allowed top-level keys from Settings yaml tags
	st := reflect.TypeOf(Settings{})
	topTags := yamlFieldTags(st)

	// For nested validation: map top-level yaml tag -> allowed nested tags (if struct)
	nestedAllowed := map[string]map[string]struct{}{}
	if st.Kind() == reflect.Ptr {
		st = st.Elem()
	}
	if st.Kind() == reflect.Struct {
		for i := 0; i < st.NumField(); i++ {
			f := st.Field(i)
			topTag := yamlTagName(f.Tag.Get("yaml"))
			if topTag == "" || topTag == "-" {
				continue
			}
			ntags := yamlFieldTags(f.Type)
			if len(ntags) > 0 {
				nestedAllowed[topTag] = ntags
			}
		}
	}

	unknown := []string{}

	for k, v := range m {
		// unknown top-level key
		if _, ok := topTags[k]; !ok {
			unknown = append(unknown, k)
			continue
		}

		// if this top-level key maps to a struct, validate nested keys
		if allowedNested, hasNested := nestedAllowed[k]; hasNested {
			mm, ok := v.(map[string]any)
			if !ok {
				unknown = append(unknown, k+" (expected map)")
				continue
			}
			for kk := range mm {
				if _, ok := allowedNested[kk]; !ok {
					unknown = append(unknown, k+"."+kk)
				}
			}
		}
	}

	return unknown
}

// yamlFieldTags returns the set of yaml tag names for the fields of a struct type.
// If t is nil or not a struct, returns an empty set.
func yamlFieldTags(t reflect.Type) map[string]struct{} {
	out := make(map[string]struct{})
	if t == nil {
		return out
	}
	// Deref pointer if needed
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := yamlTagName(f.Tag.Get("yaml"))
		if tag == "" || tag == "-" {
			continue
		}
		out[tag] = struct{}{}
	}
	return out
}

// yamlTagName extracts the name portion of a yaml tag (before any comma options).
func yamlTagName(tag string) string {
	if tag == "" {
		return ""
	}
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return strings.TrimSpace(tag)
}
