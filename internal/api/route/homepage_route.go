package route

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/api/controller"
	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/logger"
)

const defaultHomepageConfigPath = "./config/homepage.yaml"

func NewHomepageRouter(appCtx *app.App, group *gin.RouterGroup) {
	hc := controller.NewHomepageController(defaultHomepageConfigPath)
	ac := controller.NewActionController(
		controller.DefaultActionsConfigPath,
		appCtx.Config.Data.ActionsMode,
		time.Duration(appCtx.Config.Data.ActionsTimeoutSeconds)*time.Second,
	)

	defaultTimeout := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)
	group.GET("homepage", defaultTimeout, hc.GetHomepageData)

	// The action route is bounded by actions_timeout_seconds rather than the
	// generic server request timeout, since proxied actions can legitimately
	// take longer than the default 1s and the controller honors this context.
	actionTimeout := middleware.RequestTimeout(ac.Timeout())
	group.POST("homepage/action", actionTimeout, ac.ExecuteAction)

	// Non-fatal startup check: warn about actions referenced in homepage.yaml
	// that have no matching definition in actions.yml. A broken file is
	// reported here instead of being ignored: the /homepage handler would only
	// surface it on the first request.
	hpCfg, _, err := config.LoadHomepageConfig(defaultHomepageConfigPath)
	if err != nil {
		logger.WithComponent("homepage_router").Warnf("skipping dangling-reference check, cannot load %s: %v", defaultHomepageConfigPath, err)
		return
	}

	var refs []string
	for _, actionGroup := range hpCfg.Actions {
		for _, item := range actionGroup.Items {
			refs = append(refs, item.Action)
		}
	}
	ac.LogDanglingReferences(refs)
}
