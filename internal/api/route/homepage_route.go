package route

import (
	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
	"github.com/gin-gonic/gin"
)

const defaultHomepageConfigPath = "./config/homepage.yaml"

func NewHomepageRouter(appCtx *app.App, group *gin.RouterGroup) {
	hc := controller.NewHomepageController(defaultHomepageConfigPath)

	defaultTimeout := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)
	group.GET("homepage", defaultTimeout, hc.GetHomepageData)
}
