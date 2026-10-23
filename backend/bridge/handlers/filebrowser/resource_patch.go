package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
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
		return resourcePatchRequest{}, fmt.Errorf("bad_request:missing action, from, or destination")
	}

	src, err := url.QueryUnescape(req.Source)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid source path encoding")
	}
	dst, err := url.QueryUnescape(req.Dest)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid destination path encoding")
	}
	if dst == "/" || src == "/" {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:cannot modify root directory")
	}
	if invalidEntryName(filepath.Base(strings.TrimRight(dst, "/"))) {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid name")
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
		slog.Debug("parent directory not found", "path", parentDir, "error", err)
		return req, fmt.Errorf("bad_request:parent directory not found")
	}

	req.realDest = filepath.Join(parentDir, filepath.Base(dstClean))
	if strings.HasSuffix(req.dst, "/") && !strings.HasSuffix(req.realDest, "/") {
		req.realDest += "/"
	}
	req.realSrc = fsroot.CleanAbsPath(req.src)

	if _, err := root.Root.Lstat(fsroot.ToRel(req.realSrc)); err != nil {
		slog.Debug("error getting source info", "path", req.realSrc, "error", err)
		return req, fmt.Errorf("bad_request:source not found")
	}
	if err := services.CheckNotProtected(root, req.realSrc); err != nil {
		return req, fmt.Errorf("bad_request:%w", err)
	}
	return req, validatePatchDestination(root, req)
}

func validatePatchDestination(root *fsroot.FSRoot, req resourcePatchRequest) error {
	_, err := root.Root.Lstat(fsroot.ToRel(req.realDest))
	switch {
	case err == nil && req.realSrc == req.realDest:
		return fmt.Errorf("bad_request:source and destination are the same")
	case err == nil:
		return fmt.Errorf("bad_request:destination exists")
	case os.IsNotExist(err):
		return nil
	default:
		slog.Debug("error stating destination", "path", req.realDest, "error", err)
		return fmt.Errorf("bad_request:could not stat destination")
	}
}

// executeResourcePatch renames: copying and moving selections go through the
// batch tasks, which carry conflict handling and progress.
func executeResourcePatch(req resourcePatchRequest, opts *services.OperationCallbacks, size computedTransferSize) error {
	switch req.action {
	case "rename", "move":
		return services.MoveFileWithCallbacks(req.realSrc, req.realDest, false, opts, moveFileOptions(size))
	default:
		return fmt.Errorf("bad_request:unsupported action: %s", req.action)
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
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	patchReq, err = prepareResourcePatch(root, patchReq)
	if err != nil {
		return apischema.MessageResponse{}, err
	}

	srcInfo, err := root.Root.Lstat(fsroot.ToRel(patchReq.realSrc))
	if err != nil {
		slog.Debug("error getting source info", "path", patchReq.realSrc, "error", err)
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:source not found")
	}

	size := computeTransferSize(ctx, patchReq.realSrc, srcInfo)
	// Send initial progress.
	slog.Info("starting filebrowser operation",
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
		slog.Debug("error patching resource", "action", patchReq.action, "source", patchReq.realSrc, "destination", patchReq.realDest, "error", err)
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:%w", err)
	}

	notifyIndexerAfterPatch(ctx, root, patchReq)
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
	if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
		return newPath
	}

	// Try "name (copy 2).ext", "name (copy 3).ext", etc.
	for i := 2; i < 1000; i++ {
		newPath = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, i, ext))
		if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
			return newPath
		}
	}

	// Fallback to timestamp-based name
	timestamp := time.Now().Unix()
	return filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, timestamp, ext))
}
