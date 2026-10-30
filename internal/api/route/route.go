package route

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// isGhostboardEventStream reports whether path targets the ghostboard SSE
// endpoint, which must not be compressed.
func isGhostboardEventStream(path string) bool {
	return strings.HasPrefix(path, "/ghostboard/") && strings.HasSuffix(path, "/events")
}

// isGhostboardFileRoute reports whether path targets the attachments of a
// board. They are not compressed: an upload or a download is an opaque stream
// whose bytes are usually already compressed, and gzip would buffer it, drop
// the Content-Length and break the Range support of the download.
func isGhostboardFileRoute(path string) bool {
	if strings.HasPrefix(path, "/ghostboard/") || strings.HasPrefix(path, "/api/boards/") {
		return strings.Contains(path, "/files")
	}
	return false
}

func SetupRoutes(appCtx *app.App, logger *logrus.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.HoneybadgerMiddleware(logger))
	r.Use(gin.Recovery())
	gzipHandler := gzip.Gzip(gzip.DefaultCompression)
	r.Use(func(c *gin.Context) {
		if isGhostboardEventStream(c.Request.URL.Path) || isGhostboardFileRoute(c.Request.URL.Path) {
			// Compression buffers the response and hides the underlying
			// http.ResponseWriter, so the stream handler could not clear the
			// server write timeout for the long-lived connection, and a file
			// would lose its Content-Length and Range support.
			c.Next()
			return
		}
		gzipHandler(c)
	})
	r.Use(middleware.CORSMiddleware(appCtx.Config.Server.CORSAllowedOrigins))
	r.Use(middleware.SecurityHeaders(middleware.SecurityHeaderOptions{
		Enabled:               appCtx.Config.Server.SecurityHeaders.Enabled,
		ContentSecurityPolicy: appCtx.Config.Server.SecurityHeaders.CSP,
		HSTSMaxAge:            appCtx.Config.Server.SecurityHeaders.HSTSMaxAge,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "UP",
		})
	})

	// All Public APIs
	publicRouter := r.Group("")

	NewContainerRouter(appCtx, publicRouter)
	NewGroupRouter(appCtx, publicRouter)
	NewScheduleRouter(appCtx, publicRouter)
	NewRuntimeRouter(appCtx, publicRouter)
	NewConfigurationRouter(appCtx, publicRouter)
	NewHomepageRouter(appCtx, publicRouter)
	NewGhostboardRouter(appCtx, publicRouter)

	// UI static files. The Ghostboard page receives the configured size limits
	// so it can refuse oversized files before loading them.
	ghostboardCfg := appCtx.Config.Ghostboard.WithDefaults()
	maxFileBytes := int64(0)
	if ghostboardCfg.FilesEnabled {
		maxFileBytes = ghostboardCfg.MaxFileBytes
	}
	NewUIRouter(r, GhostboardPageConfig{
		MaxTextBytes:            ghostboardCfg.MaxTextBytes,
		MaxFileBytes:            maxFileBytes,
		SealedEnabled:           ghostboardCfg.SealedEnabled,
		SealedMaxPlaintextBytes: sealedcrypto.MaxPlaintextBytes(ghostboardCfg.MaxTextBytes),
	})

	return r
}
