package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
)

// ScheduleController handles schedule-related HTTP endpoints using the generic CRUD controller.
type ScheduleController struct {
	crud *CrudController[repository.Schedule]
}

// NewScheduleController creates a new ScheduleController with the given cache store.
func NewScheduleController(store cache.ScheduleStore) *ScheduleController {
	v := validator.New()
	service := &ScheduleCrudService{Store: store}
	validator := &ScheduleCrudValidator{validator: v}

	return &ScheduleController{
		crud: &CrudController[repository.Schedule]{
			Service:   service,
			Validator: validator,
		},
	}
}

// AllSchedules handles GET /schedules - returns all schedules.
func (sc *ScheduleController) AllSchedules(c *gin.Context) {
	logger.WithComponent("schedule-controller").Debugf("GET /schedules handler called")
	sc.crud.GetAll(c)
}

// CreateOrUpdateSchedule handles POST /schedule - creates or updates a schedule.
func (sc *ScheduleController) CreateOrUpdateSchedule(c *gin.Context) {
	logger.WithComponent("schedule-controller").Debugf("POST /schedule handler called")
	sc.crud.CreateOrUpdate(c)
}

// DeleteSchedule handles DELETE /schedule/:id - deletes a schedule by ID.
func (sc *ScheduleController) DeleteSchedule(c *gin.Context) {
	id := c.Param("id")
	logger.WithComponent("schedule-controller").Debugf("DELETE /schedule/%s handler called", id)
	if id == "" {
		logger.WithComponent("schedule-controller").Debugf("delete schedule: missing id parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing schedule id"})
		return
	}

	items, err := sc.crud.Service.Remove(id)
	if err != nil {
		if errors.Is(err, cache.ErrScheduleNotFound) {
			logger.WithComponent("schedule-controller").Debugf("delete schedule %s: not found", id)
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		logger.WithComponent("schedule-controller").Errorf("delete schedule %s: cache error: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cache"})
		return
	}

	logger.WithComponent("schedule-controller").Debugf("schedule %s deleted successfully", id)
	c.JSON(http.StatusOK, items)
}
