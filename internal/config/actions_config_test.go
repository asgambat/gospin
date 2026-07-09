package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadActionsConfig_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  test-action:
    method: POST
    url: "https://example.com/api"
    headers:
      Authorization: "Bearer secret"
      Content-Type: "application/json"
    body: '{"key": "value"}'
  get-action:
    method: GET
    url: "https://example.com/health"
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test actions file: %v", err)
	}

	cfg, err := LoadActionsConfig(actionsPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Actions == nil {
		t.Fatal("expected non-nil actions map")
	}
	if len(cfg.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(cfg.Actions))
	}

	// Check first action
	action, ok := cfg.Actions["test-action"]
	if !ok {
		t.Fatal("expected 'test-action' to exist")
	}
	if action.Method != "POST" {
		t.Errorf("expected method POST, got %s", action.Method)
	}
	if action.URL != "https://example.com/api" {
		t.Errorf("expected URL https://example.com/api, got %s", action.URL)
	}
	if action.Body != `{"key": "value"}` {
		t.Errorf("expected body '{\"key\": \"value\"}', got %s", action.Body)
	}
	if len(action.Headers) != 2 {
		t.Errorf("expected 2 headers, got %d", len(action.Headers))
	}
	if action.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("expected Authorization header 'Bearer secret', got '%s'", action.Headers["Authorization"])
	}
	if action.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type header 'application/json', got '%s'", action.Headers["Content-Type"])
	}

	// Check second action (GET with no headers/body)
	getAction, ok := cfg.Actions["get-action"]
	if !ok {
		t.Fatal("expected 'get-action' to exist")
	}
	if getAction.Method != "GET" {
		t.Errorf("expected method GET, got %s", getAction.Method)
	}
	if getAction.URL != "https://example.com/health" {
		t.Errorf("expected URL https://example.com/health, got %s", getAction.URL)
	}
}

func TestLoadActionsConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadActionsConfig("/nonexistent/path/actions.yml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Actions == nil {
		t.Fatal("expected non-nil actions map")
	}
	if len(cfg.Actions) != 0 {
		t.Errorf("expected empty actions map, got %d entries", len(cfg.Actions))
	}
}

func TestLoadActionsConfig_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	if err := os.WriteFile(actionsPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty actions file: %v", err)
	}

	cfg, err := LoadActionsConfig(actionsPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Actions == nil {
		t.Fatal("expected non-nil actions map after loading empty file")
	}
	if len(cfg.Actions) != 0 {
		t.Errorf("expected empty actions map, got %d entries", len(cfg.Actions))
	}
}

func TestLoadActionsConfig_NoActionsKey(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `# just a comment, no actions key
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadActionsConfig(actionsPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Actions == nil {
		t.Fatal("expected non-nil actions map when 'actions' key is absent")
	}
	if len(cfg.Actions) != 0 {
		t.Errorf("expected empty actions map, got %d entries", len(cfg.Actions))
	}
}

func TestLoadActionsConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  - this is invalid yaml for a map
    because actions should be a map, not a list
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadActionsConfig(actionsPath)
	if err == nil {
		t.Error("expected error for invalid YAML structure, got nil")
	}
}

func TestLoadActionsConfig_ActionWithoutHeaders(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  minimal-action:
    method: DELETE
    url: "https://example.com/resource/1"
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadActionsConfig(actionsPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	action, ok := cfg.Actions["minimal-action"]
	if !ok {
		t.Fatal("expected 'minimal-action' to exist")
	}
	if action.Method != "DELETE" {
		t.Errorf("expected method DELETE, got %s", action.Method)
	}
	if action.URL != "https://example.com/resource/1" {
		t.Errorf("expected URL, got %s", action.URL)
	}
	if action.Headers != nil {
		t.Errorf("expected nil headers, got %v", action.Headers)
	}
	if action.Body != "" {
		t.Errorf("expected empty body, got '%s'", action.Body)
	}
}

func TestLoadActionsConfig_MissingMethod(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  bad-action:
    url: "https://example.com/api"
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadActionsConfig(actionsPath)
	if err == nil {
		t.Error("expected error for action with missing method")
	}
}

func TestLoadActionsConfig_MissingURL(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  bad-action:
    method: POST
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadActionsConfig(actionsPath)
	if err == nil {
		t.Error("expected error for action with missing url")
	}
}

func TestLoadActionsConfig_UnsupportedScheme(t *testing.T) {
	schemes := []string{"file:///etc/passwd", "ftp://example.com/x", "gopher://example.com", "data:text/plain,hello"}
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			tmpDir := t.TempDir()
			actionsPath := filepath.Join(tmpDir, "actions.yml")

			content := "actions:\n  bad-action:\n    method: GET\n    url: \"" + scheme + "\"\n"
			if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			_, err := LoadActionsConfig(actionsPath)
			if err == nil {
				t.Errorf("expected error for unsupported scheme %q", scheme)
			}
		})
	}
}

func TestLoadActionsConfig_AllowedSchemes(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			tmpDir := t.TempDir()
			actionsPath := filepath.Join(tmpDir, "actions.yml")

			content := "actions:\n  ok-action:\n    method: GET\n    url: \"" + scheme + "://example.com\"\n"
			if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			cfg, err := LoadActionsConfig(actionsPath)
			if err != nil {
				t.Fatalf("expected no error for scheme %q, got: %v", scheme, err)
			}
			if _, ok := cfg.Actions["ok-action"]; !ok {
				t.Errorf("expected ok-action to be loaded for scheme %q", scheme)
			}
		})
	}
}

func TestLoadActionsConfig_EmptyMethod(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  bad-action:
    method: ""
    url: "https://example.com/api"
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadActionsConfig(actionsPath)
	if err == nil {
		t.Error("expected error for action with empty method")
	}
}

func TestLoadActionsConfig_MultipleActions(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")

	content := `actions:
  action-a:
    method: GET
    url: "https://a.example.com"
  action-b:
    method: POST
    url: "https://b.example.com"
    body: "hello"
  action-c:
    method: PUT
    url: "https://c.example.com"
    headers:
      X-Custom: "value"
`
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadActionsConfig(actionsPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(cfg.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(cfg.Actions))
	}

	for _, name := range []string{"action-a", "action-b", "action-c"} {
		if _, ok := cfg.Actions[name]; !ok {
			t.Errorf("expected '%s' to exist in actions map", name)
		}
	}
}
