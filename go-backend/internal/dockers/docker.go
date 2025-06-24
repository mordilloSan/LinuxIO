package dockers

import (
	"go-backend/internal/auth"
	"go-backend/internal/bridge"
	"go-backend/internal/logger"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ListContainers(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	data, err := bridge.CallWithSession(sess, "docker", "list_containers", nil)
	if err != nil {
		logger.Errorf("Bridge ListContainers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func StartContainer(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "start_container", []string{id})
	if err != nil {
		logger.Errorf("Bridge StartContainer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func StopContainer(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "stop_container", []string{id})
	if err != nil {
		logger.Errorf("Bridge StopContainer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func RemoveContainer(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "remove_container", []string{id})
	if err != nil {
		logger.Errorf("Bridge RemoveContainer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func RestartContainer(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	id := c.Param("id")
	data, err := bridge.CallWithSession(sess, "docker", "restart_container", []string{id})
	if err != nil {
		logger.Errorf("Bridge RestartContainer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListImages(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	data, err := bridge.CallWithSession(sess, "docker", "list_images", nil)
	if err != nil {
		logger.Errorf("Bridge ListImages: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListDockerNetworks(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	data, err := bridge.CallWithSession(sess, "docker", "list_networks", nil)
	if err != nil {
		logger.Errorf("Bridge ListNetworks: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// CreateDockerNetwork handles the creation of a new Docker network.
func CreateDockerNetwork(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	data, err := bridge.CallWithSession(sess, "docker", "create_network", []string{req.Name})
	if err != nil {
		logger.Errorf("Bridge CreateDockerNetwork: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerNetwork(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "docker", "delete_network", []string{name})
	if err != nil {
		logger.Errorf("Bridge DeleteDockerNetwork: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func ListDockerVolumes(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	data, err := bridge.CallWithSession(sess, "docker", "list_volumes", nil)
	if err != nil {
		logger.Errorf("Bridge ListVolumes: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// DeleteDockerVolume handles the deletion of a Docker volume.
func DeleteDockerVolume(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "docker", "delete_volume", []string{name})
	if err != nil {
		logger.Errorf("Bridge DeleteDockerVolume: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// CreateDockerVolume handles the creation of a new Docker volume.
func CreateDockerVolume(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	data, err := bridge.CallWithSession(sess, "docker", "create_volume", []string{req.Name})
	if err != nil {
		logger.Errorf("Bridge CreateDockerVolume: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func RegisterDockerRoutes(router *gin.Engine) {
	docker := router.Group("/docker", auth.AuthMiddleware())
	{
		docker.GET("/containers", ListContainers)
		docker.POST("/containers/:id/start", StartContainer)
		docker.POST("/containers/:id/stop", StopContainer)
		docker.POST("/containers/:id/restart", RestartContainer)
		docker.POST("/containers/:id/remove", RemoveContainer)
		docker.GET("/images", ListImages)
		docker.GET("/networks", ListDockerNetworks)
		docker.POST("/networks", CreateDockerNetwork)
		docker.DELETE("/networks/:name", DeleteDockerNetwork)
		docker.GET("/volumes", ListDockerVolumes)
		docker.POST("/volumes", CreateDockerVolume)
		docker.DELETE("/volumes/:name", DeleteDockerVolume)
	}
}
