package route

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/logger"
)

// homeJSCacheBuster is a short content hash of ui/home.js, computed once at
// process startup and appended as a ?v= query parameter to the <script> tag
// in /ui/home. It forces browsers (and CDNs) to re-fetch the JS after every
// deployment without waiting for the service worker cache to expire.
var homeJSCacheBuster string

// appJSCacheBuster is the same cache-busting hash for ui/assets/app.js,
// injected into /ui (index.html) the same way as homeJSCacheBuster.
var appJSCacheBuster string

// fileHash returns the first 8 hex characters of the SHA-256 hash of the
// file at path, or "dev" if the file cannot be read.
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.WithComponent("ui_router").Warnf("cannot read %s for cache-busting hash: %v", path, err)
		return "dev"
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:4])
}

func init() {
	homeJSCacheBuster = fileHash("./ui/home.js")
	appJSCacheBuster = fileHash("./ui/assets/app.js")
}

// NewUIRouter sets up routes to serve the UI static files under /ui.
// It serves index.html for the root and any sub-paths (SPA routing).
func NewUIRouter(r *gin.Engine) {
	// Serve static assets (JS, CSS, images) with long-term caching.
	// Manifests are excluded from long-term caching so icon/scope changes
	// are picked up immediately by browsers.
	assetsGroup := r.Group("/ui/assets")
	assetsGroup.Use(func(c *gin.Context) {
		if strings.Contains(c.Request.URL.Path, "manifest") {
			c.Header("Content-Type", "application/manifest+json")
			c.Header("Cache-Control", "no-cache, must-revalidate")
		} else {
			c.Header("Cache-Control", "public, max-age=31536000")
		}
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

	// Serve service worker from /ui/ so its default scope covers all /ui/* pages.
	// Service-Worker-Allowed header allows explicit scope override if needed.
	// Browsers cache SW for at most 24h per spec; we use 1h for rapid iteration.
	// MUST be registered before /ui route to avoid Gin routing conflicts.
	r.GET("/ui/service-worker.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.Header("Service-Worker-Allowed", "/")
		c.Header("Cache-Control", "public, max-age=3600")
		c.File("./ui/service-worker.js")
	})

	// Serve index.html for the /ui root (no caching for HTML).
	// The /ui/assets/app.js <script> tag is rewritten on the fly to include a
	// ?v=<hash> cache-busting query parameter so that deployments are picked
	// up immediately even when the service worker hasn't refreshed yet.
	r.GET("/ui", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")

		html, err := os.ReadFile("./ui/index.html")
		if err != nil {
			c.File("./ui/index.html")
			return
		}

		// Rewrite src="/ui/assets/app.js" → src="/ui/assets/app.js?v=<hash>"
		rewritten := strings.Replace(
			string(html),
			`src="/ui/assets/app.js"`,
			fmt.Sprintf(`src="/ui/assets/app.js?v=%s"`, appJSCacheBuster),
			1,
		)

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	})

	// Serve home.html for the dedicated homepage route (no caching for HTML).
	// The /ui/home.js <script> tag is rewritten on the fly to include a
	// ?v=<hash> cache-busting query parameter so that deployments are picked
	// up immediately even when the service worker hasn't refreshed yet.
	r.GET("/ui/home", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")

		html, err := os.ReadFile("./ui/home.html")
		if err != nil {
			c.File("./ui/home.html")
			return
		}

		// Rewrite src="/ui/home.js" → src="/ui/home.js?v=<hash>"
		rewritten := strings.Replace(
			string(html),
			`src="/ui/home.js"`,
			fmt.Sprintf(`src="/ui/home.js?v=%s"`, homeJSCacheBuster),
			1,
		)

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	})
	r.GET("/ui/home.js", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
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
