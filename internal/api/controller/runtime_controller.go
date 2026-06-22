package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// DefaultWaitingTemplatePath is the default path for the waiting page template.
const DefaultWaitingTemplatePath = "./ui/templates/waiting.html"

type RuntimeController struct {
	runtime              runtime.ContainerRuntime
	containerStore       cache.ContainerStore
	baseCtx              context.Context
	systemStatsCollector runtime.SystemStatsCollector
	config               *config.Config
	waitingTemplate      string
}

// NewRuntimeController creates a new RuntimeController with the waiting template loaded from file.
func NewRuntimeController(appCtx *app.App) *RuntimeController {
	templateContent, err := os.ReadFile(DefaultWaitingTemplatePath)
	if err != nil {
		logger.WithComponent("runtime_controller").Warnf("failed to load waiting template from %s: %v", DefaultWaitingTemplatePath, err)
		templateContent = []byte("<!-- template not found -->")
	} else {
		logger.WithComponent("runtime_controller").Infof("loaded waiting template from %s", DefaultWaitingTemplatePath)
	}

	return &RuntimeController{
		runtime:              appCtx.Runtime,
		containerStore:       appCtx.Cache,
		baseCtx:              appCtx.BaseCtx,
		config:               appCtx.Config,
		waitingTemplate:      string(templateContent),
		systemStatsCollector: runtime.NewSystemStatsCollector(appCtx.Config.Data.SystemMonitorMountPoint),
	}
}

// IsRunning checks if a container is currently running.
func (rc *RuntimeController) IsRunning(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing container name"})
		return
	}

	// Check if container exists in cache
	doc, err := rc.containerStore.Snapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read container list"})
		return
	}

	containerExists := false
	for _, container := range doc.Containers {
		if container.Name == name {
			containerExists = true
			break
		}
	}
	if !containerExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	running, err := rc.runtime.IsRunning(c.Request.Context(), name)
	if err != nil {
		// Check if error is "container not found"
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		logger.WithComponent("runtime_controller").Errorf("failed to check if container %s is running: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to determine container running state"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":    name,
		"running": running,
	})
}

// StartContainer starts a container by name.
func (rc *RuntimeController) StartContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing container name"})
		return
	}

	// Check if container exists in cache
	doc, err := rc.containerStore.Snapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read container list"})
		return
	}

	containerExists := false
	for _, container := range doc.Containers {
		if container.Name == name {
			containerExists = true
			break
		}
	}
	if !containerExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	// Check if container is running, if not start it in background
	running, err := rc.runtime.IsRunning(c.Request.Context(), name)
	if err != nil {
		logger.WithComponent("runtime_controller").Warnf("failed to check if container %s is running: %v", name, err)

		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Container not found"})
			return
		}

		// Assume not running and try to start
		running = false
	}

	if !running {
		rc.startContainerInBackground(name)
	}

	c.JSON(http.StatusOK, gin.H{
		"name":    name,
		"message": "container started",
	})
}

// StopContainer stops a container by name.
func (rc *RuntimeController) StopContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing container name"})
		return
	}

	// Check if container exists in cache
	doc, err := rc.containerStore.Snapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read container list"})
		return
	}

	containerExists := false
	for _, container := range doc.Containers {
		if container.Name == name {
			containerExists = true
			break
		}
	}
	if !containerExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	// Check if container is running, if it is then stop it in background
	running, err := rc.runtime.IsRunning(c.Request.Context(), name)
	if err != nil {
		logger.WithComponent("runtime_controller").Warnf("failed to check if container %s is running: %v", name, err)

		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Container not found"})
			return
		}

		// Assume running and try to stop
		running = true
	}

	if running {
		rc.stopContainerInBackground(name)
	}

	c.JSON(http.StatusOK, gin.H{
		"name":    name,
		"message": "container stopped",
	})
}

// stopContainerInBackground stops a container in a dedicated goroutine.
func (rc *RuntimeController) stopContainerInBackground(containerName string) {
	go func(name string) {
		logger.WithComponent("runtime_controller").Infof("stopping container %s in background", name)
		if err := rc.runtime.Stop(rc.baseCtx, name); err != nil {
			logger.WithComponent("runtime_controller").Errorf("failed to stop container %s in background: %v", name, err)
		} else {
			logger.WithComponent("runtime_controller").Infof("container %s stopped successfully", name)
		}
	}(containerName)
}

