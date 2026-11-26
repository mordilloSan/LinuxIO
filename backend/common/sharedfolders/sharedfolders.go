// Package sharedfolders holds what the programs other than the bridge read
// of the shared folders the bridge records (backend/bridge/handlers/shares,
// docs/shared-folders.md): where the record is, and which folders keep a
// recycle bin that linuxio-worker empties.
package sharedfolders

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// RecordPath is the bridge's record of the shared folders, root's alone. A
// var so tests can point it at a temp file.
var RecordPath = version.ConfigDir + "/shared-folders.json"

// RecycleBinName is the folder at the top of a shared folder that Samba
// moves what is deleted over SMB into.
const RecycleBinName = "#recycle"

// RecycleBinMaxAge is how long what is in a recycle bin is kept.
const RecycleBinMaxAge = 30 * 24 * time.Hour

// RecycleBin names a shared folder whose recycle bin is on: its pool and its
// path from the pool's top level.
type RecycleBin struct {
	UUID string
	Path string
}

// LoadRecycleBins returns the shared folders whose recycle bin is on. A
// missing record is no folders. Only the fields read here are decoded: the
// bridge owns the rest of the record.
func LoadRecycleBins() ([]RecycleBin, error) {
	data, err := os.ReadFile(RecordPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", RecordPath, err)
	}
	var doc struct {
		Folders []struct {
			UUID       string `json:"uuid"`
			Path       string `json:"path"`
			RecycleBin bool   `json:"recycleBin"`
		} `json:"folders"`
	}
	if err = json.Unmarshal(data, &doc, json.RejectUnknownMembers(false)); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RecordPath, err)
	}
	var bins []RecycleBin
	for _, f := range doc.Folders {
		if f.RecycleBin {
			bins = append(bins, RecycleBin{UUID: f.UUID, Path: f.Path})
		}
	}
	return bins, nil
}
