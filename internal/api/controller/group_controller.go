package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// GroupController handles group-related HTTP endpoints using the generic CRUD controller.
type GroupController struct {
	crud    *CrudController[repository.Group]
	store   cache.GroupStore
	runtime runtime.ContainerRuntime
	baseCtx context.Context
}

// NewGroupController creates a new GroupController with the given cache store and runtime.
func NewGroupController(baseCtx context.Context, store cache.GroupStore, rt runtime.ContainerRuntime) *GroupController {
	v := validator.New()
	service := &GroupCrudService{Store: store}
	validator := &GroupCrudValidator{validator: v}

	return &GroupController{
		crud: &CrudController[repository.Group]{
			Service:   service,
			Validator: validator,
		},
		store:   store,
		runtime: rt,
		baseCtx: baseCtx,
	}
}

// AllGroups handles GET /groups - returns all groups.
func (gc *GroupController) AllGroups(c *gin.Context) {
	logger.WithComponent("group-controller").Debugf("GET /groups handler called")
	gc.crud.GetAll(c)
}

// CreateOrUpdateGroup handles POST /group - creates or updates a group.
func (gc *GroupController) CreateOrUpdateGroup(c *gin.Context) {
	logger.WithComponent("group-controller").Debugf("POST /group handler called")
	gc.crud.CreateOrUpdate(c)
}

// DeleteGroup handles DELETE /group/:name - deletes a group by name.
func (gc *GroupController) DeleteGroup(c *gin.Context) {
	name := c.Param("name")
	logger.WithComponent("group-controller").Debugf("DELETE /group/%s handler called", name)
	if name == "" {
		logger.WithComponent("group-controller").Debugf("delete group: missing name parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing group name"})
		return
	}

	items, err := gc.crud.Service.Remove(name)
	if err != nil {
		if errors.Is(err, cache.ErrGroupNotFound) {
			logger.WithComponent("group-controller").Debugf("delete group %s: not found", name)
			c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
			return
		}
		logger.WithComponent("group-controller").Errorf("delete group %s: cache error: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cache"})
		return
	}

	logger.WithComponent("group-controller").Debugf("group %s deleted successfully", name)
	c.JSON(http.StatusOK, items)
}

// StartGroup handles POST /group/:name/start - starts all containers in a group.
func (gc *GroupController) StartGroup(c *gin.Context) {
	name := c.Param("name")
	logger.WithComponent("group-controller").Debugf("POST /group/%s/start handler called", name)
	if name == "" {
		logger.WithComponent("group-controller").Debugf("start group: missing name parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing group name"})
		return
	}

	doc, err := gc.store.Snapshot()
	if err != nil {
		logger.WithComponent("group-controller").Errorf("start group %s: failed to read snapshot: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read group data"})
		return
	}

	// Find the group
	var group *repository.Group
	for i := range doc.Groups {
		if doc.Groups[i].Name == name {
			group = &doc.Groups[i]
			break
		}
	}
	if group == nil {
		logger.WithComponent("group-controller").Debugf("start group %s: not found", name)
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	if group.Active == nil || !*group.Active {
		logger.WithComponent("group-controller").Debugf("start group %s: group is not active", name)
		c.JSON(http.StatusForbidden, gin.H{"error": "group is not active"})
		return
	}

	// Start all containers in the group in background
	for _, containerName := range group.Container {
		gc.startContainerInBackground(containerName)
	}

	logger.WithComponent("group-controller").Infof("group %s: started %d containers in background", name, len(group.Container))
	c.JSON(http.StatusOK, gin.H{
		"name":       name,
		"message":    "group containers starting",
		"containers": group.Container,
	})
}

// StopGroup handles POST /group/:name/stop - stops all containers in a group.
func (gc *GroupController) StopGroup(c *gin.Context) {
	name := c.Param("name")
	logger.WithComponent("group-controller").Debugf("POST /group/%s/stop handler called", name)
	if name == "" {
		logger.WithComponent("group-controller").Debugf("stop group: missing name parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing group name"})
		return
	}

	doc, err := gc.store.Snapshot()
	if err != nil {
		logger.WithComponent("group-controller").Errorf("stop group %s: failed to read snapshot: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read group data"})
		return
	}

	// Find the group
	var group *repository.Group
	for i := range doc.Groups {
		if doc.Groups[i].Name == name {
			group = &doc.Groups[i]
			break
		}
	}
	if group == nil {
		logger.WithComponent("group-controller").Debugf("stop group %s: not found", name)
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Stop all containers in the group in background
	for _, containerName := range group.Container {
		gc.stopContainerInBackground(containerName)
	}

	logger.WithComponent("group-controller").Infof("group %s: stopped %d containers in background", name, len(group.Container))
	c.JSON(http.StatusOK, gin.H{
		"name":       name,
		"message":    "group containers stopping",
		"containers": group.Container,
	})
}

// startContainerInBackground starts a container in a dedicated goroutine.
func (gc *GroupController) startContainerInBackground(containerName string) {
	go func(name string) {
		logger.WithComponent("group-controller").Infof("starting container %s in background", name)
		if err := gc.runtime.Start(gc.baseCtx, name); err != nil {
			logger.WithComponent("group-controller").Errorf("failed to start container %s in background: %v", name, err)
		} else {
			logger.WithComponent("group-controller").Infof("container %s started successfully", name)
		}
	}(containerName)
}

// stopContainerInBackground stops a container in a dedicated goroutine.
func (gc *GroupController) stopContainerInBackground(containerName string) {
	go func(name string) {
		logger.WithComponent("group-controller").Infof("stopping container %s in background", name)
		if err := gc.runtime.Stop(gc.baseCtx, name); err != nil {
			logger.WithComponent("group-controller").Errorf("failed to stop container %s in background: %v", name, err)
		} else {
			logger.WithComponent("group-controller").Infof("container %s stopped successfully", name)
		}
	}(containerName)
}
