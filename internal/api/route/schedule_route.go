package route

import (
	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	"github.com/bassista/go_spin/internal/app"
)

func NewScheduleRouter(appCtx *app.App, group *gin.RouterGroup) {
	sc := controller.NewScheduleController(appCtx.Cache)
	timeoutMiddleware := middleware.RequestTimeout(appCtx.Config.Server.RequestTimeout)

	group.GET("schedules", timeoutMiddleware, sc.AllSchedules)
	group.POST("schedule", timeoutMiddleware, sc.CreateOrUpdateSchedule)
	group.DELETE("schedule/:id", timeoutMiddleware, sc.DeleteSchedule)
}
