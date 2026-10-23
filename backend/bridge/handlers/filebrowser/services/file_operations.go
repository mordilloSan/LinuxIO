package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

var (
	PermFile os.FileMode = 0o664 // rw-rw-r-- (owner read+write, group read+write, rest read)
	PermDir  os.FileMode = 0o775 // rwxrwxr-x (owner read+write+execute, group read+write+execute, rest read+execute)
)

func relPath(p string) string {
	return fsroot.ToRel(fsroot.CleanAbsPath(p))
}

// CopyFileWithCallbacks copies a file or directory with progress callbacks.
// With overwrite an existing destination file is replaced and an existing
// destination directory is merged, entry by entry.
func CopyFileWithCallbacks(source, dest string, overwrite bool, opts *OperationCallbacks) error {
	source = fsroot.CleanAbsPath(source)
	dest = fsroot.CleanAbsPath(dest)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	if validateErr := validateMoveDestination(root, source, dest); validateErr != nil {
		return validateErr
	}

	// Check if the source exists without following symlinks.
	info, err := root.Root.Lstat(relPath(source))
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if err := validateSymlinkCopyDestination(root, source, dest); err != nil {
			return err
		}
	}

	return transferEntry(root, source, dest, info, overwrite, opts, newCopyState(false), MoveFileOptions{})
}

// copyState carries per-operation bookkeeping through a recursive transfer.
type copyState struct {
	// links maps a source inode to the first destination written for it, so
	// further hardlinks to that inode become links instead of second copies.
	links map[linkKey]string
	// move removes each source entry as soon as its copy is complete, so a
	// transfer that stops halfway leaves every entry in exactly one place.
	move bool
}

func newCopyState(move bool) *copyState {
	return &copyState{links: map[linkKey]string{}, move: move}
}

// transferEntry copies or moves src onto dst. A missing dst is created. With
// overwrite an existing file is replaced and an existing directory is merged,
// as Explorer does; without it an existing dst is an error.
func transferEntry(root *fsroot.FSRoot, src, dst string, info os.FileInfo, overwrite bool, opts *OperationCallbacks, state *copyState, moveOpts MoveFileOptions) error {
	if opts.IsCancelled() {
		return ErrAborted
	}
	if src == dst {
		return fmt.Errorf("cannot transfer %q onto itself", src)
	}

	dstInfo, err := root.Root.Lstat(relPath(dst))
	exists := err == nil
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	case exists && !overwrite:
		return fmt.Errorf("destination already exists")
	case exists && dstInfo.IsDir() != info.IsDir():
		return fmt.Errorf("destination exists with different type")
	case exists && info.IsDir():
		return mergeDirectory(root, src, dst, overwrite, opts, state)
	}

	finish, err := prepareDestination(root, dst, exists, !state.move)
	if err != nil {
		return err
	}
	err = placeEntry(root, src, dst, info, opts, state, moveOpts)
	finish(err)
	return err
}

// placeEntry puts src at a dst that does not exist. A move renames when the
// filesystem allows it and otherwise copies and removes entry by entry.
func placeEntry(root *fsroot.FSRoot, src, dst string, info os.FileInfo, opts *OperationCallbacks, state *copyState, moveOpts MoveFileOptions) error {
	if state.move {
		moved, err := tryRenameMove(root, src, dst, opts, moveOpts)
		if moved || err != nil {
			return err
		}
	}
	return copyEntryWithCallbacks(root, src, dst, info, opts, state)
}

// mergeDirectory transfers the children of src into the existing directory
// dst. A move then removes the emptied src, which fails when anything was
// skipped or added meanwhile, so nothing is ever deleted unseen.
func mergeDirectory(root *fsroot.FSRoot, src, dst string, overwrite bool, opts *OperationCallbacks, state *copyState) error {
	err := forEachChild(root, src, func(name string, info os.FileInfo) error {
		return transferEntry(root, filepath.Join(src, name), filepath.Join(dst, name), info, overwrite, opts, state, MoveFileOptions{})
	})
	if err != nil || !state.move {
		return err
	}
	return root.Root.Remove(relPath(src))
}

