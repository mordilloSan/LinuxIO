package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// resourcePatchRequest never overwrites: the request contract carries no
// override flag, so an existing destination is always a conflict.
type resourcePatchRequest struct {
	action   string
	src      string
	dst      string
	realSrc  string
	realDest string
}

func parseResourcePatchRequest(req apischema.ActionSourceDestinationRequest) (resourcePatchRequest, error) {
	if req.Action == "" || req.Source == "" || req.Dest == "" {
		return resourcePatchRequest{}, fmt.Errorf("%w: missing action, from, or destination", bridgeipc.ErrInvalidArgs)
	}

	src, err := url.QueryUnescape(req.Source)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("%w: malformed source path encoding", bridgeipc.ErrInvalidArgs)
	}
	dst, err := url.QueryUnescape(req.Dest)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("%w: malformed destination path encoding", bridgeipc.ErrInvalidArgs)
	}
	if dst == "/" || src == "/" {
		return resourcePatchRequest{}, fmt.Errorf("%w: cannot modify root directory", bridgeipc.ErrInvalidArgs)
	}
	if invalidEntryName(filepath.Base(strings.TrimRight(dst, "/"))) {
		return resourcePatchRequest{}, fmt.Errorf("%w: invalid name", bridgeipc.ErrInvalidArgs)
	}

	return resourcePatchRequest{
		action: req.Action,
		src:    src,
		dst:    dst,
	}, nil
}

func prepareResourcePatch(root *fsroot.FSRoot, req resourcePatchRequest) (resourcePatchRequest, error) {
	dstClean := strings.TrimRight(req.dst, "/")
	parentDir := filepath.Dir(dstClean)
	if _, err := root.Root.Stat(fsroot.ToRel(parentDir)); err != nil {
		return req, fsError("stat parent directory", err)
	}

	req.realDest = filepath.Join(parentDir, filepath.Base(dstClean))
	if strings.HasSuffix(req.dst, "/") && !strings.HasSuffix(req.realDest, "/") {
		req.realDest += "/"
	}
	req.realSrc = fsroot.CleanAbsPath(req.src)

	if _, err := root.Root.Lstat(fsroot.ToRel(req.realSrc)); err != nil {
		return req, fsError("stat source", err)
	}
	if err := services.CheckNotProtected(root, req.realSrc); err != nil {
		return req, fmt.Errorf("%w: %w", bridgeipc.ErrForbidden, err)
	}
	return req, validatePatchDestination(root, req)
}

func validatePatchDestination(root *fsroot.FSRoot, req resourcePatchRequest) error {
	_, err := root.Root.Lstat(fsroot.ToRel(req.realDest))
	switch {
	case err == nil && req.realSrc == req.realDest:
		return fmt.Errorf("%w: source and destination are the same", bridgeipc.ErrInvalidArgs)
	case err == nil:
		return fmt.Errorf("%w: destination already exists", bridgeipc.ErrConflict)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fsError("stat destination", err)
	}
}

// executeResourcePatch renames: copying and moving selections go through the
// batch tasks, which carry conflict handling and progress.
func executeResourcePatch(req resourcePatchRequest, opts *services.OperationCallbacks, size computedTransferSize) error {
	switch req.action {
	case "rename", "move":
		return services.MoveFileWithCallbacks(req.realSrc, req.realDest, false, opts, moveFileOptions(size))
	default:
		return fmt.Errorf("%w: unsupported action: %s", bridgeipc.ErrInvalidArgs, req.action)
	}
}

// resourcePatchWithProgress performs patch operations with progress feedback.
func resourcePatchWithProgress(ctx context.Context, req apischema.ActionSourceDestinationRequest, task *bridgeipc.Task) (apischema.MessageResponse, error) {
	patchReq, err := parseResourcePatchRequest(req)
	if err != nil {
		return apischema.MessageResponse{}, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.MessageResponse{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	patchReq, err = prepareResourcePatch(root, patchReq)
	if err != nil {
		return apischema.MessageResponse{}, err
	}

	srcInfo, err := root.Root.Lstat(fsroot.ToRel(patchReq.realSrc))
	if err != nil {
		return apischema.MessageResponse{}, fsError("stat source", err)
	}

	// A same-filesystem rename needs neither a size nor a walk.
	var size computedTransferSize
	srcDev, srcOK := deviceID(root, patchReq.realSrc)
	destDev, destOK := deviceID(root, filepath.Dir(patchReq.realDest))
	if !srcOK || !destOK || srcDev != destDev {
		size = computeTransferSize(ctx, patchReq.realSrc, srcInfo)
	}
	// Send initial progress.
	slog.Debug("starting filebrowser operation",
		"action", patchReq.action,
		"source", patchReq.realSrc,
		"destination", patchReq.realDest,
		"size", size.total)
	task.ReportProgress(FileProgress{
		Total: size.total,
		Phase: "preparing",
	})

	opts := newTaskPhaseCallbacks(ctx, task, nil, size.estimate(), "moving")
	if err := executeResourcePatch(patchReq, opts, size); err != nil {
		return apischema.MessageResponse{}, fsError(patchReq.action, err)
	}

	notifyIndexerAfterPatch(ctx, root, patchReq)
	slog.Info("filebrowser operation completed", "action", patchReq.action, "source", patchReq.realSrc, "destination", patchReq.realDest)
	return apischema.MessageResponse{Message: "operation completed"}, nil
}

// generateUniquePath generates a unique path by appending a suffix like " (copy)" or " (copy 2)"
func generateUniquePath(path string, isDir bool, root *fsroot.FSRoot) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// For files, split name and extension
	var name, ext string
	if !isDir {
		ext = filepath.Ext(base)
		name = strings.TrimSuffix(base, ext)
	} else {
		name = base
	}

	// Try "name (copy).ext" first
	newPath := filepath.Join(dir, name+" (copy)"+ext)
	if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); errors.Is(err, fs.ErrNotExist) {
		return newPath
	}

	// Try "name (copy 2).ext", "name (copy 3).ext", etc.
	for i := 2; i < 1000; i++ {
		newPath = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, i, ext))
		if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); errors.Is(err, fs.ErrNotExist) {
			return newPath
		}
	}

	// Fallback to timestamp-based name
	timestamp := time.Now().Unix()
	return filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, timestamp, ext))
}
