// Package updatestatus defines the app-update status record that the bridge
// writes and the webserver reads through /run/linuxio/update-status.json.
package updatestatus

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	// Version is the record schema version; readers ignore other versions.
	Version = 1

	StatusRunning = "running"
	StatusOK      = "ok"
	StatusError   = "error"
)

// Path is the runtime location of the record. It is a variable so tests can
// redirect it.
var Path = version.RunDir + "/update-status.json"

// ErrInvalid reports a record that is not valid JSON.
var ErrInvalid = errors.New("invalid update status file")

// Record is the JSON document persisted at Path.
type Record struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	OwnerUID   uint32 `json:"owner_uid"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitzero"`
	Error      string `json:"error,omitempty"`
	StartedAt  *int64 `json:"started_at,omitzero"`
	FinishedAt *int64 `json:"finished_at,omitzero"`
}

// Valid reports whether the record is a current-version, internally
// consistent snapshot of a run.
func (r Record) Valid() bool {
	if r.Version != Version || r.ID == "" || r.StartedAt == nil || *r.StartedAt <= 0 {
		return false
	}
	switch r.Status {
	case StatusRunning:
		return r.ExitCode == nil && r.FinishedAt == nil && r.Error == ""
	case StatusOK:
		return r.ExitCode != nil && *r.ExitCode == 0 && r.FinishedAt != nil && r.Error == ""
	case StatusError:
		return r.ExitCode != nil && *r.ExitCode != 0 && r.FinishedAt != nil && r.Error != ""
	default:
		return false
	}
}

// Write atomically replaces the record at Path.
func Write(r Record) error {
	data, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return err
	}
	return atomicfile.Write(Path, append(data, '\n'), 0o644)
}

// Read loads the record at Path. It returns (nil, nil) when the file is
// missing or empty and an error wrapping ErrInvalid when it is not valid JSON.
func Read() (*Record, error) {
	data, err := os.ReadFile(Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return &r, nil
}
