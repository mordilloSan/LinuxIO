package apischema

type ResourceStatData struct {
	Group       string `json:"group"`
	Mode        string `json:"mode"`
	Owner       string `json:"owner"`
	Permissions string `json:"permissions"`
}

type DirectoryListingFolder struct {
	Name     string `json:"name"`
	Modified string `json:"modified"`
	Symlink  bool   `json:"symlink"`
}

type DirectoryListingFile struct {
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	Modified      string `json:"modified"`
	Symlink       bool   `json:"symlink"`
	IsRegularFile bool   `json:"isRegularFile"`
	CanOpenAsText bool   `json:"canOpenAsText"`
}

type DirectoryListing struct {
	Folders []DirectoryListingFolder `json:"folders"`
	Files   []DirectoryListingFile   `json:"files"`
}

type DirectoryChildren struct {
	Folders []string `json:"folders"`
	Files   []string `json:"files"`
}

type TextFile struct {
	Content string `json:"content"`
	Version string `json:"version"`
	CanSave bool   `json:"canSave"`
}

type DirectorySizeData struct {
	Size        int64 `json:"size"`
	FileCount   int64 `json:"fileCount"`
	FolderCount int64 `json:"folderCount"`
}

type SubfolderData struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type SubfoldersResponse struct {
	Subfolders []SubfolderData `json:"subfolders"`
}

type SearchResult struct {
	IsDir         bool   `json:"isDir"`
	IsRegularFile bool   `json:"isRegularFile"`
	ModTime       string `json:"mod_time"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	CanOpenAsText *bool  `json:"canOpenAsText,omitempty"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type UsersGroupsResponse struct {
	Groups []string `json:"groups"`
	Users  []string `json:"users"`
}

// BatchTransferRequest copies or moves many sources into a single destination
// directory within one Task. Each source's final name is its basename.
type BatchTransferRequest struct {
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	Overwrite   *bool    `json:"overwrite,omitempty"`
}

// BatchPathRequest deletes many paths within one Task.
type BatchPathRequest struct {
	Paths []string `json:"paths"`
}

// ExistsBatchItem reports one existing path from an exists_batch pre-check.
type ExistsBatchItem struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

// ExistsBatchResponse lists which of the requested paths already exist, so the
// frontend can ask the user about collisions before starting a transfer.
type ExistsBatchResponse struct {
	Existing []ExistsBatchItem `json:"existing"`
}

type ActionSourceDestinationRequest struct {
	Action string `json:"action"`
	Source string `json:"src"`
	Dest   string `json:"dst"`
}

type FileArchiveRequest struct {
	Format string   `json:"format"`
	Paths  []string `json:"paths"`
}

// FileChmodBatchRequest changes permissions (and optionally ownership) of
// many paths within one Task.
type FileChmodBatchRequest struct {
	Paths     []string `json:"paths"`
	Mode      string   `json:"mode"`
	Owner     string   `json:"owner"`
	Group     string   `json:"group"`
	Recursive *bool    `json:"recursive,omitempty"`
}

type FileCompressRequest struct {
	Format     string   `json:"format"`
	TargetPath string   `json:"targetPath"`
	Paths      []string `json:"paths"`
}

type FileExtractRequest struct {
	ArchivePath string  `json:"archivePath"`
	Destination *string `json:"destination,omitempty"`
}

type OptionalPathRequest struct {
	Path *string `json:"path,omitempty"`
}

type FileResourcePostRequest struct {
	Path     string `json:"path"`
	Override *bool  `json:"override,omitempty"`
}

type DirectoryChildrenRequest struct {
	Path         string `json:"path"`
	IncludeFiles bool   `json:"includeFiles"`
}

type FileSearchRequest struct {
	Query    string  `json:"query"`
	Limit    *string `json:"limit,omitempty"`
	BasePath *string `json:"basePath,omitempty"`
}

type FileUploadRequest struct {
	TargetPath      string  `json:"targetPath"`
	Size            string  `json:"size"`
	Overwrite       *bool   `json:"overwrite,omitempty"`
	ExpectedVersion *string `json:"expectedVersion,omitempty"`
}

// FileUploadBatchEntry is one file in a batch upload manifest. Path is
// relative to the batch destination directory; Size is its exact byte count.
type FileUploadBatchEntry struct {
	Path string `json:"path"`
	Size string `json:"size"`
	// Modified is the file's RFC 3339 modification time as the client knew it
	// (the browser's File.lastModified); it is applied to the written file so
	// uploads keep their dates instead of all being stamped with upload time.
	Modified *string `json:"modified,omitempty"`
}

// FileUploadBatchRequest uploads many files into one destination directory as
// a single Task. The client streams every file's bytes back-to-back in manifest
// order over one data stream; the bridge derives file boundaries from the
// manifest sizes, so the stream needs no in-band framing. Directories lists
// folders to create relative to the destination (for empty directories —
// parents of manifest files are created implicitly). Without Overwrite, a file
// whose destination already exists is skipped and reported in the result's
// failed list.
type FileUploadBatchRequest struct {
	Destination string                 `json:"destination"`
	Files       []FileUploadBatchEntry `json:"files"`
	Directories []string               `json:"directories,omitempty"`
	Overwrite   *bool                  `json:"overwrite,omitempty"`
}
