package shares

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/sharedfolders"
)

// sharedFoldersFile records the shared folders LinuxIO made: which
// subvolume, under which share name, who may use it and how it is shared.
// The smb.conf section, the export and the folder's ACL are written from
// it. A var so tests can point it at a temp file.
var sharedFoldersFile = sharedfolders.RecordPath

type foldersDocument struct {
	Folders []apischema.SharedFolder `json:"folders"`
}

// loadFolders reads the record. A missing file is no folders.
func loadFolders() ([]apischema.SharedFolder, error) {
	data, err := os.ReadFile(sharedFoldersFile)
	if errors.Is(err, fs.ErrNotExist) {
		return []apischema.SharedFolder{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sharedFoldersFile, err)
	}
	var doc foldersDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", sharedFoldersFile, err)
	}
	if doc.Folders == nil {
		doc.Folders = []apischema.SharedFolder{}
	}
	return doc.Folders, nil
}

// saveFolders writes the record, readable by root alone. Folder, the place
// on the host, is worked out when listed and not stored.
func saveFolders(folders []apischema.SharedFolder) error {
	stored := slices.Clone(folders)
	for i := range stored {
		stored[i].Folder = ""
	}
	data, err := json.Marshal(foldersDocument{Folders: stored}, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("marshal %s: %w", sharedFoldersFile, err)
	}
	if err := atomicfile.Write(sharedFoldersFile, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", sharedFoldersFile, err)
	}
	return nil
}

// foldersLockFile serialises every operation on shared folders, and the
// plain share routes with them. In /run, which root alone writes (see
// confLockFile). A var so tests can use a temp path.
var foldersLockFile = "/run/linuxio-shared-folders.lock"

// withFoldersLock runs fn as the only operation on shared folders, in this
// bridge and in any other. It is taken before the lock of smb.conf and
// /etc/exports (withConfLock), never inside it.
func withFoldersLock(ctx context.Context, fn func() error) error {
	return filelock.RunExclusive(ctx, foldersLockFile, fn, filelock.WithTimeout(10*time.Second))
}

func folderIndex(folders []apischema.SharedFolder, uuid, path string) int {
	return slices.IndexFunc(folders, func(f apischema.SharedFolder) bool {
		return f.UUID == uuid && f.Path == path
	})
}

// putFolder stores f in place of the record of the same subvolume, or adds
// it.
func putFolder(f apischema.SharedFolder) error {
	folders, err := loadFolders()
	if err != nil {
		return err
	}
	if i := folderIndex(folders, f.UUID, f.Path); i >= 0 {
		folders[i] = f
	} else {
		folders = append(folders, f)
	}
	return saveFolders(folders)
}

func removeFolder(uuid, path string) error {
	folders, err := loadFolders()
	if err != nil {
		return err
	}
	folders = slices.DeleteFunc(folders, func(f apischema.SharedFolder) bool {
		return f.UUID == uuid && f.Path == path
	})
	return saveFolders(folders)
}

// folderNamed returns the record whose share name is name, as Samba compares
// section names: without regard to case.
func folderNamed(folders []apischema.SharedFolder, name string) (apischema.SharedFolder, bool) {
	i := slices.IndexFunc(folders, func(f apischema.SharedFolder) bool { return strings.EqualFold(f.Name, name) })
	if i < 0 {
		return apischema.SharedFolder{}, false
	}
	return folders[i], true
}
