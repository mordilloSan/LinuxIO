package filebrowser

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/filebrowser/backend/adapters/fs/files"
	"github.com/mordilloSan/filebrowser/backend/adapters/fs/fileutils"
	"github.com/mordilloSan/filebrowser/backend/common/errors"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
	"github.com/mordilloSan/filebrowser/backend/common/utils"
	"github.com/mordilloSan/filebrowser/backend/indexing/iteminfo"
	"github.com/mordilloSan/filebrowser/backend/preview"
	"github.com/mordilloSan/go_logger/logger"
)

// validateMoveOperation checks if a move/rename operation is valid at the HTTP level
// It prevents moving a directory into itself or its subdirectories
func validateMoveOperation(src, dst string, isSrcDir bool) error {
	// Clean and normalize paths
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

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
		if _, err := os.Stat(dstParent); os.IsNotExist(err) {
			return fmt.Errorf("destination directory does not exist: '%s'", dstParent)
		}
	}

	return nil
}

type resourceStatData struct {
	Mode        string `json:"mode"`
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Size        int64  `json:"size"`
	Modified    string `json:"modified"`
	Raw         string `json:"raw"`
	Permissions string `json:"permissions"`
	Path        string `json:"path"`
	RealPath    string `json:"realPath"`
	Name        string `json:"name"`
}

// resourceGetHandler retrieves information about a resource.
// @Summary Get resource information
// @Description Returns metadata and optionally file contents for a specified resource path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Param content query string false "Include file content if true"
// @Success 200 {object} iteminfo.FileInfo "Resource metadata"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [get]
func resourceGetHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	encodedPath := r.URL.Query().Get("path")
	rawSource := r.URL.Query().Get("source")
	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath
	getContent := r.URL.Query().Get("content") == "true"
	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Expand:   true,
		Content:  getContent,
	})
	if err != nil {
		return errToStatus(err), err
	}
	if fileInfo.Type == "directory" {
		return renderJSON(w, r, fileInfo)
	}
	return renderJSON(w, r, fileInfo)
}

// resourceStatHandler returns extended metadata.
// @Summary Get extended file information
// @Description Gets all stats for the requested resource and returns the parsed output.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 {object} resourceStatData "Extended resource metadata"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources/stat [get]
func resourceStatHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	encodedPath := r.URL.Query().Get("path")
	rawSource := r.URL.Query().Get("source")

	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath

	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Expand:   false,
	})
	if err != nil {
		return errToStatus(err), err
	}

	statData, err := collectStatInfo(fileInfo.RealPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	statData.Path = path
	statData.Name = fileInfo.Name
	if statData.Size == 0 {
		statData.Size = fileInfo.Size
	}

	return renderJSON(w, r, statData)
}

// resourceDeleteHandler deletes a resource at a specified path.
// @Summary Delete a resource
// @Description Deletes a resource located at the specified path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 "Resource deleted successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [delete]
func resourceDeleteHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	encodedPath := r.URL.Query().Get("path")
	rawSource := r.URL.Query().Get("source")
	var err error
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath
	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	if path == "/" {
		return http.StatusForbidden, nil
	}
	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Expand:   false,
	})
	if err != nil {
		return errToStatus(err), err
	}

	// delete thumbnails
	preview.DelThumbs(r.Context(), *fileInfo)

	err = files.DeleteFiles(source, fileInfo.RealPath, filepath.Dir(fileInfo.RealPath))
	if err != nil {
		return errToStatus(err), err
	}
	return http.StatusOK, nil
}

