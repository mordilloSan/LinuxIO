package filebrowser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/filebrowser/backend/adapters/fs/files"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
	"github.com/mordilloSan/filebrowser/backend/common/utils"
	"github.com/mordilloSan/filebrowser/backend/indexing/iteminfo"
	"github.com/mordilloSan/filebrowser/backend/preview"
	"github.com/mordilloSan/go_logger/logger"
)

type FileCache interface {
	Store(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) ([]byte, bool, error)
	Delete(ctx context.Context, key string) error
}

// isClientCancellation checks if an error is due to client cancellation (navigation away)
func isClientCancellation(ctx context.Context, err error) bool {
	// Check context state first
	if ctx.Err() == context.Canceled {
		return true
	}

	// Check if the error chain contains context cancellation
	return errors.Is(err, context.Canceled)
}

// previewHandler handles the preview request for images.
// @Summary Get image preview
// @Description Returns a preview image based on the requested path and size.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "File path of the image to preview"
// @Param size query string false "Preview size ('small' or 'original'). Defaults to 'small'."
// @Success 200 {file} file "Preview image content"
// @Failure 400 {object} map[string]string "Invalid request path"
// @Failure 404 {object} map[string]string "File not found"
// @Failure 415 {object} map[string]string "Unsupported file type for preview"
// @Failure 500 {object} map[string]string "Internal server error"
// @Failure 501 {object} map[string]string "Preview generation not implemented"
// @Router /api/preview [get]
func previewHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	path := r.URL.Query().Get("path")
	rawSource := r.URL.Query().Get("source")
	var err error
	// decode url encoded source name
	if rawSource != "" {
		if _, err = url.PathUnescape(rawSource); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath
	if path == "" {
		return http.StatusBadRequest, fmt.Errorf("invalid request path")
	}
	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Metadata: true,
	})
	if err != nil {
		return errToStatus(err), err
	}
	d.fileInfo = *fileInfo
	return previewHelperFunc(w, r, d)
}

func rawFileHandler(w http.ResponseWriter, r *http.Request, file iteminfo.ExtendedFileInfo) (int, error) {
	// Use file.RealPath directly (already resolved by FileInfoFaster)
	fd, err := os.Open(file.RealPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	defer fd.Close()

	setContentDisposition(w, r, file.Name)

	w.Header().Set("Cache-Control", "private")
	http.ServeContent(w, r, file.Name, file.ModTime, fd)
	return 0, nil
}

func previewHelperFunc(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	previewSize := r.URL.Query().Get("size")
	if previewSize != "original" {
		previewSize = "small"
	}
	if !d.fileInfo.HasPreview {
		return http.StatusBadRequest, fmt.Errorf("this item does not have a preview")
	}
	if d.fileInfo.Type == "directory" {
		return http.StatusBadRequest, fmt.Errorf("folder previews are disabled")
	}

	setContentDisposition(w, r, d.fileInfo.Name)
	isImage := strings.HasPrefix(d.fileInfo.Type, "image")
	ext := strings.ToLower(filepath.Ext(d.fileInfo.Name))
	resizable := iteminfo.ResizableImageTypes[ext]
	if (!resizable) && isImage {
		return rawFileHandler(w, r, d.fileInfo)
	}
	seekPercentage := 0
	percentage := r.URL.Query().Get("atPercentage")
	if percentage != "" {
		var err error
		// convert string to int
		seekPercentage, err = strconv.Atoi(percentage)
		if err != nil {
			seekPercentage = 10
		}
		if seekPercentage < 0 || seekPercentage > 100 {
			seekPercentage = 10
		}
	}

	// Use the context from the request context (which includes timeout)
	ctx := r.Context()
	if d.ctx != nil {
		ctx = d.ctx
	}

	previewImg, err := preview.GetPreviewForFile(ctx, d.fileInfo, previewSize, "", seekPercentage)
	if err != nil {
		// Check if it was a context cancellation (client navigated away)
		if isClientCancellation(ctx, err) {
			// Return 200 to avoid error logging - client cancellation is normal
			return http.StatusOK, nil
		}

		// Check if it was a context timeout (server-side timeout)
		if ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
			logger.Errorf("Preview timeout for file '%s' after 60 seconds", d.fileInfo.Name)
			return http.StatusRequestTimeout, fmt.Errorf("preview generation timed out after 60 seconds")
		}

		// Log detailed error information for actual server errors
		logger.Errorf("Preview generation failed for file '%s' (type: %s, size: %s, seek: %d%%): %v",
			d.fileInfo.Name, d.fileInfo.Type, previewSize, seekPercentage, err)

		return http.StatusInternalServerError, err
	}
	w.Header().Set("Cache-Control", "private")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, d.fileInfo.Name+"-preview.jpg", d.fileInfo.ModTime, bytes.NewReader(previewImg))
	return 0, nil
}
