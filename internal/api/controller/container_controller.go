package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/cosmos"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type ContainerController struct {
	crud          *CrudController[repository.Container]
	importService *cosmos.ImportService
	cosmosEnabled bool
	cosmosBaseUrl string
	cosmosToken   string
}

func NewContainerController(ctx context.Context, store cache.ContainerStore, runtime runtime.ContainerRuntime, cosmosBaseUrl, cosmosToken string) *ContainerController {
	v := validator.New()
	service := &ContainerCrudService{Store: store, Runtime: runtime, Ctx: ctx}
	validator := &ContainerCrudValidator{validator: v}

	cosmosEnabled := cosmosBaseUrl != "" && cosmosToken != ""
	var importService *cosmos.ImportService
	if cosmosEnabled {
		cosmosClient := cosmos.NewClient()
		importService = cosmos.NewImportService(cosmosClient, store)
	}

	return &ContainerController{
		crud: &CrudController[repository.Container]{
			Service:   service,
			Validator: validator,
		},
		importService: importService,
		cosmosEnabled: cosmosEnabled,
		cosmosBaseUrl: cosmosBaseUrl,
		cosmosToken:   cosmosToken,
	}
}

// AllContainers handles GET /containers - returns all containers.
func (cc *ContainerController) AllContainers(c *gin.Context) {
	logger.WithComponent("container-controller").Debugf("GET /containers handler called")
	cc.crud.GetAll(c)
}

// CreateOrUpdateContainer handles POST /container - creates or updates a container.
func (cc *ContainerController) CreateOrUpdateContainer(c *gin.Context) {
	logger.WithComponent("container-controller").Debugf("POST /container handler called")
	cc.crud.CreateOrUpdate(c)
}

// DeleteContainer handles DELETE /container/:name - deletes a container by name.
func (cc *ContainerController) DeleteContainer(c *gin.Context) {
	name := c.Param("name")
	logger.WithComponent("container-controller").Debugf("DELETE /container/%s handler called", name)
	if name == "" {
		logger.WithComponent("container-controller").Debugf("delete container: missing name parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing container name"})
		return
	}

	items, err := cc.crud.Service.Remove(name)
	if err != nil {
		if errors.Is(err, cache.ErrContainerNotFound) {
			logger.WithComponent("container-controller").Debugf("delete container %s: not found", name)
			c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
			return
		}
		logger.WithComponent("container-controller").Errorf("delete container %s: cache error: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cache"})
		return
	}

	logger.WithComponent("container-controller").Debugf("container %s deleted successfully", name)
	c.JSON(http.StatusOK, items)
}

// Ready checks whether the container identified by name is reachable and responding 200.
// Route: GET /container/:name/ready
func (cc *ContainerController) Ready(c *gin.Context) {
	name := c.Param("name")
	logger.WithComponent("container-controller").Debugf("GET /container/%s/ready handler called", name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ready": false})
		return
	}

	// Assert underlying service type to access Store and Runtime
	svc, ok := cc.crud.Service.(*ContainerCrudService)
	if !ok {
		logger.WithComponent("container-controller").Errorf("ready: unexpected service type")
		c.JSON(http.StatusInternalServerError, gin.H{"ready": false})
		return
	}

	// Find container in store snapshot
	doc, err := svc.Store.Snapshot()
	if err != nil {
		logger.WithComponent("container-controller").Errorf("ready: failed to snapshot store: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"ready": false})
		return
	}

	var container *repository.Container
	for i := range doc.Containers {
		if doc.Containers[i].Name == name {
			container = &doc.Containers[i]
			break
		}
	}
	if container == nil {
		logger.WithComponent("container-controller").Warnf("ready: container not found: %s", name)
		c.JSON(http.StatusNotFound, gin.H{"ready": false})
		return
	}

	// Check runtime
	running, err := svc.Runtime.IsRunning(svc.Ctx, container.Name)
	if err != nil {
		logger.WithComponent("container-controller").Warnf("ready: runtime check failed for %s: %v", container.Name, err)
		c.JSON(http.StatusOK, gin.H{"ready": false})
		return
	}
	if !running {
		c.JSON(http.StatusOK, gin.H{"ready": false})
		return
	}

	if container.URL == "" {
		logger.WithComponent("container-controller").Warnf("ready: container URL is empty: %s", name)
		c.JSON(http.StatusInternalServerError, gin.H{"ready": false})
		return
	}

	containerURL := container.URL
	if !strings.HasPrefix(containerURL, "http://") && !strings.HasPrefix(containerURL, "https://") {
		containerURL = "https://" + containerURL
	}
	if !strings.HasSuffix(containerURL, "/") {
		containerURL = containerURL + "/"
	}

	// Perform GET with timeout
	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, containerURL, nil)
	if err != nil {
		logger.WithComponent("container-controller").Warnf("ready: failed to create request for %s and url %s: %v", container.Name, containerURL, err)
		c.JSON(http.StatusOK, gin.H{"ready": false})
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithComponent("container-controller").Warnf("ready: request failed for %s and url %s: %v", container.Name, containerURL, err)
		c.JSON(http.StatusOK, gin.H{"ready": false})
		return
	} else {
		logger.WithComponent("container-controller").Debugf("ready: request succeeded for %s and url %s with status %d", container.Name, containerURL, resp.StatusCode)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	isContainerUrlReady := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPermanentRedirect || resp.StatusCode == http.StatusTemporaryRedirect
	logger.WithComponent("container-controller").Debugf("GET /container/%s/ready handled with status: %v", name, isContainerUrlReady)
	c.JSON(http.StatusOK, gin.H{"ready": isContainerUrlReady})
}

func (cc *ContainerController) ImportContainers(c *gin.Context) {
	logger.WithComponent("container-controller").Debugf("POST /containers/import handler called")

	if !cc.cosmosEnabled {
		logger.WithComponent("container-controller").Warnf("import containers: cosmos integration not configured")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Cosmos integration not configured"})
		return
	}

	result, err := cc.importService.ImportContainers(c.Request.Context(), cc.cosmosBaseUrl, cc.cosmosToken)
	if err != nil {
		logger.WithComponent("container-controller").Errorf("import containers failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	logger.WithComponent("container-controller").Infof("import completed: imported=%d, skipped=%d", result.Imported, result.SkippedExisting)
	c.JSON(http.StatusOK, result)
}
