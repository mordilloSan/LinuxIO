package filebrowser

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

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

// newSessionUser extracts the user session from the Gin context and wraps it
func newSessionUser(c *gin.Context) (*sessionUser, error) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	return &sessionUser{user: sess.User}, nil
}

// sessionUser wraps the session.User to implement User interface
type sessionUser struct {
	user session.User
}

// GetUsername returns the username from the session user
func (su *sessionUser) GetUsername() string {
	return su.user.Username
}
