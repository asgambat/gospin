package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/version"
)

func TestHomepageController_GetHomepageData_ValidConfig(t *testing.T) {
	// Create a temporary YAML config file
	content := `
services:
  - group: TestGroup
    items:
      - name: TestService
        url: https://example.com
        description: A test service
        icon: test.svg
bookmarks:
  - group: TestBookmarks
    items:
      - name: Example
        url: https://example.com
        abbr: EX
settings:
  theme: Test Theme
  title: Test Title
  title_font_size: "2rem"
  # version field removed: it is not user-configurable anymore.
  polling_interval_seconds: 5
`
	tmpFile, err := os.CreateTemp("", "homepage_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	hc := NewHomepageController(tmpFile.Name())

	r := gin.New()
	r.GET("/homepage", hc.GetHomepageData)

	req := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp HomepageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Services) != 1 {
		t.Errorf("expected 1 service group, got %d", len(resp.Services))
	}
	if resp.Services[0].Group != "TestGroup" {
		t.Errorf("expected service group 'TestGroup', got %s", resp.Services[0].Group)
	}
	if resp.Settings.Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got %s", resp.Settings.Title)
	}
	// Version is now a top-level field sourced from internal/version.Version
	// (not from homepage.yaml). Even if a user sneaks "version: foo" into the
	// YAML, it must be ignored — the server value wins.
	if resp.Version != version.Version {
		t.Errorf("expected top-level version %q (from internal/version), got %q", version.Version, resp.Version)
	}
	if resp.Hash == "" {
		t.Error("expected non-empty hash in response")
	}
	if resp.Settings.PollingIntervalSeconds != 5 {
		t.Errorf("expected polling interval 5, got %d", resp.Settings.PollingIntervalSeconds)
	}
	if resp.Settings.TitleFontSize != "2rem" {
		t.Errorf("expected title font size '2rem', got %q", resp.Settings.TitleFontSize)
	}
}

// TestHomepageController_GetHomepageData_YAMLVersionIgnored defends against
// users re-adding "version: foo" to their homepage.yaml: the YAML key must
// be silently ignored and the top-level response must still report the
// internal software version.
func TestHomepageController_GetHomepageData_YAMLVersionIgnored(t *testing.T) {
	content := `
settings:
  title: Test
  version: "hax0r-override-attempt"
`
	tmpFile, err := os.CreateTemp("", "homepage_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	hc := NewHomepageController(tmpFile.Name())
	r := gin.New()
	r.GET("/homepage", hc.GetHomepageData)

	req := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp HomepageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Version != version.Version {
		t.Errorf("expected top-level version %q (internal) to win over YAML, got %q", version.Version, resp.Version)
	}
	if resp.Version == "hax0r-override-attempt" {
		t.Error("YAML `version` key leaked into top-level response — it must be ignored")
	}
}

func TestHomepageController_GetHomepageData_MissingFile(t *testing.T) {

	hc := NewHomepageController("/nonexistent/path/homepage.yaml")

	r := gin.New()
	r.GET("/homepage", hc.GetHomepageData)

	req := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for missing file (returns empty config), got %d: %s", w.Code, w.Body.String())
	}

	var resp HomepageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Empty config should have default polling interval
	if resp.Settings.PollingIntervalSeconds != 10 {
		t.Errorf("expected default polling interval 10, got %d", resp.Settings.PollingIntervalSeconds)
	}
	// Empty config should have default title font size
	if resp.Settings.TitleFontSize != "1.25rem" {
		t.Errorf("expected default title font size '1.25rem', got %q", resp.Settings.TitleFontSize)
	}
}

func TestHomepageController_GetHomepageData_DefaultTitleFontSize(t *testing.T) {
	// Valid YAML without title_font_size: the field default must be applied.
	content := `
settings:
  title: Test
`
	tmpFile, err := os.CreateTemp("", "homepage_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	hc := NewHomepageController(tmpFile.Name())

	r := gin.New()
	r.GET("/homepage", hc.GetHomepageData)

	req := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp HomepageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Settings.TitleFontSize != "1.25rem" {
		t.Errorf("expected default title font size '1.25rem' when YAML omits title_font_size, got %q", resp.Settings.TitleFontSize)
	}
}

func TestHomepageController_GetHomepageData_HashChanges(t *testing.T) {
	// Create a temporary YAML config file
	content := `
settings:
  title: Test
`
	tmpFile, err := os.CreateTemp("", "homepage_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	hc := NewHomepageController(tmpFile.Name())

	r := gin.New()
	r.GET("/homepage", hc.GetHomepageData)

	// First request
	req1 := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	var resp1 HomepageResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Modify config file
	content2 := `
settings:
  title: Changed
`
	if err := os.WriteFile(tmpFile.Name(), []byte(content2), 0644); err != nil {
		t.Fatalf("failed to write updated temp file: %v", err)
	}

	// Second request — hash should differ
	req2 := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var resp2 HomepageResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp1.Hash == resp2.Hash {
		t.Error("expected hash to change after file modification")
	}
	if resp2.Settings.Title != "Changed" {
		t.Errorf("expected title 'Changed', got %s", resp2.Settings.Title)
	}
}