// forEachChild calls fn for every entry of dir except the special files a
// copy cannot recreate; those are skipped with a warning, as rsync does.
func forEachChild(root *fsroot.FSRoot, dir string, fn func(name string, info os.FileInfo) error) error {
	d, err := root.Root.Open(relPath(dir))
	if err != nil {
		return err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entryType := info.Mode().Type(); entryType&specialFileTypes != 0 {
			slog.Warn("skipping special file during copy", "component", "filebrowser", "subsystem", "file_operations", "path", filepath.Join(dir, entry.Name()), "type", entryType.String())
			continue
		}
		if err := fn(entry.Name(), info); err != nil {
			return err
		}
	}
	return nil
}

type linkKey struct {
	dev uint64
	ino uint64
}

// preservedModeBits are the mode bits a copy carries over from its source.
const preservedModeBits = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// specialFileTypes are entry types a copy cannot recreate; they are skipped
// inside directories, as rsync does, instead of failing the whole transfer.
const specialFileTypes = os.ModeNamedPipe | os.ModeSocket | os.ModeDevice | os.ModeCharDevice

// copyEntryWithCallbacks copies source to a dest that does not exist yet and,
// for a move, removes the source once the copy is complete. That removal is a
// plain Remove: a directory still holding a skipped or newly added entry
// stays behind with an error instead of being deleted unseen.
func copyEntryWithCallbacks(root *fsroot.FSRoot, source, dest string, info os.FileInfo, opts *OperationCallbacks, state *copyState) error {
	if opts.IsCancelled() {
		return ErrAborted
	}

	var err error
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		err = copySymlink(root, source, dest, info)
	case info.IsDir():
		err = copyDirectoryWithCallbacks(root, source, dest, info, opts, state)
	case mode.IsRegular():
		err = copySingleFileWithCallbacks(root, source, dest, info, opts, state)
	default:
		return fmt.Errorf("cannot copy non-regular file %q with mode %s", source, mode.Type())
	}
	if err != nil || !state.move {
		return err
	}
	return root.Root.Remove(relPath(source))
}

func copySymlink(root *fsroot.FSRoot, source, dest string, info os.FileInfo) error {
	target, err := root.Root.Readlink(relPath(source))
	if err != nil {
		return err
	}

	if err := root.Root.MkdirAll(relPath(filepath.Dir(dest)), PermDir); err != nil {
		return err
	}

	destRel := relPath(dest)
	if err := root.Root.Symlink(target, destRel); err != nil {
		return err
	}
	return chownLike(info, func(uid, gid int) error { return root.Root.Lchown(destRel, uid, gid) })
}

func validateSymlinkCopyDestination(root *fsroot.FSRoot, source, dest string) error {
	target, err := root.Root.Readlink(relPath(source))
	if err != nil {
		return err
	}

	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(filepath.Dir(source), targetPath)
	}

	if fsroot.CleanAbsPath(targetPath) == fsroot.CleanAbsPath(dest) {
		return fmt.Errorf("cannot copy symlink %q onto its target %q", source, dest)
	}
	return nil
}

