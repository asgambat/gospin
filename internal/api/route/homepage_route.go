package route

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/config"
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
	// that have no matching definition in actions.yml.
	if hpCfg, _, err := config.LoadHomepageConfig(defaultHomepageConfigPath); err == nil {
		var refs []string
		for _, group := range hpCfg.Actions {
			for _, item := range group.Items {
				refs = append(refs, item.Action)
			}
		}
		ac.LogDanglingReferences(refs)
	}
}
