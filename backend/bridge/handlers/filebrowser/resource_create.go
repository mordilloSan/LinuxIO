package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
)

type resourcePostRequest struct {
	cleanPath string
	relPath   string
	isDir     bool
	override  bool
}

func parseResourcePostRequest(req apischema.FileResourcePostRequest) (resourcePostRequest, error) {
	if req.Path == "" {
		return resourcePostRequest{}, fmt.Errorf("bad_request:missing path")
	}

	path, err := url.QueryUnescape(req.Path)
	if err != nil {
		return resourcePostRequest{}, fmt.Errorf("bad_request:invalid path encoding")
	}

	cleanPath := fsroot.CleanAbsPath(path)
	if cleanPath == "/" {
		return resourcePostRequest{}, fmt.Errorf("bad_request:cannot create root")
	}

	return resourcePostRequest{
		cleanPath: cleanPath,
		relPath:   strings.TrimPrefix(cleanPath, "/"),
		isDir:     strings.HasSuffix(path, "/"),
		override:  req.Override != nil && *req.Override,
	}, nil
}

func ensureResourcePostType(root *fsroot.FSRoot, req resourcePostRequest) error {
	stat, err := root.Root.Stat(req.relPath)
	if err != nil {
		return nil
	}
	if stat.IsDir() != req.isDir && !req.override {
		return fmt.Errorf("bad_request:resource already exists with different type")
	}
	return nil
}

func createDirectoryResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	if stat, statErr := root.Root.Stat(req.relPath); statErr == nil && !stat.IsDir() && req.override {
		if removeErr := root.Root.Remove(req.relPath); removeErr != nil {
			slog.Debug("error removing existing file for directory create", "path", req.cleanPath, "error", removeErr)
			return nil, fmt.Errorf("bad_request:%w", removeErr)
		}
	}

	if mkdirErr := root.Root.MkdirAll(req.relPath, services.PermDir); mkdirErr != nil {
		slog.Debug("error writing directory", "path", req.cleanPath, "error", mkdirErr)
		return nil, fmt.Errorf("bad_request:%w", mkdirErr)
	}
	if chmodErr := root.Root.Chmod(req.relPath, services.PermDir); chmodErr != nil {
		slog.Debug("error setting directory permissions", "path", req.cleanPath, "error", chmodErr)
		return nil, fmt.Errorf("bad_request:%w", chmodErr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath)
	slog.Info("directory created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

func createFileResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	parentRel := filepath.Dir(req.relPath)
	if parentRel != "." {
		if mkdirErr := root.Root.MkdirAll(parentRel, services.PermDir); mkdirErr != nil {
			slog.Debug("error creating parent directory", "path", req.cleanPath, "error", mkdirErr)
			return nil, fmt.Errorf("bad_request:failed to create parent directory: %w", mkdirErr)
		}
	}

	if _, statErr := root.Root.Stat(req.relPath); statErr == nil && !req.override {
		return nil, fmt.Errorf("bad_request:file already exists")
	}

	f, err := root.Root.OpenFile(req.relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, services.PermFile)
	if err != nil {
		slog.Debug("error creating file", "path", req.cleanPath, "error", err)
		return nil, fmt.Errorf("bad_request:%w", err)
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
		slog.Debug("error opening filesystem root", "error", err)
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
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
