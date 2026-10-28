package route

import (
	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/api/controller"
	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/app"
)

func NewScheduleRouter(appCtx *app.App, group *gin.RouterGroup) {
	sc := controller.NewScheduleController(appCtx.Cache)
	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)

	group.GET("schedules", timeoutMiddleware, sc.AllSchedules)
	group.POST("schedule", timeoutMiddleware, sc.CreateOrUpdateSchedule)
	group.DELETE("schedule/:id", timeoutMiddleware, sc.DeleteSchedule)
}
