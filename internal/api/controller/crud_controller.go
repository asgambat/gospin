package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/cache"
)

// CrudService defines the minimal interface required for CRUD operations.
type CrudService[T any] interface {
	All() ([]T, error)
	Add(item T) ([]T, error)
	Remove(name string) ([]T, error)
}

// CrudValidator defines the interface for validating a resource.
type CrudValidator[T any] interface {
	Validate(item T) error
}

// CrudController provides generic CRUD handlers for resources.
type CrudController[T any] struct {
	Service   CrudService[T]
	Validator CrudValidator[T]
}

// RegisterCrudRoutes registers CRUD endpoints for a resource on the given router group.
func (cc *CrudController[T]) RegisterCrudRoutes(rg *gin.RouterGroup, resource string) {
	rg.GET("/"+resource+"s", cc.GetAll)
	rg.POST("/"+resource, cc.CreateOrUpdate)
	rg.DELETE("/"+resource+"/:name", cc.Delete)
}

// GetAll handles GET requests to list all resources.
func (cc *CrudController[T]) GetAll(c *gin.Context) {
	items, err := cc.Service.All()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read resource list"})
		return
	}
	c.JSON(http.StatusOK, items)
}

// CreateOrUpdate handles POST requests to create or update a resource.
func (cc *CrudController[T]) CreateOrUpdate(c *gin.Context) {
	var item T
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if cc.Validator != nil {
		if err := cc.Validator.Validate(item); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	items, err := cc.Service.Add(item)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update resource"})
		return
	}
	c.JSON(http.StatusOK, items)
}

// Delete handles DELETE requests to remove a resource by name.
func (cc *CrudController[T]) Delete(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing resource name"})
		return
	}
	items, err := cc.Service.Remove(name)
	if err != nil {
		// Check for specific "not found" errors
		if errors.Is(err, cache.ErrContainerNotFound) ||
			errors.Is(err, cache.ErrGroupNotFound) ||
			errors.Is(err, cache.ErrScheduleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete resource"})
		return
	}
	c.JSON(http.StatusOK, items)
}