// WaitingPage serves a waiting HTML page for a container or group.
// It starts containers in background if they are not running.
// Returns 404 if container/group not found, 403 if not active.
func (rc *RuntimeController) WaitingPage(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing container or group name"})
		return
	}

	doc, err := rc.containerStore.Snapshot()
	if err != nil {
		logger.WithComponent("runtime_controller").Errorf("failed to read container list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read container list"})
		return
	}

	// Try to find as container first
	container, found := rc.findContainer(doc, name)
	if found {
		rc.handleContainerWaitingPage(c, container)
		return
	}

	// Try to find as group
	group, found := rc.findGroup(doc, name)
	if found {
		rc.handleGroupWaitingPage(c, doc, group)
		return
	}

	// Check if container with that name exists in runtime
	found = true
	_, errRuntime := rc.runtime.IsRunning(c.Request.Context(), name)
	if errRuntime != nil {
		found = false
	}

	if found {
		containerFromRuntime := &repository.Container{
			Name:         name,
			FriendlyName: name,
			URL:          strings.ReplaceAll(rc.config.Data.BaseUrl, "$1", name),
			Active:       func(b bool) *bool { return &b }(true),
		}
		rc.handleContainerWaitingPage(c, containerFromRuntime)
		return
	}

	// Not found as container or group
	c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("container or group '%s' not found", name)})
}

// findContainer searches for a container by name in the data document.
func (rc *RuntimeController) findContainer(doc repository.DataDocument, name string) (*repository.Container, bool) {
	for i := range doc.Containers {
		if doc.Containers[i].FriendlyName == name {
			return &doc.Containers[i], true
		}
	}

	for i := range doc.Containers {
		if doc.Containers[i].Name == name {
			return &doc.Containers[i], true
		}
	}
	return nil, false
}

// findGroup searches for a group by name in the data document.
func (rc *RuntimeController) findGroup(doc repository.DataDocument, name string) (*repository.Group, bool) {
	for i := range doc.Groups {
		if doc.Groups[i].Name == name {
			return &doc.Groups[i], true
		}
	}
	return nil, false
}

// handleContainerWaitingPage handles the waiting page for a single container.
func (rc *RuntimeController) handleContainerWaitingPage(c *gin.Context, container *repository.Container) {
	// Check if container is active
	if container.Active == nil || !*container.Active {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("container '%s' is not active", container.Name)})
		return
	}

	// Check if container is running, if not start it in background
	running, err := rc.runtime.IsRunning(c.Request.Context(), container.Name)
	if err != nil {
		logger.WithComponent("runtime_controller").Warnf("failed to check if container %s is running: %v", container.Name, err)
		// Assume not running and try to start
		running = false
	}

	if !running {
		rc.startContainerInBackground(container.Name)
	}

	// Serve the waiting page
	rc.serveWaitingPage(c, container.Name, container.URL)
}

// handleGroupWaitingPage handles the waiting page for a group of containers.
func (rc *RuntimeController) handleGroupWaitingPage(c *gin.Context, doc repository.DataDocument, group *repository.Group) {
	// Check if group is active
	if group.Active == nil || !*group.Active {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("group '%s' is not active", group.Name)})
		return
	}

	// Find the first container in the group to get the redirect URL
	if len(group.Container) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("group '%s' has no containers", group.Name)})
		return
	}

	var firstContainer *repository.Container
	for _, containerName := range group.Container {
		container, found := rc.findContainer(doc, containerName)
		if found {
			firstContainer = container
			break
		}
	}

	if firstContainer == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("no valid containers found in group '%s'", group.Name)})
		return
	}

	// Start all containers in the group that are not running (in background)
	for _, containerName := range group.Container {
		container, found := rc.findContainer(doc, containerName)
		if !found {
			logger.WithComponent("runtime_controller").Warnf("container %s in group %s not found", containerName, group.Name)
			continue
		}

		// Check if container is active before starting
		if container.Active == nil || !*container.Active {
			logger.WithComponent("runtime_controller").Debugf("container %s in group %s is not active, skipping", containerName, group.Name)
			continue
		}

		running, err := rc.runtime.IsRunning(c.Request.Context(), containerName)
		if err != nil {
			logger.WithComponent("runtime_controller").Warnf("failed to check if container %s is running: %v", containerName, err)
			running = false
		}

		if !running {
			rc.startContainerInBackground(containerName)
		}
	}

	// Serve the waiting page with the group name and first container's URL
	rc.serveWaitingPage(c, group.Name, firstContainer.URL)
}

