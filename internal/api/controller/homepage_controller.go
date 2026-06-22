package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/logger"
)

// HomepageController handles homepage-related API endpoints.
type HomepageController struct {
	configPath string
}

// NewHomepageController creates a new HomepageController that loads config from the given path.
func NewHomepageController(configPath string) *HomepageController {
	return &HomepageController{configPath: configPath}
}

// HomepageResponse wraps the homepage config with a content hash for change detection.
type HomepageResponse struct {
	Hash string `json:"hash"`
	config.HomepageConfig
}

// GetHomepageData reloads the homepage config from file and returns it as JSON with a content hash.
func (hc *HomepageController) GetHomepageData(c *gin.Context) {
	cfg, hash, err := config.LoadHomepageConfig(hc.configPath)
	if err != nil {
		logger.WithComponent("homepage_controller").Errorf("failed to reload homepage config from %s: %v", hc.configPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "homepage configuration unavailable"})
		return
	}

	c.JSON(http.StatusOK, HomepageResponse{HomepageConfig: *cfg, Hash: hash})
}
