package route

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// NewUIRouter sets up routes to serve the UI static files under /ui.
// It serves index.html for the root and any sub-paths (SPA routing).
func NewUIRouter(r *gin.Engine) {
	// Serve static assets (JS, CSS, images) with long-term caching
	assetsGroup := r.Group("/ui/assets")
	assetsGroup.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000")
		c.Next()
	})
	assetsGroup.Static("/", "./ui/assets")

	// Serve favicon
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Content-Type", "image/x-icon")
		c.Header("Cache-Control", "public, max-age=86400")
		c.File("./ui/assets/vite.ico")
	})

	// Redirect root to /ui
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui")
	})

	// Serve index.html for the /ui root (no caching for HTML)
	r.GET("/ui", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.File("./ui/index.html")
	})

	// Serve home.html for the dedicated homepage route (no caching for HTML)
	r.GET("/ui/home", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.File("./ui/home.html")
	})
	r.GET("/ui/home.js", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000")
		c.File("./ui/home.js")
	})

	// Serve index.html for any sub-path under /ui (SPA client-side routing)
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path

		// Only handle /ui/* paths, return 404 for others
		if p == "/ui" || strings.HasPrefix(p, "/ui/") {
			c.File("./ui/index.html")
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})
}
