package docker

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"

	"github.com/gin-gonic/gin"
)

func ListContainers(c *gin.Context) {
	sess := session.SessionFromContext(c)
	data, err := bridge.CallWithSession(sess, "docker", "list_containers", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func StartContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "start_container", []string{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func StopContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "stop_container", []string{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func RemoveContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "remove_container", []string{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func RestartContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "restart_container", []string{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListImages(c *gin.Context) {
	sess := session.SessionFromContext(c)
	data, err := bridge.CallWithSession(sess, "docker", "list_images", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListDockerNetworks(c *gin.Context) {
	sess := session.SessionFromContext(c)
	data, err := bridge.CallWithSession(sess, "docker", "list_networks", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
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

	data, err := bridge.CallWithSession(sess, "docker", "create_network", []string{req.Name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerNetwork(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "docker", "delete_network", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListDockerVolumes(c *gin.Context) {
	sess := session.SessionFromContext(c)
	data, err := bridge.CallWithSession(sess, "docker", "list_volumes", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerVolume(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "docker", "delete_volume", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
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

	data, err := bridge.CallWithSession(sess, "docker", "create_volume", []string{req.Name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func LogContainer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	id := c.Param("id")
	args := []string{id}
	data, err := bridge.CallWithSession(sess, "docker", "get_container_logs", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// If data is plain text, send as text/plain. If you wrap in JSON, set to application/json.
	c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
}
