package route

import (
	"net/http"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

func SetupRoutes(appCtx *app.App, logger *logrus.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.HoneybadgerMiddleware(logger))
	r.Use(gin.Recovery())
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	r.Use(middleware.HoneybadgerMiddleware(logger))
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

	// UI static files
	NewUIRouter(r)

	return r
}
