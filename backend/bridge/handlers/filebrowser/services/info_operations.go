package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const directoryReadBatchSize = 128

// ListDirectory returns the metadata needed to render one directory.
// It deliberately does not inspect editor save permissions or file content.
func ListDirectory(ctx context.Context, path string) (iteminfo.DirectoryListing, error) {
	listing := iteminfo.DirectoryListing{
		Folders: make([]iteminfo.ItemInfo, 0),
		Files:   make([]iteminfo.ItemInfo, 0),
	}
	err := withDirectoryEntries(ctx, path, func(root *fsroot.FSRoot, dirPath string, entry os.FileInfo) error {
		item, isDir, err := directoryItem(ctx, root, dirPath, entry)
		if err != nil {
			return err
		}
		if isDir {
			listing.Folders = append(listing.Folders, item)
		} else {
			listing.Files = append(listing.Files, item)
		}
		return nil
	})
	if err != nil {
		return listing, err
	}
	if err := ctx.Err(); err != nil {
		return listing, err
	}
	iteminfo.SortItems(listing.Folders)
	if err := ctx.Err(); err != nil {
		return listing, err
	}
	iteminfo.SortItems(listing.Files)
	if err := ctx.Err(); err != nil {
		return listing, err
	}
	return listing, nil
}

// ListDirectoryChildren returns only names and directory/file classification.
func ListDirectoryChildren(ctx context.Context, path string, includeFiles bool) (iteminfo.DirectoryChildren, error) {
	children := iteminfo.DirectoryChildren{
		Folders: make([]string, 0),
		Files:   make([]string, 0),
	}
	err := withDirectoryEntries(ctx, path, func(root *fsroot.FSRoot, dirPath string, entry os.FileInfo) error {
		isDir, err := directoryEntryIsDirectory(ctx, root, dirPath, entry)
		if err != nil {
			return err
		}
		if isDir {
			children.Folders = append(children.Folders, entry.Name())
		} else if includeFiles {
			children.Files = append(children.Files, entry.Name())
		}
		return nil
	})
	if err != nil {
		return children, err
	}
	if err := ctx.Err(); err != nil {
		return children, err
	}
	iteminfo.SortNames(children.Folders)
	if err := ctx.Err(); err != nil {
		return children, err
	}
	iteminfo.SortNames(children.Files)
	if err := ctx.Err(); err != nil {
		return children, err
	}
	return children, nil
}

func directoryEntryIsDirectory(ctx context.Context, root *fsroot.FSRoot, dirPath string, entry os.FileInfo) (bool, error) {
	targetInfo, err := directoryEntryInfo(ctx, root, dirPath, entry)
	if err != nil {
		return false, err
	}
	return iteminfo.IsDirectory(targetInfo.info), nil
}

func withDirectoryEntries(ctx context.Context, path string, fn func(*fsroot.FSRoot, string, os.FileInfo) error) error {
	root, dir, cleanPath, err := openDirectoryForRead(ctx, path)
	if err != nil {
		return err
	}
	defer root.Close()
	defer dir.Close()

	return readDirectoryEntries(ctx, root, dir, cleanPath, fn)
}

func openDirectoryForRead(ctx context.Context, path string) (*fsroot.FSRoot, *os.File, string, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, "", ctxErr
	}
	root, err := fsroot.Open()
	if err != nil {
		return nil, nil, "", err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		root.Close()
		return nil, nil, "", ctxErr
	}
	cleanPath, isDir, err := iteminfo.ResolveSymlinksAt(ctx, root, path)
	if err != nil {
		root.Close()
		return nil, nil, "", err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		root.Close()
		return nil, nil, "", ctxErr
	}
	if !isDir {
		root.Close()
		return nil, nil, "", fmt.Errorf("path is not a directory: %s: %w", path, os.ErrInvalid)
	}

	dir, err := root.Root.Open(fsroot.ToRel(utils.CleanAbsPath(cleanPath)))
	if err != nil {
		root.Close()
		return nil, nil, "", err
	}
	return root, dir, cleanPath, nil
}

func readDirectoryEntries(ctx context.Context, root *fsroot.FSRoot, dir *os.File, cleanPath string, fn func(*fsroot.FSRoot, string, os.FileInfo) error) error {
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		entries, readErr := dir.Readdir(directoryReadBatchSize)
		for _, entry := range entries {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err := fn(root, cleanPath, entry); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func directoryItem(ctx context.Context, root *fsroot.FSRoot, dirPath string, entry os.FileInfo) (iteminfo.ItemInfo, bool, error) {
	targetInfo, err := directoryEntryInfo(ctx, root, dirPath, entry)
	if err != nil {
		return iteminfo.ItemInfo{}, false, err
	}
	isDir := iteminfo.IsDirectory(targetInfo.info)
	item := iteminfo.ItemInfo{
		Name:          entry.Name(),
		ModTime:       targetInfo.info.ModTime(),
		Symlink:       targetInfo.symlink,
		IsRegularFile: targetInfo.info.Mode().IsRegular(),
	}
	if !isDir {
		item.Size = targetInfo.info.Size()
		item.CanOpenAsText = item.IsRegularFile && item.Size < MaxTextFileBytes
	}
	return item, isDir, nil
}

type resolvedDirectoryEntry struct {
	info    os.FileInfo
	symlink bool
}

func directoryEntryInfo(ctx context.Context, root *fsroot.FSRoot, dirPath string, entry os.FileInfo) (resolvedDirectoryEntry, error) {
	result := resolvedDirectoryEntry{info: entry, symlink: entry.Mode()&os.ModeSymlink != 0}
	if !result.symlink {
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	targetPath, _, err := iteminfo.ResolveSymlinksAt(ctx, root, filepath.Join(dirPath, entry.Name()))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		// Keep dangling links as files described by the link itself.
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	targetInfo, err := root.Root.Lstat(fsroot.ToRel(targetPath))
	if err != nil {
		return result, nil
	}
	result.info = targetInfo
	return result, nil
}