// resourcePostHandler creates or uploads a new resource.
// @Summary Create or upload a resource
// @Description Creates a new resource or uploads a file at the specified path. Supports file uploads and directory creation.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "url encoded destination path where to place the files inside the destination source, a directory must end in / to create a directory"
// @Param source query string true "Name for the desired filebrowser destination source name, default is used if not provided"
// @Param override query bool false "Override existing file if true"
// @Success 200 "Resource created successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 409 {object} map[string]string "Conflict - Resource already exists"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [post]
func resourcePostHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	path := r.URL.Query().Get("path")
	rawSource := r.URL.Query().Get("source")
	var err error
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			logger.Debugf("invalid source encoding: %v", err)
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath
	path, err = url.QueryUnescape(path)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	// Determine if this is a directory or file based on trailing slash
	isDir := strings.HasSuffix(path, "/")

	fileOpts := utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Expand:   false,
	}
	// Direct filesystem access
	realPath := filepath.Join(source, path)

	// Check for file/folder conflicts before creation
	if stat, statErr := os.Stat(realPath); statErr == nil {
		// Path exists, check for type conflicts
		existingIsDir := stat.IsDir()
		requestingDir := isDir

		// If type mismatch (file vs folder or folder vs file) and not overriding
		if existingIsDir != requestingDir && r.URL.Query().Get("override") != "true" {
			return http.StatusConflict, nil
		}
	}

	// Directories creation on POST.
	if isDir {
		err = files.WriteDirectory(fileOpts)
		if err != nil {
			logger.Debugf("error writing directory: %v", err)
			return errToStatus(err), err
		}
		return http.StatusOK, nil
	}

	// Handle Chunked Uploads
	chunkOffsetStr := r.Header.Get("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		var offset int64
		offset, err = strconv.ParseInt(chunkOffsetStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid chunk offset: %v", err)
			return http.StatusBadRequest, fmt.Errorf("invalid chunk offset: %v", err)
		}

		var totalSize int64
		totalSizeStr := r.Header.Get("X-File-Total-Size")
		totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid total size: %v", err)
			return http.StatusBadRequest, fmt.Errorf("invalid total size: %v", err)
		}
		// On the first chunk, check for conflicts or handle override
		if offset == 0 {
			// Check for file/folder conflicts for chunked uploads
			if stat, statErr := os.Stat(realPath); statErr == nil {
				existingIsDir := stat.IsDir()
				requestingDir := false // Files are never directories

				// If type mismatch (existing dir vs requesting file) and not overriding
				if existingIsDir != requestingDir && r.URL.Query().Get("override") != "true" {
					logger.Debugf("Type conflict detected in chunked: existing is dir=%v, requesting dir=%v at path=%v", existingIsDir, requestingDir, realPath)
					return http.StatusConflict, nil
				}
			}

			var fileInfo *iteminfo.ExtendedFileInfo
			fileInfo, err = files.FileInfoFaster(fileOpts)
			if err == nil { // File exists
				if r.URL.Query().Get("override") != "true" {
					logger.Debugf("resource already exists: %v", fileInfo.RealPath)
					logger.Debugf("Resource already exists: %v", fileInfo.RealPath)
					return http.StatusConflict, nil
				}
				// If overriding, delete existing thumbnails
				preview.DelThumbs(r.Context(), *fileInfo)
			}
		}

		// Use a temporary file in the cache directory for chunks.
		// Create a unique name for the temporary file to avoid collisions.
		hasher := md5.New()
		hasher.Write([]byte(realPath))
		uploadID := hex.EncodeToString(hasher.Sum(nil))
		tempFilePath := filepath.Join(settings.Config.Server.CacheDir, "uploads", uploadID)

		if err = os.MkdirAll(filepath.Dir(tempFilePath), fileutils.PermDir); err != nil {
			logger.Debugf("could not create temp dir: %v", err)
			return http.StatusInternalServerError, fmt.Errorf("could not create temp dir: %v", err)
		}
		// Create or open the temporary file
		var outFile *os.File
		outFile, err = os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, fileutils.PermFile)
		if err != nil {
			logger.Debugf("could not open temp file: %v", err)
			return http.StatusInternalServerError, fmt.Errorf("could not open temp file: %v", err)
		}
		defer outFile.Close()

		// Seek to the correct offset to write the chunk
		_, err = outFile.Seek(offset, 0)
		if err != nil {
			logger.Debugf("could not seek in temp file: %v", err)
			return http.StatusInternalServerError, fmt.Errorf("could not seek in temp file: %v", err)
		}

		// Write the request body (the chunk) to the file
		var chunkSize int64
		chunkSize, err = io.Copy(outFile, r.Body)
		if err != nil {
			logger.Debugf("could not write chunk to temp file: %v", err)
			return http.StatusInternalServerError, fmt.Errorf("could not write chunk to temp file: %v", err)
		}
		// check if the file is complete
		if (offset + chunkSize) >= totalSize {
			// close file before moving
			outFile.Close()
			// Move the completed file from the temp location to the final destination
			err = fileutils.MoveFile(tempFilePath, realPath)
			if err != nil {
				logger.Debugf("could not move temp file to destination: %v", err)
				return http.StatusInternalServerError, fmt.Errorf("could not move temp file to destination: %v", err)
			}
			go files.RefreshIndex(source, realPath, false, false) //nolint:errcheck
		}

		return http.StatusOK, nil
	}

	// Check for file/folder conflicts for non-chunked uploads
	if stat, statErr := os.Stat(realPath); statErr == nil {
		existingIsDir := stat.IsDir()
		requestingDir := false // Files are never directories

		// If type mismatch (existing dir vs requesting file) and not overriding
		if existingIsDir != requestingDir && r.URL.Query().Get("override") != "true" {
			return http.StatusConflict, nil
		}
	}

	fileInfo, err := files.FileInfoFaster(fileOpts)
	if err == nil {
		if r.URL.Query().Get("override") != "true" {
			return http.StatusConflict, nil
		}

		preview.DelThumbs(r.Context(), *fileInfo)
	}
	err = files.WriteFile(fileOpts, r.Body)
	if err != nil {
		logger.Debugf("error writing file: %v", err)
		return errToStatus(err), err

	}
	return http.StatusOK, nil
}

