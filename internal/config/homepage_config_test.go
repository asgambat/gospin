package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHomepageConfig_MissingFileUsesDefaults(t *testing.T) {
	cfg, hash, err := LoadHomepageConfig(filepath.Join(t.TempDir(), "homepage.yaml"))
	if err != nil {
		t.Fatalf("LoadHomepageConfig() error = %v, want nil for a missing file", err)
	}
	if hash != "" {
		t.Errorf("hash = %q, want empty for a missing file", hash)
	}
	if cfg.Settings.PollingIntervalSeconds != DefaultPollingIntervalSeconds {
		t.Errorf("polling interval = %d, want %d", cfg.Settings.PollingIntervalSeconds, DefaultPollingIntervalSeconds)
	}
	if cfg.Settings.StatsPollingIntervalSeconds != DefaultStatsPollingIntervalSeconds {
		t.Errorf("stats polling interval = %d, want %d", cfg.Settings.StatsPollingIntervalSeconds, DefaultStatsPollingIntervalSeconds)
	}
	if cfg.Settings.FontFamily != DefaultFontFamily || cfg.Settings.FontSize != DefaultFontSize || cfg.Settings.TitleFontSize != DefaultTitleFontSize {
		t.Errorf("settings defaults = %+v, want the package defaults", cfg.Settings)
	}
	if cfg.Services == nil || cfg.Bookmarks == nil || cfg.Actions == nil {
		t.Errorf("sections = %v/%v/%v, want empty non-nil slices", cfg.Services, cfg.Bookmarks, cfg.Actions)
	}
}

func TestLoadHomepageConfig_SectionsMissingFromFileAreEmptyArrays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homepage.yaml")
	content := "services:\n  - group: UTILITY\n    items:\n      - name: Test\n        url: https://example.com\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write homepage.yaml: %v", err)
	}

	cfg, hash, err := LoadHomepageConfig(path)
	if err != nil {
		t.Fatalf("LoadHomepageConfig() error = %v", err)
	}
	if hash == "" {
		t.Error("hash is empty, want the content hash")
	}
	if cfg.Actions == nil || cfg.Bookmarks == nil {
		t.Fatalf("actions/bookmarks = %v/%v, want empty non-nil slices", cfg.Actions, cfg.Bookmarks)
	}

	// The API response must carry [] rather than null: the UI iterates these
	// sections with x-for, which fails on a null value.
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), `"actions":null`) || strings.Contains(string(data), `"bookmarks":null`) {
		t.Errorf("marshalled config contains null sections: %s", data)
	}
}

func TestLoadHomepageConfig_GroupWithoutItemsGetsEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homepage.yaml")
	content := "services:\n  - group: UTILITY\nbookmarks:\n  - group: Developer\nactions:\n  - group: Controlli\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write homepage.yaml: %v", err)
	}

	cfg, _, err := LoadHomepageConfig(path)
	if err != nil {
		t.Fatalf("LoadHomepageConfig() error = %v", err)
	}
	if cfg.Services[0].Items == nil || cfg.Bookmarks[0].Items == nil || cfg.Actions[0].Items == nil {
		t.Errorf("item lists = %v/%v/%v, want empty non-nil slices",
			cfg.Services[0].Items, cfg.Bookmarks[0].Items, cfg.Actions[0].Items)
	}
}

func TestLoadHomepageConfig_ResolvesIcons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homepage.yaml")
	content := `services:
  - group: UTILITY
    items:
      - name: Shorthand
        url: https://example.com
        icon: bitmagnet.png
      - name: Absolute
        url: https://example.com
        icon: https://cdn.example.com/icon.svg
actions:
  - group: Controlli
    items:
      - name: Avvia
        action: start
        icon: firefox.png
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write homepage.yaml: %v", err)
	}

	cfg, _, err := LoadHomepageConfig(path)
	if err != nil {
		t.Fatalf("LoadHomepageConfig() error = %v", err)
	}

	if got, want := cfg.Services[0].Items[0].Icon, dashboardIconsBase+"/png/bitmagnet.png"; got != want {
		t.Errorf("shorthand icon = %q, want %q", got, want)
	}
	if got, want := cfg.Services[0].Items[1].Icon, "https://cdn.example.com/icon.svg"; got != want {
		t.Errorf("absolute icon = %q, want %q", got, want)
	}
	if got, want := cfg.Actions[0].Items[0].Icon, dashboardIconsBase+"/png/firefox.png"; got != want {
		t.Errorf("action icon = %q, want %q", got, want)
	}
}

func TestLoadHomepageConfig_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homepage.yaml")
	if err := os.WriteFile(path, []byte("services: [broken\n"), 0644); err != nil {
		t.Fatalf("failed to write homepage.yaml: %v", err)
	}

	if _, _, err := LoadHomepageConfig(path); err == nil {
		t.Fatal("LoadHomepageConfig() error = nil, want a parse error")
	}
}
