package templates

import (
	"fmt"
	"go-backend/internal/logger"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Expects the embed.FS and manifest FS to be passed in
func RegisterStaticRoutes(router *gin.Engine, staticFS fs.FS, pwaManifest fs.FS) {
	// Serve /assets/*
	assetsFS, err := fs.Sub(staticFS, "frontend/assets")
	if err != nil {
		panic(fmt.Sprintf("Failed to create assets sub FS: %v", err))
	}
	router.GET("/assets/*filepath", gin.WrapH(http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS)))))

	// Serve /manifest.json
	router.GET("/manifest.json", func(c *gin.Context) {
		data, err := fs.ReadFile(pwaManifest, "frontend/manifest.json")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "application/manifest+json")
		if _, err := c.Writer.Write(data); err != nil {
			logger.Warnf("failed to write response: %v", err)
		}
	})

	// Serve /favicon-*.png
	for i := 1; i <= 6; i++ {
		route := fmt.Sprintf("/favicon-%d.png", i)
		filename := fmt.Sprintf("frontend/favicon-%d.png", i)
		router.GET(route, func(filename string) gin.HandlerFunc {
			return func(c *gin.Context) {
				data, err := fs.ReadFile(pwaManifest, filename)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				c.Header("Content-Type", "image/png")
				if _, err := c.Writer.Write(data); err != nil {
					logger.Warnf("failed to write response: %v", err)
				}
			}
		}(filename))
	}

	// Serve /favicon.ico (browser tab icon)
	router.GET("/favicon.ico", func(c *gin.Context) {
		data, err := fs.ReadFile(pwaManifest, "frontend/favicon-6.png")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "image/png")
		if _, err := c.Writer.Write(data); err != nil {
			logger.Warnf("failed to write response: %v", err)
		}

	})
}