// resourcePutHandler updates an existing file resource.
// @Summary Update a file resource
// @Description Updates an existing file at the specified path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Destination path where to place the files inside the destination source"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 "Resource updated successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 405 {object} map[string]string "Method not allowed"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [put]
func resourcePutHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	rawSource := r.URL.Query().Get("source")
	var err error
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
		}
	}
	source := settings.RootPath

	encodedPath := r.URL.Query().Get("path")

	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	// Only allow PUT for files.
	if strings.HasSuffix(path, "/") {
		return http.StatusMethodNotAllowed, nil
	}

	fileOpts := utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Source:   source,
		Expand:   false,
	}

	// Check access control for the target path
	err = files.WriteFile(fileOpts, r.Body)
	return errToStatus(err), err
}

// resourcePatchHandler performs a patch operation (e.g., move, rename) on a resource.
// @Summary Patch resource (move/rename)
// @Description Moves or renames a resource to a new destination.
// @Tags Resources
// @Accept json
// @Produce json
// @Param from query string true "Path from resource in <source_name>::<index_path> format"
// @Param destination query string true "Destination path for the resource"
// @Param action query string true "Action to perform (copy, rename)"
// @Param overwrite query bool false "Overwrite if destination exists"
// @Param rename query bool false "Rename if destination exists"
// @Success 200 "Resource moved/renamed successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 409 {object} map[string]string "Conflict - Destination exists"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [patch]
func resourcePatchHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	action := r.URL.Query().Get("action")

	encodedFrom := r.URL.Query().Get("from")
	// Decode the URL-encoded path
	src, err := url.QueryUnescape(encodedFrom)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid path encoding: %v", err)
	}
	dst := r.URL.Query().Get("destination")
	dst, err = url.QueryUnescape(dst)
	if err != nil {
		return errToStatus(err), err
	}

	splitSrc := strings.Split(src, "::")
	if len(splitSrc) <= 1 {
		return http.StatusBadRequest, fmt.Errorf("invalid source path: %v", src)
	}
	srcIndex := settings.RootPath
	src = splitSrc[1]

	splitDst := strings.Split(dst, "::")
	if len(splitDst) <= 1 {
		return http.StatusBadRequest, fmt.Errorf("invalid destination path: %v", dst)
	}
	dstIndex := settings.RootPath
	dst = splitDst[1]

	if dst == "/" || src == "/" {
		return http.StatusForbidden, fmt.Errorf("forbidden: source or destination is attempting to modify root")
	}

	// Direct filesystem access - check target dir exists
	parentDir := filepath.Join(dstIndex, filepath.Dir(dst))
	_, statErr := os.Stat(parentDir)
	if statErr != nil {
		logger.Debugf("Could not get real path for parent dir: %v %v", filepath.Dir(dst), statErr)
		return http.StatusNotFound, statErr
	}
	realDest := filepath.Join(parentDir, filepath.Base(dst))

	realSrc := filepath.Join(srcIndex, src)
	stat, err := os.Stat(realSrc)
	if err != nil {
		return http.StatusNotFound, err
	}
	isSrcDir := stat.IsDir()

	// Check access control for both source and destination paths
	rename := r.URL.Query().Get("rename") == "true"
	if rename {
		realDest = addVersionSuffix(realDest)
	}

	// Validate move/rename operation to prevent circular references
	if action == "rename" || action == "move" {
		if err = validateMoveOperation(realSrc, realDest, isSrcDir); err != nil {
			return http.StatusBadRequest, err
		}
	}

	err = patchAction(r.Context(), patchActionParams{
		action:   action,
		srcIndex: srcIndex,
		dstIndex: dstIndex,
		src:      realSrc,
		dst:      realDest,
		d:        d,
		isSrcDir: isSrcDir,
	})
	if err != nil {
		logger.Debugf("Could not run patch action. src=%v dst=%v err=%v", realSrc, realDest, err)
	}
	return errToStatus(err), err
}

func addVersionSuffix(source string) string {
	counter := 1
	dir, name := path.Split(source)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for {
		if _, err := os.Stat(source); err != nil {
			break
		}
		renamed := fmt.Sprintf("%s(%d)%s", base, counter, ext)
		source = path.Join(dir, renamed)
		counter++
	}
	return source
}

type patchActionParams struct {
	action   string
	srcIndex string
	dstIndex string
	src      string
	dst      string
	d        *requestContext
	isSrcDir bool
}

func patchAction(ctx context.Context, params patchActionParams) error {
	switch params.action {
	case "copy":
		err := files.CopyResource(params.isSrcDir, params.srcIndex, params.dstIndex, params.src, params.dst)
		return err
	case "rename", "move":
		// Direct filesystem access
		fileInfo, err := files.FileInfoFaster(utils.FileOptions{
			Username: params.d.user.Username,
			Path:     params.src,
			Source:   params.srcIndex,
			IsDir:    params.isSrcDir,
		})
		if err != nil {
			return err
		}

		// delete thumbnails
		preview.DelThumbs(ctx, *fileInfo)
		return files.MoveResource(params.isSrcDir, params.srcIndex, params.dstIndex, params.src, params.dst)
	default:
		return fmt.Errorf("unsupported action %s: %w", params.action, errors.ErrInvalidRequestParams)
	}
}
