package route

import (
	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

func NewRuntimeRouter(appCtx *app.App, group *gin.RouterGroup) {
	rc := controller.NewRuntimeController(appCtx)

	// Apply default timeout middleware to most routes
	defaultTimeout := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)
	group.GET("runtime/:name/status", defaultTimeout, rc.IsRunning)
	group.POST("runtime/:name/start", defaultTimeout, rc.StartContainer)
	group.POST("runtime/:name/stop", defaultTimeout, rc.StopContainer)
	group.GET("runtime/containers", defaultTimeout, rc.ListContainers)
	group.GET("start/:name", defaultTimeout, rc.WaitingPage)

	// Stats endpoint needs a longer timeout since it queries all containers
	statsRequestTimeout := appCtx.Config.Server.ReadTimeout
	group.GET("runtime/stats", middleware.RequestTimeout(statsRequestTimeout), rc.AllStats)
	group.GET("runtime/system-stats", middleware.RequestTimeout(statsRequestTimeout), rc.SystemStats)
}
