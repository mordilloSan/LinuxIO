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
	CanOpenAsText *bool  `json:"canOpenAsText,omitzero"`
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
	Overwrite   *bool    `json:"overwrite,omitzero"`
	// KeepBoth lists sources that land under a unique name ("name (copy)")
	// when their destination already exists, instead of replacing it.
	KeepBoth []string `json:"keepBoth,omitempty"`
}

// BatchPathRequest deletes many paths within one Task.
type BatchPathRequest struct {
	Paths []string `json:"paths"`
}

// ExistsBatchItem reports one existing path from an exists_batch pre-check.
type ExistsBatchItem struct {
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // RFC 3339; empty when unknown
}

// ExistsBatchResponse lists which of the requested paths already exist, so the
// frontend can ask the user about collisions before starting a transfer.
type ExistsBatchResponse struct {
	Existing []ExistsBatchItem `json:"existing"`
}

// FileBatchFailureReason says why one item of a batch failed, for the
// failures the page offers a way out of; it is left out for any other
// failure, whose message says why.
type FileBatchFailureReason string

const (
	// FileBatchFailureExists: a restore found something at the name the item
	// goes back under, and the request chose neither overwrite nor keep both
	// for it. The page can ask and send the item again with a choice.
	FileBatchFailureExists FileBatchFailureReason = "exists"
	// FileBatchFailureNoTrash: the item's filesystem cannot hold a trash for
	// the session user, so it stayed where it is. The page can offer to
	// delete it for good instead.
	FileBatchFailureNoTrash FileBatchFailureReason = "no_trash"
)

// TrashEntry is one item in the session user's trash. Path is where it sits
// now (under a trash "files" directory) and is what restore and purge take.
// The page shows the item under the base name of OriginalPath, or Name when
// OriginalPath is empty: a restore puts it back under that name, and a
// restore into a chosen folder names it so in TrashRestoreRequest.Names.
type TrashEntry struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	OriginalPath string `json:"originalPath"` // empty when no .trashinfo records it
	DeletedAt    string `json:"deletedAt"`    // RFC 3339; empty when unknown
	IsDir        bool   `json:"isDir"`
	Size         int64  `json:"size"`
	// ConfirmRestore is set when LinuxIO has no record of moving the item to
	// the trash (another program did) and restoring it to OriginalPath goes
	// into a folder the session user does not own: filebrowser.trash_restore
	// then needs OriginalPath in ConfirmedDestinations.
	ConfirmRestore bool `json:"confirmRestore"`
}

// TrashRestoreRequest restores trashed items, by default to where they came
// from. ConfirmedDestinations holds, by the item's path in Paths, the
// original path the user confirmed restoring it to, for items LinuxIO has no
// record of trashing whose restore goes into a folder the user does not own
// (see TrashEntry.ConfirmRestore); such a restore without its own destination
// here is refused.
//
// Destination, when set, is an existing folder the user chose: every item
// goes into it instead, as a move there would, under the name Names gives it
// by its path in Paths (the name the page shows, see TrashEntry) or else its
// name in the trash; the name is the request's, never read from the trash.
// ConfirmedDestinations does not apply there. A restore does not move an item
// to another filesystem, so a folder on another filesystem than the item's
// trash is refused.
//
// Overwrite and KeepBoth decide, as on BatchTransferRequest, what happens to
// an item whose name is taken where it goes: an item listed in KeepBoth (by
// its path in Paths) goes in under a free "name (copy)" name; with Overwrite
// any other item replaces what is there, a folder merging into the folder
// there. An item covered by neither fails with reason "exists". Back where
// it came from, on a session with sudo, an item LinuxIO has no record of
// trashing replaces only the destination ConfirmedDestinations gives it, and
// a folder does not merge into a folder the user does not own or into one
// inside such a folder.
type TrashRestoreRequest struct {
	Paths                 []string          `json:"paths"`
	ConfirmedDestinations map[string]string `json:"confirmedDestinations,omitzero"`
	Destination           string            `json:"destination,omitzero"`
	Names                 map[string]string `json:"names,omitzero"`
	Overwrite             *bool             `json:"overwrite,omitzero"`
	KeepBoth              []string          `json:"keepBoth,omitempty"`
}

type TrashListResponse struct {
	Entries []TrashEntry `json:"entries"`
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
	Recursive *bool    `json:"recursive,omitzero"`
}

type FileCompressRequest struct {
	Format     string   `json:"format"`
	TargetPath string   `json:"targetPath"`
	Paths      []string `json:"paths"`
}

type FileExtractRequest struct {
	ArchivePath string  `json:"archivePath"`
	Destination *string `json:"destination,omitzero"`
}

type OptionalPathRequest struct {
	Path *string `json:"path,omitzero"`
}

type FileResourcePostRequest struct {
	Path string `json:"path"`
}

type DirectoryChildrenRequest struct {
	Path         string `json:"path"`
	IncludeFiles bool   `json:"includeFiles"`
}

type FileSearchRequest struct {
	Query    string  `json:"query"`
	Limit    *string `json:"limit,omitzero"`
	BasePath *string `json:"basePath,omitzero"`
}

// FileUploadOwner names who the entries an upload creates belong to. The only
// value is "session": the bridge takes the ids from the authenticated
// session, never from the request.
type FileUploadOwner string

type FileUploadRequest struct {
	TargetPath      string  `json:"targetPath"`
	Size            string  `json:"size"`
	Overwrite       *bool   `json:"overwrite,omitzero"`
	ExpectedVersion *string `json:"expectedVersion,omitzero"`
	// Owner "session" makes a root bridge hand the new file (0644), and the
	// folders the upload creates (0755), to the session user, but only inside
	// a folder that user already owns; in a setgid folder they keep the
	// inherited group. Unset, a root bridge keeps them root-owned. An
	// overwritten file always keeps its owner and mode. Set, a symbolic link
	// at the target is refused (409) instead of written through, as an
	// upload without owner does.
	Owner FileUploadOwner `json:"owner,omitempty"`
	// Private creates a new file readable only by its owner (0600), for files
	// such as a Compose .env that may hold secrets.
	Private *bool `json:"private,omitzero"`
}

// FileUploadBatchEntry is one file in a batch upload manifest. Path is
// relative to the batch destination directory; Size is its exact byte count.
type FileUploadBatchEntry struct {
	Path string `json:"path"`
	Size string `json:"size"`
	// Modified is the file's RFC 3339 modification time as the client knew it
	// (the browser's File.lastModified); it is applied to the written file so
	// uploads keep their dates instead of all being stamped with upload time.
	Modified *string `json:"modified,omitzero"`
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
	Overwrite   *bool                  `json:"overwrite,omitzero"`
	// KeepBoth lists manifest paths that land under a unique name when their
	// destination already exists, instead of being skipped or replaced.
	KeepBoth []string `json:"keepBoth,omitempty"`
	// Owner and Private apply to every file and folder the batch creates,
	// as they do for FileUploadRequest.
	Owner   FileUploadOwner `json:"owner,omitempty"`
	Private *bool           `json:"private,omitzero"`
}
