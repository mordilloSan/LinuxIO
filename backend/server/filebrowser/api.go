package filebrowser

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type requestContext struct {
	user session.User
}

// RegisterRoutes wires the Filebrowser HTTP handlers into the provided router group.
// The caller should wrap the group with session middleware before invoking this.
func RegisterRoutes(r *gin.RouterGroup) error {
	r.GET("/api/resources", resourceGetHandler)
	r.GET("/api/resources/stat", resourceStatHandler)
	r.DELETE("/api/resources", resourceDeleteHandler)
	r.POST("/api/resources", resourcePostHandler)
	r.PUT("/api/resources", resourcePutHandler)
	r.PATCH("/api/resources", resourcePatchHandler)

	r.GET("/api/raw", rawHandler)

	return nil
}

func newRequestContext(c *gin.Context) (*requestContext, error) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	return &requestContext{
		user: sess.User,
	}, nil
}
