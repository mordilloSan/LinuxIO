package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
)

const MaxTextFileBytes int64 = 1_000_000

var (
	ErrEditorFileNotEligible = errors.New("file is not eligible for text editing")
	ErrEditorFileContainsNUL = errors.New("file contains a NUL byte")
	// ErrNotRegularFile is what OpenRegularForRead answers for anything but a
	// regular file.
	ErrNotRegularFile = errors.New("not a regular file")
)

// OpenRegularForRead opens the regular file at path for a read: the download,
// the editor and an archive the user's own programs would read. path is
// reached as a read reaches it, every link followed (see
// linksafe.OpenEntryFollowing), and its last name is opened as an O_PATH
// handle without following a link. Only when fstat on that handle says it is a
// regular file is the file reopened through the handle, by /proc/self/fd, for
// reading without blocking or taking a terminal. So a FIFO, socket or device
// is never opened for reading, whatever is put at the name in between: what
// is judged is what is opened. Anything else comes back as ErrNotRegularFile,
// with what fstat said of it.
func OpenRegularForRead(ctx context.Context, path string) (*os.File, os.FileInfo, error) {
	dir, name, err := linksafe.OpenEntryFollowing(ctx, path, Owner{})
	if err != nil {
		return nil, nil, err
	}
	defer dir.Close()
	return openRegularAt(entry{dir: dir, name: name, path: filepath.Join(dir.Name(), name)})
}

// openRegularAt is OpenRegularForRead for e, in its open folder.
func openRegularAt(e entry) (*os.File, os.FileInfo, error) {
	handle, info, err := e.openPath()
	if err != nil {
		return nil, nil, err
	}
	defer handle.Close()
	if !info.Mode().IsRegular() {
		return nil, info, fmt.Errorf("%s: %w", e.path, ErrNotRegularFile)
	}
	fd, err := unix.Open(procPath(handle), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, &fs.PathError{Op: "open", Path: e.path, Err: err}
	}
	return os.NewFile(uintptr(fd), e.path), info, nil
}

// EditorFileInfo is the result of the text-read API.
type EditorFileInfo struct {
	CanSave bool
	Version string
	Content []byte
}

// ReadEditorFile resolves and opens only the requested target. It does not
// enumerate the target's parent directory. linkRule is the session user a
// save would act for (see linksafe.ActingFor): the read follows every link,
// a save on a root bridge only the ones that user cannot have planted, so a
// file the save could not reach is not offered for saving.
func ReadEditorFile(ctx context.Context, path string, linkRule Owner) (EditorFileInfo, error) {
	if err := ctx.Err(); err != nil {
		return EditorFileInfo{}, err
	}
	root, err := fsroot.Open()
	if err != nil {
		return EditorFileInfo{}, err
	}
	defer root.Close()
	result, err := readEditorFile(ctx, root, path)
	if err != nil || !result.CanSave || !linkRule.Set {
		return result, err
	}
	if _, err = linksafe.Resolve(ctx, path, linkRule); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		result.CanSave = false
	}
	return result, nil
}

func readEditorFile(ctx context.Context, root *fsroot.FSRoot, path string) (EditorFileInfo, error) {
	result := EditorFileInfo{}
	cleanPath, requestedInfo, err := editorTargetInfo(ctx, root, path)
	if err != nil {
		return result, err
	}
	if !requestedInfo.Mode().IsRegular() || requestedInfo.Size() >= MaxTextFileBytes {
		return result, ErrEditorFileNotEligible
	}

	file, openedInfo, err := openEditorTarget(ctx, cleanPath)
	if err != nil {
		return result, err
	}
	defer file.Close()

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

// openEditorTarget opens the file at path for the editor through
// OpenRegularForRead, so what is not a regular file is refused unopened.
func openEditorTarget(ctx context.Context, path string) (*os.File, os.FileInfo, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	file, info, err := OpenRegularForRead(ctx, path)
	if errors.Is(err, ErrNotRegularFile) {
		return nil, nil, ErrEditorFileNotEligible
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open editor file: %w", err)
	}
	if info.Size() >= MaxTextFileBytes {
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
