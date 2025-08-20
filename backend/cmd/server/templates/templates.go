package templates

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/mordilloSan/LinuxIO/internal/logger"

	"github.com/gin-gonic/gin"
)

//go:embed index.tmpl
var tmplFS embed.FS

var IndexTemplate *template.Template

func init() {
	var err error
	IndexTemplate, err = template.ParseFS(tmplFS, "index.tmpl")
	if err != nil {
		log.Fatalf("❌ Failed to parse embedded template: %v", err)
	}
}

// viteDevPort is now provided by main (flag), instead of env.
func ServeIndex(c *gin.Context, env string, viteDevPort int, viteManifest []byte) {
	logger.Debugf("📄 ServeIndex called for: %s", c.Request.URL.Path)

	var js, css string

	if env == "development" {
		js = fmt.Sprintf("http://localhost:%d/src/main.tsx", viteDevPort)
		css = "" // Vite injects CSS in dev mode
	} else {
		var err error
		js, css, err = ParseViteManifestBytes(viteManifest)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load bundle info")
			return
		}
	}

	background := "#ffffff"
	shimmer := "#eeeeee"

	data := map[string]string{
		"JSBundle":          js,
		"CSSBundle":         css,
		"Background":        background,
		"ShimmerBackground": shimmer,
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := IndexTemplate.Execute(c.Writer, data); err != nil {
		logger.Errorf("❌ Failed to execute index template: %v", err)
	}
}
