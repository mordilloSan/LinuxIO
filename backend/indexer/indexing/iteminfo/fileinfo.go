// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package iteminfo

import (
	"time"
)

// ItemInfo represents basic information about a file or directory
type ItemInfo struct {
	Name    string    `json:"name"`     // name of the file/folder
	Size    int64     `json:"size"`     // size in bytes
	ModTime time.Time `json:"modified"` // modification time
	Type    string    `json:"type"`     // "directory" for folders, or basic type for files
	Hidden  bool      `json:"hidden"`   // whether the file is hidden
	Inode   uint64    `json:"inode"`    // inode identifier when available
}

// FileInfo describes a file or directory scoped to the associated index.
type FileInfo struct {
	ItemInfo
	Path string `json:"path"` // path scoped to the associated index
}
