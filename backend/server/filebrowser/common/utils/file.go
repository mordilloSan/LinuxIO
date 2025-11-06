package utils

// FileOptions are the options when getting a file info.
type FileOptions struct {
	Username   string // username for access control
	Path       string // realpath
	Source     string
	IsDir      bool
	Expand     bool
	ReadHeader bool
	Content    bool
	Recursive  bool // whether to recursively index directories
	Metadata   bool // whether to get metadata
}
