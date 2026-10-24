package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// fsError classifies a filesystem failure: a missing, existing, forbidden or
// invalid path is the caller's problem, anything else stays a plain error.
func fsError(op string, err error) error {
	var sentinel error
	switch {
	case errors.Is(err, fs.ErrNotExist):
		sentinel = bridgeipc.ErrNotFound
	case errors.Is(err, fs.ErrExist):
		sentinel = bridgeipc.ErrConflict
	case errors.Is(err, fs.ErrPermission):
		sentinel = bridgeipc.ErrForbidden
	case errors.Is(err, fs.ErrInvalid), errors.Is(err, services.ErrEditorFileNotEligible), errors.Is(err, services.ErrEditorFileContainsNUL):
		sentinel = bridgeipc.ErrInvalidArgs
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
	return fmt.Errorf("%w: %s: %w", sentinel, op, err)
}

func listDirectory(ctx context.Context, req apischema.PathRequest) (apischema.DirectoryListing, error) {
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryListing{}, err
	}
	if req.Path == "" {
		return apischema.DirectoryListing{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}

	listing, err := services.ListDirectory(ctx, req.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.DirectoryListing{}, err
		}
		return apischema.DirectoryListing{}, fsError("list directory", err)
	}
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryListing{}, err
	}
	return directoryListingResponse(listing), nil
}

func directoryChildren(ctx context.Context, req apischema.DirectoryChildrenRequest) (apischema.DirectoryChildren, error) {
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryChildren{}, err
	}
	if req.Path == "" {
		return apischema.DirectoryChildren{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}
	children, err := services.ListDirectoryChildren(ctx, req.Path, req.IncludeFiles)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.DirectoryChildren{}, err
		}
		return apischema.DirectoryChildren{}, fsError("list directory children", err)
	}
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryChildren{}, err
	}
	return apischema.DirectoryChildren{Folders: children.Folders, Files: children.Files}, nil
}

func readText(ctx context.Context, req apischema.PathRequest) (apischema.TextFile, error) {
	if err := ctx.Err(); err != nil {
		return apischema.TextFile{}, err
	}
	if req.Path == "" {
		return apischema.TextFile{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}
	file, err := services.ReadEditorFile(ctx, req.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.TextFile{}, err
		}
		return apischema.TextFile{}, fsError("read text file", err)
	}
	return apischema.TextFile{Content: string(file.Content), Version: file.Version, CanSave: file.CanSave}, nil
}

func directoryListingResponse(listing iteminfo.DirectoryListing) apischema.DirectoryListing {
	response := apischema.DirectoryListing{
		Folders: make([]apischema.DirectoryListingFolder, 0, len(listing.Folders)),
		Files:   make([]apischema.DirectoryListingFile, 0, len(listing.Files)),
	}
	for _, folder := range listing.Folders {
		response.Folders = append(response.Folders, apischema.DirectoryListingFolder{
			Name: folder.Name, Modified: formatResourceModTime(folder.ModTime), Symlink: folder.Symlink,
		})
	}
	for _, file := range listing.Files {
		response.Files = append(response.Files, apischema.DirectoryListingFile{
			Name: file.Name, Size: file.Size, Modified: formatResourceModTime(file.ModTime),
			Symlink: file.Symlink, IsRegularFile: file.IsRegularFile, CanOpenAsText: file.CanOpenAsText,
		})
	}
	return response
}

func formatResourceModTime(modTime time.Time) string {
	if modTime.IsZero() {
		return ""
	}
	return modTime.Format(time.RFC3339Nano)
}

// resourceStat returns direct permission metadata without enumerating siblings.
func resourceStat(ctx context.Context, req apischema.PathRequest) (*apischema.ResourceStatData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}

	statData, err := iteminfo.CollectStatInfo(ctx, req.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fsError("collect stat info", err)
	}

	return statData, nil
}

// existsBatch reports which of the requested paths already exist. Used as a
// pre-flight check so transfers can ask the user about collisions before any
// bytes move.
func existsBatch(ctx context.Context, req apischema.BatchPathRequest) (apischema.ExistsBatchResponse, error) {
	response := apischema.ExistsBatchResponse{Existing: []apischema.ExistsBatchItem{}}
	if len(req.Paths) == 0 {
		return response, nil
	}

	root, err := fsroot.Open()
	if err != nil {
		return response, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	for _, raw := range req.Paths {
		if err := ctx.Err(); err != nil {
			return response, err
		}
		path := fsroot.CleanAbsPath(raw)
		info, statErr := root.Root.Lstat(fsroot.ToRel(path))
		if statErr != nil {
			continue
		}
		response.Existing = append(response.Existing, apischema.ExistsBatchItem{
			Path:     path,
			IsDir:    info.IsDir(),
			Size:     info.Size(),
			Modified: formatResourceModTime(info.ModTime()),
		})
	}
	return response, nil
}