// copySingleFileWithCallbacks handles copying a single file with progress callbacks.
func copySingleFileWithCallbacks(root *fsroot.FSRoot, source, dest string, info os.FileInfo, opts *OperationCallbacks, state *copyState) error {
	if opts.IsCancelled() {
		return ErrAborted
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot copy non-regular file %q with mode %s", source, info.Mode().Type())
	}

	if mkdirErr := root.Root.MkdirAll(relPath(filepath.Dir(dest)), PermDir); mkdirErr != nil {
		return mkdirErr
	}

	destRel := relPath(dest)
	key, linked := hardlinkKey(info)
	if existing, ok := state.links[key]; linked && ok {
		if err := root.Root.Link(relPath(existing), destRel); err == nil {
			opts.ReportProgress(info.Size())
			return nil
		}
		// Linking can fail when the destination tree spans filesystems; a
		// plain copy is still a correct, if larger, result.
	}

	src, err := root.Root.Open(relPath(source))
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := root.Root.OpenFile(destRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer dst.Close()

	if err := copyWithCallbacks(dst, src, opts); err != nil {
		// Half a file under its final name would pass for the whole one.
		if removeErr := root.Root.Remove(destRel); removeErr != nil {
			slog.Warn("failed to remove partial copy", "component", "filebrowser", "subsystem", "file_operations", "path", dest, "error", removeErr)
		}
		return err
	}

	if err := applyAttributes(root, src, dst, destRel, info); err != nil {
		return err
	}
	if linked {
		state.links[key] = dest
	}
	return nil
}

// copyDirectoryWithCallbacks copies a directory tree to a dest that does not exist yet.
func copyDirectoryWithCallbacks(root *fsroot.FSRoot, source, dest string, info os.FileInfo, opts *OperationCallbacks, state *copyState) error {
	if opts.IsCancelled() {
		return ErrAborted
	}

	// Created writable so the children can be written; the source mode is
	// applied once they are in place, which also handles read-only sources.
	if err := root.Root.MkdirAll(relPath(dest), PermDir); err != nil {
		return err
	}

	err := forEachChild(root, source, func(name string, entryInfo os.FileInfo) error {
		return copyEntryWithCallbacks(root, filepath.Join(source, name), filepath.Join(dest, name), entryInfo, opts, state)
	})
	if err != nil {
		return err
	}

	srcDir, err := root.Root.Open(relPath(source))
	if err != nil {
		return err
	}
	defer srcDir.Close()
	dstDir, err := root.Root.Open(relPath(dest))
	if err != nil {
		return err
	}
	defer dstDir.Close()
	return applyAttributes(root, srcDir, dstDir, relPath(dest), info)
}

// applyAttributes makes dst match info: xattrs and ownership through the open
// descriptors, then mode, since a chown clears setuid and setgid, then times.
func applyAttributes(root *fsroot.FSRoot, src, dst *os.File, destRel string, info os.FileInfo) error {
	copyXattrs(src, dst, destRel)
	if err := chownLike(info, dst.Chown); err != nil {
		return err
	}
	if err := dst.Chmod(info.Mode() & preservedModeBits); err != nil {
		return err
	}
	atime, mtime := fileTimes(info)
	return root.Root.Chtimes(destRel, atime, mtime)
}

// chownLike applies the source ownership. Only root may hand files to other
// users, so a permission error in an unprivileged session is expected and
// ignored, as cp -p does.
func chownLike(info os.FileInfo, chown func(uid, gid int) error) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	err := chown(int(st.Uid), int(st.Gid))
	if errors.Is(err, os.ErrPermission) {
		return nil
	}
	return err
}

func hardlinkKey(info os.FileInfo) (linkKey, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return linkKey{}, false
	}
	return linkKey{dev: st.Dev, ino: st.Ino}, true
}

func fileTimes(info os.FileInfo) (atime, mtime time.Time) {
	mtime = info.ModTime()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atim.Sec, st.Atim.Nsec), mtime
	}
	return mtime, mtime
}

// copyXattrs is best effort: filesystems without xattr support and attributes
// the caller may not set (security.*, trusted.*) are not errors for a copy.
func copyXattrs(src, dst *os.File, destRel string) {
	names, err := listXattrs(src)
	if err != nil {
		return
	}
	for _, name := range names {
		value, err := getXattr(src, name)
		if err != nil {
			continue
		}
		if err := unix.Fsetxattr(int(dst.Fd()), name, value, 0); err != nil {
			slog.Debug("could not copy xattr", "component", "filebrowser", "subsystem", "file_operations", "path", destRel, "name", name, "error", err)
		}
	}
}

func listXattrs(f *os.File) ([]string, error) {
	fd := int(f.Fd())
	size, err := unix.Flistxattr(fd, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := unix.Flistxattr(fd, buf)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(buf[:n]), "\x00"), "\x00"), nil
}

func getXattr(f *os.File, name string) ([]byte, error) {
	fd := int(f.Fd())
	size, err := unix.Fgetxattr(fd, name, nil)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := unix.Fgetxattr(fd, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// ComputeCopySize calculates the total size of files to be copied.
// For directories, it recursively sums the sizes of all contained files.
func ComputeCopySize(path string) (int64, error) {
	path = fsroot.CleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	info, err := root.Root.Lstat(relPath(path))
	if err != nil {
		return 0, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return 0, nil
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return 0, nil
		}
		return info.Size(), nil
	}

	var totalSize int64
	err = root.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entryType := entry.Type()
		if entryType&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		entryInfo, entryInfoErr := entry.Info()
		if entryInfoErr != nil {
			return entryInfoErr
		}
		if !entryInfo.Mode().IsRegular() {
			return nil
		}
		totalSize += entryInfo.Size()
		return nil
	})

	return totalSize, err
}

