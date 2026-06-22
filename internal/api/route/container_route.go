package route

import (
	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

func NewContainerRouter(appCtx *app.App, group *gin.RouterGroup) {
	cc := controller.NewContainerController(appCtx.BaseCtx, appCtx.Cache, appCtx.Runtime, appCtx.Config.Misc.CosmosBaseUrl, appCtx.Config.Misc.CosmosToken)

	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)

	group.GET("containers", timeoutMiddleware, cc.AllContainers)
	group.POST("containers/import", timeoutMiddleware, cc.ImportContainers)
	group.POST("container", timeoutMiddleware, cc.CreateOrUpdateContainer)
	group.DELETE("container/:name", timeoutMiddleware, cc.DeleteContainer)
	group.GET("container/:name/ready", timeoutMiddleware, cc.Ready)
}
