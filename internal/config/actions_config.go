package config

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// ActionConfig holds the technical configuration for a single HTTP action.
// These fields are server-side only and never exposed to the browser.
type ActionConfig struct {
	Method  string            `yaml:"method"  json:"method"`
	URL     string            `yaml:"url"     json:"url"`
	Headers map[string]string `yaml:"headers" json:"headers"`
	Body    string            `yaml:"body"    json:"body"`
}

// ActionsFile is the root structure of the actions.yml configuration file.
type ActionsFile struct {
	Actions map[string]ActionConfig `yaml:"actions"`
}

// LoadActionsConfig reads the YAML file at the given path and returns a parsed
// ActionsFile. If the file does not exist, it returns an empty actions map
// with a nil error.
func LoadActionsConfig(path string) (*ActionsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ActionsFile{Actions: make(map[string]ActionConfig)}, nil
		}
		return nil, fmt.Errorf("failed to read actions config file %s: %w", path, err)
	}

	var cfg ActionsFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse actions config file %s: %w", path, err)
	}

	if cfg.Actions == nil {
		cfg.Actions = make(map[string]ActionConfig)
	}

	// Validate required fields for each action
	for name, action := range cfg.Actions {
		if action.Method == "" {
			return nil, fmt.Errorf("action %q: method is required", name)
		}
		if action.URL == "" {
			return nil, fmt.Errorf("action %q: url is required", name)
		}
		// SSRF guard: only http/https are permitted. This blocks dangerous
		// schemes such as file://, ftp:// or gopher:// from being proxied.
		u, err := url.Parse(action.URL)
		if err != nil {
			return nil, fmt.Errorf("action %q: invalid url: %w", name, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("action %q: unsupported url scheme %q (only http and https are allowed)", name, u.Scheme)
		}
	}

	return &cfg, nil
}
