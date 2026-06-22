package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/config"
)

// ConfigurationResponse represents the configuration response structure for the API.
type ConfigurationResponse struct {
	BaseUrl                 string `json:"baseUrl"`
	SpinUpUrl               string `json:"spinUpUrl"`
	RefreshIntervalSec      int    `json:"refreshIntervalSec"`
	StatsRefreshIntervalSec int    `json:"statsRefreshIntervalSec"`
}

// ConfigurationController handles configuration-related API endpoints.
type ConfigurationController struct {
	config *config.Config
}

// NewConfigurationController creates a new ConfigurationController.
func NewConfigurationController(cfg *config.Config) *ConfigurationController {
	return &ConfigurationController{
		config: cfg,
	}
}

// GetConfiguration returns the application configuration for the frontend.
func (cc *ConfigurationController) GetConfiguration(c *gin.Context) {
	response := ConfigurationResponse{
		BaseUrl:                 cc.config.Data.BaseUrl,
		SpinUpUrl:               cc.config.Data.SpinUpUrl,
		RefreshIntervalSec:      cc.config.Data.RefreshIntervalSecs,
		StatsRefreshIntervalSec: cc.config.Data.StatsRefreshIntervalSecs,
	}
	c.JSON(http.StatusOK, response)
}
