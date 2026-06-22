package route

import (
	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

func NewGroupRouter(appCtx *app.App, group *gin.RouterGroup) {
	gc := controller.NewGroupController(appCtx.BaseCtx, appCtx.Cache, appCtx.Runtime)
	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)

	group.GET("groups", timeoutMiddleware, gc.AllGroups)
	group.POST("group", timeoutMiddleware, gc.CreateOrUpdateGroup)
	group.DELETE("group/:name", timeoutMiddleware, gc.DeleteGroup)
	group.POST("group/:name/start", timeoutMiddleware, gc.StartGroup)
	group.POST("group/:name/stop", timeoutMiddleware, gc.StopGroup)
}
