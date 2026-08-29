package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

const MaxTextFileBytes int64 = 1_000_000

var (
	ErrEditorFileNotEligible = errors.New("file is not eligible for text editing")
	ErrEditorFileContainsNUL = errors.New("file contains a NUL byte")
)

// EditorFileInfo keeps the validation details used by existing transfer code;
// callers of the text-read API need only Content, Version, and CanSave.
type EditorFileInfo struct {
	IsRegularFile bool
	Size          int64
	CanOpenAsText bool
	CanSave       bool
	Version       string
	Content       []byte
}

// ReadEditorFile resolves and opens only the requested target. It does not
// enumerate the target's parent directory.
func ReadEditorFile(ctx context.Context, path string) (EditorFileInfo, error) {
	if err := ctx.Err(); err != nil {
		return EditorFileInfo{}, err
	}
	root, err := fsroot.Open()
	if err != nil {
		return EditorFileInfo{}, err
	}
	defer root.Close()
	return readEditorFile(ctx, root, path)
}

func readEditorFile(ctx context.Context, root *fsroot.FSRoot, path string) (EditorFileInfo, error) {
	result := EditorFileInfo{}
	cleanPath, requestedInfo, err := editorTargetInfo(ctx, root, path)
	if err != nil {
		return result, err
	}
	result.Size = requestedInfo.Size()
	if !requestedInfo.Mode().IsRegular() || requestedInfo.Size() >= MaxTextFileBytes {
		return result, ErrEditorFileNotEligible
	}

	file, openedInfo, err := openEditorTarget(ctx, root, cleanPath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	result.IsRegularFile = true
	result.Size = openedInfo.Size()

	content, err := readEditorContent(ctx, file)
	if err != nil {
		return result, err
	}
	result.Content = content

	if bytes.IndexByte(content, 0) >= 0 {
		return result, ErrEditorFileContainsNUL
	}
	if !isEditableTextContent(content) {
		return result, ErrEditorFileNotEligible
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	digest := sha256.Sum256(content)
	result.CanOpenAsText = true
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	result.CanSave = canSaveEditorFile(root, cleanPath, openedInfo)
	result.Version = hex.EncodeToString(digest[:])
	return result, nil
}

func editorTargetInfo(ctx context.Context, root *fsroot.FSRoot, path string) (string, os.FileInfo, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", nil, ctxErr
	}
	cleanPath, _, err := iteminfo.ResolveSymlinksAt(ctx, root, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", nil, ctxErr
		}
		return "", nil, fmt.Errorf("resolve editor file: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", nil, ctxErr
	}
	info, err := root.Root.Lstat(fsroot.ToRel(cleanPath))
	if err != nil {
		return "", nil, fmt.Errorf("stat editor file: %w", err)
	}
	return cleanPath, info, nil
}

func openEditorTarget(ctx context.Context, root *fsroot.FSRoot, path string) (*os.File, os.FileInfo, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	file, err := root.Root.OpenFile(fsroot.ToRel(path), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open editor file: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		file.Close()
		return nil, nil, ctxErr
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("stat editor file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() >= MaxTextFileBytes {
		file.Close()
		return nil, nil, ErrEditorFileNotEligible
	}
	return file, info, nil
}

func readEditorContent(ctx context.Context, file *os.File) ([]byte, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxTextFileBytes))
	if err != nil {
		return nil, fmt.Errorf("read editor file: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if int64(len(content)) >= MaxTextFileBytes {
		return nil, ErrEditorFileNotEligible
	}
	return content, nil
}

// canSaveEditorFile checks the permissions needed by the atomic temp-file
// plus rename save path. The target's write bit is intentionally irrelevant.
func canSaveEditorFile(root *fsroot.FSRoot, path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	parentInfo, ok := editorParentCanSave(root, filepath.Dir(path))
	if !ok {
		return false
	}
	return canSaveEditorEntry(parentInfo, info)
}

func editorParentCanSave(root *fsroot.FSRoot, parentPath string) (os.FileInfo, bool) {
	if err := unix.Faccessat2(unix.AT_FDCWD, parentPath, unix.W_OK|unix.X_OK, unix.AT_EACCESS); err != nil {
		return nil, false
	}
	parentInfo, err := root.Root.Stat(fsroot.ToRel(parentPath))
	return parentInfo, err == nil && parentInfo.IsDir()
}

func canSaveEditorEntry(parentInfo, targetInfo os.FileInfo) bool {
	if !targetInfo.Mode().IsRegular() {
		return false
	}
	if parentInfo.Mode()&os.ModeSticky == 0 || os.Geteuid() == 0 {
		return true
	}

	parentStat, parentOK := parentInfo.Sys().(*syscall.Stat_t)
	targetStat, targetOK := targetInfo.Sys().(*syscall.Stat_t)
	if !parentOK || !targetOK {
		return false
	}
	uid := uint32(os.Geteuid())
	return parentStat.Uid == uid || targetStat.Uid == uid
}

// EditorFileVersion validates and hashes the directly requested target for
// optimistic upload checks. The context is explicit so cancellation reaches
// the same bounded read path as ReadEditorFile.
func EditorFileVersion(ctx context.Context, root *fsroot.FSRoot, path string) (string, error) {
	result, err := readEditorFile(ctx, root, path)
	if err != nil {
		return "", err
	}
	return result.Version, nil
}
