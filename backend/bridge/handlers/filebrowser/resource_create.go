package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

type resourcePostRequest struct {
	cleanPath string
	relPath   string
	isDir     bool
}

// invalidEntryName reports the names no entry can have. Slashes are left to
// the callers: folder creation accepts nested paths on purpose.
func invalidEntryName(name string) bool {
	return name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0)
}

func parseResourcePostRequest(req apischema.FileResourcePostRequest) (resourcePostRequest, error) {
	if req.Path == "" {
		return resourcePostRequest{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}

	path := req.Path
	cleanPath := fsroot.CleanAbsPath(path)
	if cleanPath == "/" {
		return resourcePostRequest{}, fmt.Errorf("%w: cannot create root", bridgeipc.ErrInvalidArgs)
	}
	if invalidEntryName(filepath.Base(strings.TrimRight(path, "/"))) {
		return resourcePostRequest{}, fmt.Errorf("%w: invalid name", bridgeipc.ErrInvalidArgs)
	}

	return resourcePostRequest{
		cleanPath: cleanPath,
		relPath:   strings.TrimPrefix(cleanPath, "/"),
		isDir:     strings.HasSuffix(path, "/"),
	}, nil
}

func ensureResourcePostType(root *fsroot.FSRoot, req resourcePostRequest) error {
	stat, err := root.Root.Stat(req.relPath)
	if err != nil {
		return nil
	}
	if stat.IsDir() != req.isDir {
		return fmt.Errorf("%w: resource already exists with a different type", bridgeipc.ErrConflict)
	}
	return nil
}

// nearestDirForCreate opens the deepest folder of a path to create in that
// exists and returns the names below it that are missing. On a root bridge
// acting for owner it is reached without following a link the session user
// could have planted (see services.OpenNearestDirNoUserLinks).
func nearestDirForCreate(root *fsroot.FSRoot, cleanPath, rel string, owner services.Owner) (*os.File, []string, error) {
	if !owner.Set {
		return services.OpenNearestDir(root, rel)
	}
	return services.OpenNearestDirNoUserLinks(root, cleanPath, owner)
}

// createDirectoryResource makes the folder and any missing folder above it
// through the handle of the nearest existing one, never by path, so a folder
// on the way that a process of the session user swapped for a link cannot
// send the create, or the mode set after it, elsewhere.
func createDirectoryResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest, owner services.Owner) (any, error) {
	dir, missing, err := nearestDirForCreate(root, req.cleanPath, req.relPath, owner)
	if err != nil {
		return nil, fsError("create directory", err)
	}
	// An existing folder is refused: the mode set below would rewrite its mode.
	if len(missing) == 0 {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: folder already exists", bridgeipc.ErrConflict)
	}
	name := missing[len(missing)-1]
	parent, err := services.MkdirAllAt(dir, missing[:len(missing)-1], services.PermDir, services.Owner{})
	if err != nil {
		return nil, fsError("create directory", err)
	}
	defer func() { _ = parent.Close() }()

	if mkdirErr := unix.Mkdirat(int(parent.Fd()), name, uint32(services.PermDir)); mkdirErr != nil {
		return nil, fsError("create directory", &fs.PathError{Op: "mkdirat", Path: req.cleanPath, Err: mkdirErr})
	}
	// The umask masked the mode: set it on the folder's own handle, opened
	// without following a link put at its name since.
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fsError("set directory permissions", &fs.PathError{Op: "openat", Path: req.cleanPath, Err: err})
	}
	chmodErr := unix.Fchmod(fd, uint32(services.PermDir))
	_ = unix.Close(fd)
	if chmodErr != nil {
		return nil, fsError("set directory permissions", &fs.PathError{Op: "fchmod", Path: req.cleanPath, Err: chmodErr})
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath)
	slog.Info("directory created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

// createFileResource makes an empty file, and any missing folder above it,
// through the handle of the nearest existing folder. O_EXCL refuses a file or
// a link already at the name, so nothing that exists is ever truncated.
func createFileResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest, owner services.Owner) (any, error) {
	dir, missing, err := nearestDirForCreate(root, filepath.Dir(req.cleanPath), filepath.Dir(req.relPath), owner)
	if err != nil {
		return nil, fsError("create parent directory", err)
	}
	parent, err := services.MkdirAllAt(dir, missing, services.PermDir, services.Owner{})
	if err != nil {
		return nil, fsError("create parent directory", err)
	}
	defer func() { _ = parent.Close() }()

	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(req.cleanPath), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(services.PermFile))
	if errors.Is(err, unix.EEXIST) {
		return nil, fmt.Errorf("%w: file already exists", bridgeipc.ErrConflict)
	}
	if err != nil {
		return nil, fsError("create file", &fs.PathError{Op: "openat", Path: req.cleanPath, Err: err})
	}
	if cerr := unix.Close(fd); cerr != nil {
		slog.Warn("failed to close created file", "path", req.cleanPath, "error", cerr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath)
	slog.Info("file created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

// resourcePost creates a new resource for the bridge acting for owner (see
// actingFor).
func resourcePost(ctx context.Context, req apischema.FileResourcePostRequest, owner services.Owner) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	postReq, err := parseResourcePostRequest(req)
	if err != nil {
		return nil, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	defer func() {
		if cerr := root.Close(); cerr != nil {
			slog.Warn("failed to close filesystem root", "error", cerr)
		}
	}()

	if err := ensureResourcePostType(root, postReq); err != nil {
		return nil, err
	}

	if postReq.isDir {
		return createDirectoryResource(ctx, root, postReq, owner)
	}
	return createFileResource(ctx, root, postReq, owner)
}