// startContainerInBackground starts a container in a dedicated goroutine.
func (rc *RuntimeController) startContainerInBackground(containerName string) {
	go func(name string) {
		logger.WithComponent("runtime_controller").Infof("starting container %s in background", name)
		if err := rc.runtime.Start(rc.baseCtx, name); err != nil {
			logger.WithComponent("runtime_controller").Errorf("failed to start container %s in background: %v", name, err)
		} else {
			logger.WithComponent("runtime_controller").Infof("container %s started successfully", name)
		}
	}(containerName)
}

// serveWaitingPage renders the waiting HTML template with placeholders replaced.
func (rc *RuntimeController) serveWaitingPage(c *gin.Context, containerName, redirectURL string) {
	html := rc.waitingTemplate
	html = strings.ReplaceAll(html, "{{CONTAINER_NAME}}", containerName)
	html = strings.ReplaceAll(html, "{{REDIRECT_URL}}", redirectURL)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// ListContainers returns a JSON array with the names of containers present in the runtime.
func (rc *RuntimeController) ListContainers(c *gin.Context) {
	names, err := rc.runtime.ListContainers(c.Request.Context())
	if err != nil {
		logger.WithComponent("runtime_controller").Errorf("failed to list containers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to list containers"})
		return
	}
	c.JSON(http.StatusOK, names)
}

// ContainerStatsResponse represents the stats for a single container.
type ContainerStatsResponse struct {
	Name       string  `json:"name"`
	Error      string  `json:"error,omitempty"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
}

// AllStats returns CPU and memory statistics for all containers defined in the store.
// Stats are fetched in parallel to avoid sequential timeout accumulation.
func (rc *RuntimeController) AllStats(c *gin.Context) {
	doc, err := rc.containerStore.Snapshot()
	if err != nil {
		logger.WithComponent("runtime_controller").Errorf("failed to read container list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read container list"})
		return
	}

	// Fetch stats for all containers in parallel
	type statsResult struct {
		resp  ContainerStatsResponse
		index int
	}

	resultChan := make(chan statsResult, len(doc.Containers))
	ctx := c.Request.Context()

	// Log context deadline for debugging
	if deadline, ok := ctx.Deadline(); ok {
		logger.WithComponent("runtime_controller").Debugf("AllStats context deadline: %v (in %v)", deadline, time.Until(deadline))
	} else {
		logger.WithComponent("runtime_controller").Debugf("AllStats context has no deadline")
	}

	for i, container := range doc.Containers {
		go func(idx int, name string) {
			stats, err := rc.runtime.Stats(ctx, name)
			if err != nil {
				logger.WithComponent("runtime_controller").Warnf("failed to get stats for container %s: %v", name, err)
				resultChan <- statsResult{
					index: idx,
					resp: ContainerStatsResponse{
						Name:  name,
						Error: err.Error(),
					},
				}
				return
			}
			resultChan <- statsResult{
				index: idx,
				resp: ContainerStatsResponse{
					Name:       name,
					CPUPercent: stats.CPUPercent,
					MemoryMB:   stats.MemoryMB,
				},
			}
		}(i, container.Name)
	}

	// Collect all results
	results := make([]ContainerStatsResponse, len(doc.Containers))
	for range doc.Containers {
		res := <-resultChan
		results[res.index] = res.resp
	}

	c.JSON(http.StatusOK, results)
}

// SystemStats returns system-wide resource usage (CPU, RAM, disk).
func (rc *RuntimeController) SystemStats(c *gin.Context) {
	collector := runtime.NewSystemStatsCollector(rc.config.Data.SystemMonitorMountPoint)
	stats, err := collector.GetStats(c.Request.Context())

	if err != nil {
		logger.WithComponent("runtime_controller").Errorf("failed to get system stats: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get system stats: %v", err)})
		return
	}
	c.JSON(http.StatusOK, stats)
}
