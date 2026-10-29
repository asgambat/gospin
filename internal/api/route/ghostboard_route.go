package route

import (
	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/api/controller"
	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/logger"
)

// NewGhostboardRouter registers the shared text board API. The event stream
// route deliberately has no request timeout: it stays open for the lifetime of
// the page and is cancelled by the client closing the connection.
//
// The boards named in ghostboard.private_boards go through the PIN gate. The
// unlock route is registered without it, since it is how a session is obtained.
func NewGhostboardRouter(appCtx *app.App, group *gin.RouterGroup) {
	if appCtx.Ghostboard == nil {
		logger.WithComponent("ghostboard").Error("ghostboard store is not initialized, skipping routes")
		return
	}

	cfg := appCtx.Config.Ghostboard.WithDefaults()
	gc := controller.NewGhostboardController(appCtx.Ghostboard, appCtx.GhostboardAuth, cfg.MaxTextBytes, cfg.HeartbeatInterval)

	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)
	pinGate := middleware.GhostboardPIN(appCtx.GhostboardAuth)

	group.GET("ghostboard/:board", timeoutMiddleware, pinGate, gc.Get)
	group.POST("ghostboard/:board", timeoutMiddleware, pinGate, gc.Update)
	group.GET("ghostboard/:board/events", pinGate, gc.Events)

	// Plain-text board API for CLIs and scripts: GET returns the board text
	// as-is, POST replaces it with the raw request body and answers 204.
	group.GET("api/boards/:board", timeoutMiddleware, pinGate, gc.GetText)
	group.POST("api/boards/:board", timeoutMiddleware, pinGate, gc.SetText)

	// Session handling of a protected board: unlock trades the PIN for a signed
	// token, lock expires the cookie of the browser.
	group.POST("ghostboard/:board/unlock", timeoutMiddleware, gc.Unlock)
	group.DELETE("ghostboard/:board/unlock", timeoutMiddleware, gc.Lock)
}