type MoveFileOptions struct {
	KnownSize    int64
	HasKnownSize bool
}

// MoveFileWithCallbacks moves src to dst with progress callbacks. It renames
// when src and dst are on one filesystem and otherwise copies each entry and
// removes its source right after. With overwrite an existing destination file
// is replaced and an existing destination directory is merged.
func MoveFileWithCallbacks(src, dst string, overwrite bool, opts *OperationCallbacks, moveOpts MoveFileOptions) error {
	src = fsroot.CleanAbsPath(src)
	dst = fsroot.CleanAbsPath(dst)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	if validateErr := validateMoveDestination(root, src, dst); validateErr != nil {
		return validateErr
	}

	srcInfo, err := root.Root.Lstat(relPath(src))
	if err != nil {
		return err
	}

	return transferEntry(root, src, dst, srcInfo, overwrite, opts, newCopyState(true), moveOpts)
}

// tryRenameMove moves src with one rename. Only a cross-filesystem refusal
// (EXDEV) is reported as "not moved" so the caller can copy and delete
// instead; any other rename failure is returned as is.
func tryRenameMove(root *fsroot.FSRoot, src, dst string, opts *OperationCallbacks, moveOpts MoveFileOptions) (bool, error) {
	if err := root.Root.Rename(relPath(src), relPath(dst)); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return false, nil
		}
		return false, err
	}

	totalSize := moveOpts.KnownSize
	if !moveOpts.HasKnownSize {
		var sizeErr error
		totalSize, sizeErr = ComputeCopySize(dst)
		if sizeErr != nil {
			slog.Debug("failed to compute move size after rename", "component", "filebrowser", "subsystem", "file_operations", "path", dst, "error", sizeErr)
			return true, nil
		}
	}

	opts.ReportProgress(totalSize)
	return true, nil
}

type DeleteOptions struct {
	// Progress receives the running count of removed entries (files and
	// directories, including the deleted root). Totals and percentages are the
	// caller's concern, since only the caller can aggregate across a batch.
	Progress func(processed int64)
}

// CountEntries counts the entries an operation over absPath touches: absPath
// itself plus, when recursive, all of its descendants (files and directories).
func CountEntries(ctx context.Context, absPath string, recursive bool) (int64, error) {
	absPath = fsroot.CleanAbsPath(absPath)
	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()
	return countRecursiveEntries(ctx, root, absPath, recursive)
}

// TopLevelEntryCountWithin reports whether a directory has at most maxEntries direct children.
func TopLevelEntryCountWithin(absPath string, maxEntries int) (bool, error) {
	root, err := fsroot.Open()
	if err != nil {
		return false, err
	}
	defer root.Close()

	dir, err := root.Root.Open(relPath(absPath))
	if err != nil {
		return false, err
	}
	defer dir.Close()

	entries, err := dir.ReadDir(maxEntries + 1)
	if err != nil {
		return false, err
	}
	return len(entries) <= maxEntries, nil
}

// DeleteFilesWithProgress removes a file or directory while reporting item-count progress.
func DeleteFilesWithProgress(ctx context.Context, absPath string, opts DeleteOptions) (int64, error) {
	absPath = fsroot.CleanAbsPath(absPath)
	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	targetRel := relPath(absPath)
	if targetRel == "." {
		return 0, fmt.Errorf("cannot delete root")
	}

	info, err := root.Root.Lstat(targetRel)
	if err != nil {
		return 0, err
	}

	if !info.IsDir() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := root.Root.Remove(targetRel); err != nil {
			return 0, err
		}
		if opts.Progress != nil {
			opts.Progress(1)
		}
		return 1, nil
	}

	return deleteDirectoryWithProgress(ctx, root, absPath, opts.Progress)
}

func deleteDirectoryWithProgress(
	ctx context.Context,
	root *fsroot.FSRoot,
	absPath string,
	progress func(processed int64),
) (int64, error) {
	reporter := deleteItemReporter{progress: progress}

	dirs, err := deleteFilesDuringWalk(ctx, root, absPath, &reporter)
	if err != nil {
		return reporter.processed, err
	}
	sortDeleteDirsDeepestFirst(dirs)

	if err := removeDeleteDirs(ctx, root, dirs, &reporter); err != nil {
		return reporter.processed, err
	}

	return reporter.processed, nil
}

