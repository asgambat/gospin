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

	// Attachments. The upload, the download and the archive carry their own
	// deadline: a file of hundreds of megabytes needs file_transfer_timeout_secs,
	// not the per-request timeout of the JSON routes.
	if cfg.FilesEnabled {
		group.GET("ghostboard/:board/files", timeoutMiddleware, pinGate, gc.ListFiles)
		group.GET("ghostboard/:board/files.zip", pinGate, gc.DownloadFilesArchive)
		group.POST("ghostboard/:board/files", pinGate, gc.UploadFile)
		group.GET("ghostboard/:board/files/:id", pinGate, gc.DownloadFile)
		group.DELETE("ghostboard/:board/files/:id", timeoutMiddleware, pinGate, gc.DeleteFile)
		group.DELETE("ghostboard/:board/files", timeoutMiddleware, pinGate, gc.DeleteFiles)

		// The same operations on the plain API: the list and the created file
		// are tab-separated lines, so a script needs no JSON parser.
		group.GET("api/boards/:board/files", timeoutMiddleware, pinGate, gc.ListFilesPlain)
		group.GET("api/boards/:board/files.zip", pinGate, gc.DownloadFilesArchive)
		group.POST("api/boards/:board/files", pinGate, gc.UploadFilePlain)
		group.GET("api/boards/:board/files/:id", pinGate, gc.DownloadFile)
		group.DELETE("api/boards/:board/files/:id", timeoutMiddleware, pinGate, gc.DeleteFile)
		group.DELETE("api/boards/:board/files", timeoutMiddleware, pinGate, gc.DeleteFiles)
	}

	// Session handling of a protected board: unlock trades the PIN for a signed
	// token, lock expires the cookie of the browser.
	group.POST("ghostboard/:board/unlock", timeoutMiddleware, gc.Unlock)
	group.DELETE("ghostboard/:board/unlock", timeoutMiddleware, gc.Lock)

	// Opening a burn board is a POST: a GET would be consumed by a link preview
	// or a browser prefetch before the reader saw the note. With sealed boards
	// disabled the routes are not registered at all, so they answer 404 like
	// every other unconfigured feature.
	if cfg.SealedEnabled {
		group.POST("ghostboard/:board/open", timeoutMiddleware, pinGate, gc.Open)
		group.POST("api/boards/:board/open", timeoutMiddleware, pinGate, gc.OpenText)
	}
}
