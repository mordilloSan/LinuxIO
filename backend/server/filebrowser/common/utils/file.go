package utils

// FileOptions are the options when getting a file info.
type FileOptions struct {
	Username   string // username for access control
	Path       string // realpath
	IsDir      bool
	Expand     bool
	ReadHeader bool
	Content    bool
	Metadata   bool // whether to get metadata
}
