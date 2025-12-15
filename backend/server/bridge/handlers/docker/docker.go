package docker

import (
	"encoding/json"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"

	"github.com/gin-gonic/gin"
)

func ListContainers(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "list_containers", nil, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func StartContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "start_container", []string{id}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func StopContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "stop_container", []string{id}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func RemoveContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "remove_container", []string{id}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func RestartContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "restart_container", []string{id}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func ListImages(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "list_images", nil, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func ListDockerNetworks(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "list_networks", nil, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateDockerNetwork handles the creation of a new Docker network.
func CreateDockerNetwork(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "create_network", []string{req.Name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerNetwork(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "delete_network", []string{name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func ListDockerVolumes(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "list_volumes", nil, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerVolume(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "delete_volume", []string{name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateDockerVolume handles the creation of a new Docker volume.
func CreateDockerVolume(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "create_volume", []string{req.Name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func LogContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	args := []string{id}
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "docker", "get_container_logs", args, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
