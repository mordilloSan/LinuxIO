package schedules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const maxSchedules = 100
const maxDefinitionBytes = 512 << 10

type store struct{ configDir, unitDir string }

var defaultStore = store{configDir: "/etc/linuxio/schedules", unitDir: "/etc/systemd/system"}

type definitionDocument struct {
	Version    int                          `json:"version"`
	Definition apischema.ScheduleDefinition `json:"definition"`
}

func validID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil() && parsed.String() == id
}

func secureDirectory(path string, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe scheduled script directory %s", path)
	}
	// MkdirAll applies the bridge's umask. Execution users must be able to
	// traverse script directories; definitions still receive their private mode.
	return os.Chmod(path, mode)
}

func (s store) locked(ctx context.Context, fn func() error) error {
	if err := secureDirectory(s.configDir, 0o755); err != nil {
		return err
	}
	// ponytail: one lock for at most 100 administrator-managed definitions;
	// partition only if concurrent configuration changes become a bottleneck.
	return filelock.RunExclusive(ctx, filepath.Join(s.configDir, ".lock"), fn, filelock.WithPermissions(0o600))
}

func (s store) definitionPath(id string) (string, error) {
	if !validID(id) {
		return "", errors.New("invalid scheduled script ID")
	}
	return filepath.Join(s.configDir, "definitions", id+".json"), nil
}

func (s store) definition(id string) (apischema.ScheduleDefinition, error) {
	var doc definitionDocument
	path, err := s.definitionPath(id)
	if err != nil {
		return doc.Definition, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return doc.Definition, fmt.Errorf("read scheduled script: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) || info.Size() > maxDefinitionBytes {
		return doc.Definition, errors.New("unsafe scheduled script definition")
	}
	f, err := os.Open(path)
	if err != nil {
		return doc.Definition, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxDefinitionBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return doc.Definition, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return doc.Definition, errors.New("trailing definition data")
	}
	if doc.Version != 1 || doc.Definition.ID != id {
		return doc.Definition, errors.New("scheduled script identity or version mismatch")
	}
	return doc.Definition, nil
}

func (s store) save(def apischema.ScheduleDefinition) error {
	path, err := s.definitionPath(def.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(definitionDocument{Version: 1, Definition: def}, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxDefinitionBytes {
		return errors.New("scheduled script definition is too large")
	}
	if err := secureDirectory(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(path, data, 0o600)
}

func (s store) definitions(ctx context.Context) ([]apischema.ScheduleDefinition, error) {
	entries, err := os.ReadDir(filepath.Join(s.configDir, "definitions"))
	if errors.Is(err, fs.ErrNotExist) {
		return []apischema.ScheduleDefinition{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]apischema.ScheduleDefinition, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !validID(id) {
			continue
		}
		if len(result) >= maxSchedules {
			return nil, errors.New("scheduled script count exceeds limit")
		}
		def, err := s.definition(id)
		if err != nil {
			return nil, err
		}
		result = append(result, def)
	}
	return result, nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s store) removeScripts(id, keep string) error {
	if !validID(id) {
		return errors.New("invalid scheduled script ID")
	}
	paths, err := filepath.Glob(filepath.Join(s.configDir, "scripts", id+"-*.sh"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if path != keep {
			if err := removeFile(path); err != nil {
				return err
			}
		}
	}
	return nil
}
