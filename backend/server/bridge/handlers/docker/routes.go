package docker

import "github.com/gin-gonic/gin"

func RegisterDockerRoutes(docker *gin.RouterGroup) {
	{
		docker.GET("/containers", ListContainers)
		docker.POST("/containers/:id/start", StartContainer)
		docker.POST("/containers/:id/stop", StopContainer)
		docker.POST("/containers/:id/restart", RestartContainer)
		docker.POST("/containers/:id/remove", RemoveContainer)
		docker.GET("/containers/:id/logs", LogContainer)
		docker.GET("/images", ListImages)
		docker.GET("/networks", ListDockerNetworks)
		docker.POST("/networks", CreateDockerNetwork)
		docker.DELETE("/networks/:name", DeleteDockerNetwork)
		docker.GET("/volumes", ListDockerVolumes)
		docker.POST("/volumes", CreateDockerVolume)
		docker.DELETE("/volumes/:name", DeleteDockerVolume)
	}
}