type deleteItemReporter struct {
	processed int64
	progress  func(processed int64)
}

func (r *deleteItemReporter) report() {
	r.processed++
	if r.progress != nil {
		r.progress(r.processed)
	}
}

func deleteFilesDuringWalk(ctx context.Context, root *fsroot.FSRoot, absPath string, reporter *deleteItemReporter) ([]string, error) {
	dirs := make([]string, 0)
	err := root.WalkDir(absPath, func(walkRel string, entry fs.DirEntry, walkErr error) error {
		return deleteWalkEntry(ctx, root, walkRel, entry, walkErr, &dirs, reporter)
	})
	return dirs, err
}

func deleteWalkEntry(
	ctx context.Context,
	root *fsroot.FSRoot,
	walkRel string,
	entry fs.DirEntry,
	walkErr error,
	dirs *[]string,
	reporter *deleteItemReporter,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if walkErr != nil {
		return walkErr
	}
	if entry.IsDir() {
		*dirs = append(*dirs, walkRel)
		return nil
	}
	if err := root.Root.Remove(walkRel); err != nil {
		return err
	}
	reporter.report()
	return nil
}

func sortDeleteDirsDeepestFirst(dirs []string) {
	slices.SortFunc(dirs, compareDeleteDirs)
}

func compareDeleteDirs(a, b string) int {
	depthA := strings.Count(a, "/")
	depthB := strings.Count(b, "/")
	if depthA != depthB {
		return depthB - depthA
	}
	if len(a) != len(b) {
		return len(b) - len(a)
	}
	return strings.Compare(a, b)
}

func removeDeleteDirs(ctx context.Context, root *fsroot.FSRoot, dirs []string, reporter *deleteItemReporter) error {
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Root.Remove(dir); err != nil {
			return err
		}
		reporter.report()
	}
	return nil
}

func isEditableTextContent(content []byte) bool {
	const maxNonPrintableRuneRatio = 0.05

	stringContent := string(content)
	if !utf8.ValidString(stringContent) {
		return false
	}
	return nonPrintableRuneRatio(stringContent) <= maxNonPrintableRuneRatio
}

func nonPrintableRuneRatio(content string) float64 {
	nonPrintableRuneCount := 0
	totalRuneCount := 0

	for _, r := range content {
		totalRuneCount++
		if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
			nonPrintableRuneCount++
		}
	}
	if totalRuneCount == 0 {
		return 0
	}
	return float64(nonPrintableRuneCount) / float64(totalRuneCount)
}

// ChangePermissionsCtx changes permissions and reports processed entries when requested.
func ChangePermissionsCtx(ctx context.Context, path string, mode os.FileMode, recursive bool, cb func(processed int64)) error {
	path = fsroot.CleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Check if path exists
	info, err := root.Root.Stat(relPath(path))
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	recursiveDir := info.IsDir() && recursive
	report := progressReporter(cb)

	if err := ctx.Err(); err != nil {
		return err
	}

	// Change permissions of the main path
	if err := root.Root.Chmod(relPath(path), mode); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}
	report()

	if recursiveDir {
		return changePermissionsRecursive(ctx, root, path, mode, report)
	}

	return nil
}

// ChangeOwnershipCtx changes ownership and reports processed entries when requested.
func ChangeOwnershipCtx(ctx context.Context, path string, uid, gid int, recursive bool, cb func(processed int64)) error {
	path = fsroot.CleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	info, err := root.Root.Lstat(relPath(path))
	if err != nil {
		return fmt.Errorf("failed to lstat path: %w", err)
	}

	recursiveDir := info.IsDir() && recursive
	report := progressReporter(cb)

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := root.Root.Lchown(relPath(path), uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s: %w", path, err)
	}
	report()

	if recursiveDir {
		return changeOwnershipRecursive(ctx, root, path, uid, gid, report)
	}

	return nil
}

func progressReporter(cb func(processed int64)) func() {
	var processed int64
	return func() {
		processed++
		if cb != nil {
			cb(processed)
		}
	}
}

