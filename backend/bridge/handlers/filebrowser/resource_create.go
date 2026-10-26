package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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

func createDirectoryResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	// MkdirAll would accept an existing folder and the chmod below would then
	// rewrite its mode, so an existing folder has to be refused here.
	if _, statErr := root.Root.Stat(req.relPath); statErr == nil {
		return nil, fmt.Errorf("%w: folder already exists", bridgeipc.ErrConflict)
	}

	if mkdirErr := root.Root.MkdirAll(req.relPath, services.PermDir); mkdirErr != nil {
		return nil, fsError("create directory", mkdirErr)
	}
	if chmodErr := root.Root.Chmod(req.relPath, services.PermDir); chmodErr != nil {
		return nil, fsError("set directory permissions", chmodErr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath)
	slog.Info("directory created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

func createFileResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	parentRel := filepath.Dir(req.relPath)
	if parentRel != "." {
		if mkdirErr := root.Root.MkdirAll(parentRel, services.PermDir); mkdirErr != nil {
			return nil, fsError("create parent directory", mkdirErr)
		}
	}

	if _, statErr := root.Root.Stat(req.relPath); statErr == nil {
		return nil, fmt.Errorf("%w: file already exists", bridgeipc.ErrConflict)
	}

	f, err := root.Root.OpenFile(req.relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, services.PermFile)
	if err != nil {
		return nil, fsError("create file", err)
	}
	if cerr := f.Close(); cerr != nil {
		slog.Warn("failed to close created file", "path", req.cleanPath, "error", cerr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath)
	slog.Info("file created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

// resourcePost creates a new resource.
func resourcePost(ctx context.Context, req apischema.FileResourcePostRequest) (any, error) {
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
		return createDirectoryResource(ctx, root, postReq)
	}
	return createFileResource(ctx, root, postReq)
}
