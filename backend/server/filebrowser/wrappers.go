package filebrowser

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/services"
)

// ============================================================================
// WRAPPER FUNCTIONS - Helper functions for HTTP handlers
// ============================================================================

// patchActionParams holds parameters for PATCH operations
type patchActionParams struct {
	action string
	src    string
	dst    string
}

// patchAction routes PATCH actions (copy, rename, move) to appropriate service functions
func patchAction(params patchActionParams) error {
	switch params.action {
	case "copy":
		err := services.CopyFile(params.src, params.dst)
		if err != nil {
			logger.Debugf("error copying resource: %v", err)
		}
		return err
	case "rename", "move":
		err := services.MoveFile(params.src, params.dst)
		if err != nil {
			logger.Debugf("error moving/renaming resource: %v", err)
		}
		return err
	default:
		err := fmt.Errorf("unsupported action: %s", params.action)
		logger.Debugf("unsupported patch action: %v", err)
		return err
	}
}

// statusFromError maps an error to an appropriate HTTP status code
func statusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}

	// Check for OS-level errors
	if os.IsPermission(err) {
		return http.StatusForbidden
	}
	if os.IsNotExist(err) {
		return http.StatusNotFound
	}
	if os.IsExist(err) {
		return http.StatusConflict
	}

	// Default to internal server error for unknown errors
	return http.StatusInternalServerError
}

// setContentDisposition sets the Content-Disposition HTTP header for downloads
func setContentDisposition(c *gin.Context, fileName string) {
	if c.Query("inline") == "true" {
		c.Header("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(fileName))
	} else {
		// As per RFC6266 section 4.3
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}
}
