package route

import (
	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/logger"
)

// NewGhostboardRouter registers the shared text board API. The event stream
// route deliberately has no request timeout: it stays open for the lifetime of
// the page and is cancelled by the client closing the connection.
func NewGhostboardRouter(appCtx *app.App, group *gin.RouterGroup) {
	if appCtx.Ghostboard == nil {
		logger.WithComponent("ghostboard").Error("ghostboard store is not initialized, skipping routes")
		return
	}

	cfg := appCtx.Config.Ghostboard.WithDefaults()
	gc := controller.NewGhostboardController(appCtx.Ghostboard, cfg.MaxTextBytes, cfg.HeartbeatInterval)

	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)

	group.GET("ghostboard/:board", timeoutMiddleware, gc.Get)
	group.POST("ghostboard/:board", timeoutMiddleware, gc.Update)
	group.GET("ghostboard/:board/events", gc.Events)
}