func changePermissionsRecursive(ctx context.Context, root *fsroot.FSRoot, path string, mode os.FileMode, report func()) error {
	rootRel := relPath(path)
	return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during chmod", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if isRecursiveRoot(walkRel, rootRel) {
			return nil
		}
		if err := root.Root.Chmod(walkRel, mode); err != nil {
			slog.Error("failed to chmod path", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", err)
			return nil
		}
		report()
		return nil
	})
}

func changeOwnershipRecursive(ctx context.Context, root *fsroot.FSRoot, path string, uid, gid int, report func()) error {
	rootRel := relPath(path)
	return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during chown", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if isRecursiveRoot(walkRel, rootRel) {
			return nil
		}
		if err := root.Root.Lchown(walkRel, uid, gid); err != nil {
			slog.Error("failed to chown path", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", err)
		}
		report()
		return nil
	})
}

func isRecursiveRoot(walkRel, rootRel string) bool {
	return walkRel == "." || walkRel == rootRel
}

func countRecursiveEntries(ctx context.Context, root *fsroot.FSRoot, path string, recursive bool) (int64, error) {
	total := int64(1)
	if !recursive {
		return total, nil
	}
	err := root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during permission count", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if walkRel == "." || walkRel == relPath(path) {
			return nil
		}
		total++
		return nil
	})
	return total, err
}

// validateMoveDestination validates that a move operation is safe
func validateMoveDestination(root *fsroot.FSRoot, src, dst string) error {
	// Clean and normalize paths
	src = fsroot.CleanAbsPath(src)
	dst = fsroot.CleanAbsPath(dst)

	if src == dst {
		return fmt.Errorf("source and destination are the same")
	}

	// Check if source is a directory
	srcInfo, err := root.Root.Lstat(relPath(src))
	if err != nil {
		return err
	}

	isSrcDir := srcInfo.IsDir()

	// If source is a directory, check if destination is within source
	if isSrcDir {
		// Get the parent directory of the destination
		dstParent := filepath.Dir(dst)

		// Check if destination parent is the source directory or a subdirectory of it
		if strings.HasPrefix(dstParent+string(filepath.Separator), src+string(filepath.Separator)) || dstParent == src {
			return fmt.Errorf("cannot move directory '%s' to a location within itself: '%s'", src, dst)
		}
	}

	// Check if destination parent directory exists
	dstParent := filepath.Dir(dst)
	if dstParent != "." && dstParent != "/" {
		if _, err := root.Root.Stat(relPath(dstParent)); os.IsNotExist(err) {
			return fmt.Errorf("destination directory does not exist: '%s'", dstParent)
		}
	}

	return nil
}

// prepareDestination clears the way for a transfer onto dst. An existing dst
// is moved aside rather than deleted: the returned finish removes it after a
// successful transfer and puts it back after a failed one, so a transfer that
// dies halfway never loses what it was replacing. With removePartial, finish
// also drops whatever a failed transfer left at a previously empty dst, so a
// half-written tree never sits there under its final name; a move keeps it,
// since the entries already inside were removed from the source.
func prepareDestination(root *fsroot.FSRoot, dst string, exists, removePartial bool) (func(error), error) {
	aside := ""
	if exists {
		aside = fmt.Sprintf("%s.linuxio-replaced-%d", dst, time.Now().UnixNano())
		if err := root.Root.Rename(relPath(dst), relPath(aside)); err != nil {
			return nil, err
		}
	}
	return func(transferErr error) {
		if transferErr == nil {
			removeAside(root, aside)
			return
		}
		if aside != "" || removePartial {
			if err := root.Root.RemoveAll(relPath(dst)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("failed to clear partial destination", "component", "filebrowser", "subsystem", "file_operations", "path", dst, "error", err)
			}
		}
		restoreAside(root, aside, dst)
	}, nil
}

func removeAside(root *fsroot.FSRoot, aside string) {
	if aside == "" {
		return
	}
	if err := root.Root.RemoveAll(relPath(aside)); err != nil {
		slog.Warn("failed to remove replaced destination", "component", "filebrowser", "subsystem", "file_operations", "path", aside, "error", err)
	}
}

func restoreAside(root *fsroot.FSRoot, aside, dst string) {
	if aside == "" {
		return
	}
	if err := root.Root.Rename(relPath(aside), relPath(dst)); err != nil {
		slog.Error("failed to restore replaced destination", "component", "filebrowser", "subsystem", "file_operations", "path", aside, "destination", dst, "error", err)
	}
}
