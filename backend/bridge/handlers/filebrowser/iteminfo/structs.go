package iteminfo

import "time"

// ItemInfo is the metadata needed to render one directory child.
type ItemInfo struct {
	Name          string
	Size          int64
	ModTime       time.Time
	Symlink       bool
	IsRegularFile bool
	CanOpenAsText bool
}

type DirectoryListing struct {
	Folders []ItemInfo
	Files   []ItemInfo
}

type DirectoryChildren struct {
	Folders []string
	Files   []string
}
