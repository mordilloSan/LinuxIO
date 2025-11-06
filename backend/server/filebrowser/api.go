package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type requestContext struct {
	ctx  context.Context
	user session.User
}

// RegisterRoutes wires the Filebrowser HTTP handlers into the provided router group.
// The caller should wrap the group with session middleware before invoking this.
func RegisterRoutes(r *gin.RouterGroup) error {
	r.GET("/api/resources", adapt(resourceGetHandler))
	r.GET("/api/resources/stat", adapt(resourceStatHandler))
	r.DELETE("/api/resources", adapt(resourceDeleteHandler))
	r.POST("/api/resources", adapt(resourcePostHandler))
	r.PUT("/api/resources", adapt(resourcePutHandler))
	r.PATCH("/api/resources", adapt(resourcePatchHandler))

	r.GET("/api/raw", adapt(rawHandler))

	return nil
}

func adapt(fn func(http.ResponseWriter, *http.Request, *requestContext) (int, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCtx, err := newRequestContext(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		status, err := fn(c.Writer, c.Request, reqCtx)
		if err != nil {
			if status == 0 {
				status = http.StatusInternalServerError
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				c.AbortWithStatus(status)
				return
			}
			if c.Writer.Written() {
				c.AbortWithStatus(status)
			} else {
				c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
			}
			return
		}

		if status > 0 && !c.Writer.Written() {
			c.Status(status)
		}
	}
}

func newRequestContext(c *gin.Context) (*requestContext, error) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	return &requestContext{
		ctx:  c.Request.Context(),
		user: sess.User,
	}, nil
}

func renderJSON(w http.ResponseWriter, _ *http.Request, data any) (int, error) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, nil
}
