package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BookmarkItem represents a single bookmark
type BookmarkItem struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	Abbr string `yaml:"abbr" json:"abbr"` // 2-char abbreviation
}

// BookmarkGroup represents a group of bookmarks
type BookmarkGroup struct {
	Group string         `yaml:"group" json:"group"`
	Items []BookmarkItem `yaml:"items" json:"items"`
}

// ServiceItem represents a single service card
type ServiceItem struct {
	Name        string `yaml:"name" json:"name"`
	URL         string `yaml:"url" json:"url"`
	Description string `yaml:"description" json:"description"`
	Icon        string `yaml:"icon" json:"icon"`
}

// ServiceGroup represents a group of services
type ServiceGroup struct {
	Group string        `yaml:"group" json:"group"`
	Items []ServiceItem `yaml:"items" json:"items"`
}

// HomepageSettings holds the settings section
type HomepageSettings struct {
	Theme                      string `yaml:"theme" json:"theme"`
	Title                      string `yaml:"title" json:"title"`
	Version                    string `yaml:"version" json:"version"`
	FontFamily                 string `yaml:"font_family" json:"fontFamily"`
	FontSize                   string `yaml:"font_size" json:"fontSize"`
	PollingIntervalSeconds      int    `yaml:"polling_interval_seconds" json:"pollingIntervalSeconds"`
	StatsPollingIntervalSeconds int    `yaml:"stats_polling_interval_seconds" json:"statsPollingIntervalSeconds"`
}

// DefaultPollingIntervalSeconds is the default polling interval for config file change detection
const DefaultPollingIntervalSeconds = 10

// DefaultStatsPollingIntervalSeconds is the default polling interval for system stats refresh
const DefaultStatsPollingIntervalSeconds = 3

// DefaultFontFamily is the default font stack
const DefaultFontFamily = "Inter, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"

// DefaultFontSize is the default base font size (slightly larger than browser default)
const DefaultFontSize = "17px"

// dashboardIconsBase is the CDN base URL for homarr-labs dashboard-icons
const dashboardIconsBase = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"

// resolveIconURL resolves an icon reference to a full CDN URL.
// If the icon is already a full URL (http/https), it is returned as-is.
// Otherwise, the extension is extracted and the URL is built as:
//
//	https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/{ext}/{filename}
//
// Examples:
//
//	bitmagnet.png → https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/bitmagnet.png
//	palla.svg     → https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/palla.svg
func resolveIconURL(icon string) string {
	if icon == "" {
		return ""
	}

	// Already a full URL
	if strings.HasPrefix(icon, "http://") || strings.HasPrefix(icon, "https://") {
		return icon
	}

	// Extract extension (without dot) and build CDN URL
	ext := strings.TrimPrefix(filepath.Ext(icon), ".")
	if ext == "" {
		return icon // no extension, return as-is
	}

	return fmt.Sprintf("%s/%s/%s", dashboardIconsBase, ext, icon)
}

// HomepageConfig is the root struct
type HomepageConfig struct {
	Services  []ServiceGroup   `yaml:"services" json:"services"`
	Bookmarks []BookmarkGroup  `yaml:"bookmarks" json:"bookmarks"`
	Settings  HomepageSettings `yaml:"settings" json:"settings"`
}

// LoadHomepageConfig reads the YAML file at the given path and returns a parsed HomepageConfig.
// If the file does not exist, it returns an empty config with a nil error.
// It also returns the SHA-256 hash of the raw file content for change detection.
func LoadHomepageConfig(path string) (*HomepageConfig, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &HomepageConfig{}
			cfg.Settings.PollingIntervalSeconds = DefaultPollingIntervalSeconds
		cfg.Settings.StatsPollingIntervalSeconds = DefaultStatsPollingIntervalSeconds
		cfg.Settings.FontFamily = DefaultFontFamily
		cfg.Settings.FontSize = DefaultFontSize
		return cfg, "", nil
		}
		return nil, "", fmt.Errorf("failed to read homepage config file: %w", err)
	}

	// Compute hash for change detection
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	var cfg HomepageConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("failed to parse homepage config file: %w", err)
	}

	// Apply default polling interval if not set
	if cfg.Settings.PollingIntervalSeconds <= 0 {
		cfg.Settings.PollingIntervalSeconds = DefaultPollingIntervalSeconds
	}
	if cfg.Settings.StatsPollingIntervalSeconds <= 0 {
		cfg.Settings.StatsPollingIntervalSeconds = DefaultStatsPollingIntervalSeconds
	}
	if cfg.Settings.FontFamily == "" {
		cfg.Settings.FontFamily = DefaultFontFamily
	}
	if cfg.Settings.FontSize == "" {
		cfg.Settings.FontSize = DefaultFontSize
	}

	// Resolve icon URLs
	for i := range cfg.Services {
		for j := range cfg.Services[i].Items {
			cfg.Services[i].Items[j].Icon = resolveIconURL(cfg.Services[i].Items[j].Icon)
		}
	}

	return &cfg, hash, nil
}
