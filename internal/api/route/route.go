package route

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

// isGhostboardEventStream reports whether path targets the ghostboard SSE
// endpoint, which must not be compressed.
func isGhostboardEventStream(path string) bool {
	return strings.HasPrefix(path, "/ghostboard/") && strings.HasSuffix(path, "/events")
}

func SetupRoutes(appCtx *app.App, logger *logrus.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.HoneybadgerMiddleware(logger))
	r.Use(gin.Recovery())
	gzipHandler := gzip.Gzip(gzip.DefaultCompression)
	r.Use(func(c *gin.Context) {
		if isGhostboardEventStream(c.Request.URL.Path) {
			// Compression buffers the response and hides the underlying
			// http.ResponseWriter, so the stream handler could not clear the
			// server write timeout for the long-lived connection.
			c.Next()
			return
		}
		gzipHandler(c)
	})
	r.Use(middleware.CORSMiddleware(appCtx.Config.Server.CORSAllowedOrigins))

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

	// UI static files. The Ghostboard page receives the configured board size
	// so it can refuse oversized files before loading them.
	NewUIRouter(r, appCtx.Config.Ghostboard.WithDefaults().MaxTextBytes)

	return r
}
