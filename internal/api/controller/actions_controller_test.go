package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestActionController_ExecuteAction_Sync(t *testing.T) {
	// Start a test HTTP server to act as the target
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("X-Custom") != "test-value" {
			t.Errorf("expected header X-Custom: test-value, got %s", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "ok"}`))
	}))
	defer targetServer.Close()

	// Create actions.yml pointing to the test server
	actionsYAML := "actions:\n  test-action:\n    method: POST\n    url: \"" + targetServer.URL + "/api\"\n    headers:\n      X-Custom: \"test-value\"\n    body: '{\"key\": \"value\"}'\n"
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte(actionsYAML), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	ac := NewActionController(actionsPath, "sync", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "test-action"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp actionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected statusCode 200, got %d", resp.StatusCode)
	}
	if resp.Body != `{"result": "ok"}` {
		t.Errorf("expected body '{\"result\": \"ok\"}', got '%s'", resp.Body)
	}
}

func TestActionController_ExecuteAction_Async(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer targetServer.Close()

	actionsYAML := "actions:\n  test-action:\n    method: GET\n    url: \"" + targetServer.URL + "/ping\"\n"
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte(actionsYAML), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	ac := NewActionController(actionsPath, "async", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "test-action"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp actionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected statusCode 201, got %d", resp.StatusCode)
	}
	// In async mode, body should be empty
	if resp.Body != "" {
		t.Errorf("expected empty body in async mode, got '%s'", resp.Body)
	}
}

func TestActionController_ExecuteAction_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte("actions: {}\n"), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	ac := NewActionController(actionsPath, "async", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActionController_ExecuteAction_MissingActionName(t *testing.T) {
	ac := NewActionController("/nonexistent/actions.yml", "async", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActionController_ExecuteAction_TargetError(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "something went wrong"}`))
	}))
	defer targetServer.Close()

	actionsYAML := "actions:\n  test-action:\n    method: GET\n    url: \"" + targetServer.URL + "/error\"\n"
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte(actionsYAML), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	ac := NewActionController(actionsPath, "sync", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "test-action"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp actionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected statusCode 500, got %d", resp.StatusCode)
	}
	// In sync mode, body should contain the target's error response
	if resp.Body != `{"error": "something went wrong"}` {
		t.Errorf("expected body with error, got '%s'", resp.Body)
	}
}

func TestActionController_ExecuteAction_ActionsFileNotFound(t *testing.T) {
	ac := NewActionController("/nonexistent/path/actions.yml", "async", 30*time.Second)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "test-action"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Actions file not found should result in 404 (action not found in empty map)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNewActionController_Defaults(t *testing.T) {
	// Empty mode defaults to "async"
	ac := NewActionController("/path", "", 0)
	if ac.actionsMode != "async" {
		t.Errorf("expected default mode 'async', got '%s'", ac.actionsMode)
	}
	if ac.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", ac.timeout)
	}
}

func TestActionController_ExecuteAction_Timeout(t *testing.T) {
	// Create a server that sleeps longer than our timeout
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the 1s timeout we'll configure
		<-r.Context().Done() // wait for client to timeout
	}))
	defer targetServer.Close()

	actionsYAML := "actions:\n  test-action:\n    method: GET\n    url: \"" + targetServer.URL + "/slow\"\n"
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte(actionsYAML), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	// Use a 100ms timeout to keep the test fast
	ac := NewActionController(actionsPath, "async", 100*time.Millisecond)

	r := gin.New()
	r.POST("/homepage/action", ac.ExecuteAction)

	reqBody := `{"actionName": "test-action"}`
	req := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should get a 502 Bad Gateway due to timeout
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 Bad Gateway, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActionController_ExecuteAction_ContextCancelled(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the (cancelled) client request context is done.
		<-r.Context().Done()
	}))
	defer targetServer.Close()

	actionsYAML := "actions:\n  test-action:\n    method: GET\n    url: \"" + targetServer.URL + "/slow\"\n"
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	if err := os.WriteFile(actionsPath, []byte(actionsYAML), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	// Large timeout: the only thing that should abort the call is the
	// (already cancelled) request context, proving the outbound request
	// honors c.Request.Context().
	ac := NewActionController(actionsPath, "async", 30*time.Second)

	reqBody := `{"actionName": "test-action"}`
	httpReq := httptest.NewRequest(http.MethodPost, "/homepage/action", strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(httpReq.Context())
	cancel()
	httpReq = httpReq.WithContext(ctx)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httpReq

	start := time.Now()
	ac.ExecuteAction(c)
	elapsed := time.Since(start)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway on cancelled context, got %d: %s", w.Code, w.Body.String())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("handler did not honor cancelled context, took %s", elapsed)
	}
}

func TestActionController_LogDanglingReferences_DetectsMissing(t *testing.T) {
	tmpDir := t.TempDir()
	actionsPath := filepath.Join(tmpDir, "actions.yml")
	content := "actions:\n  defined-action:\n    method: GET\n    url: \"https://example.com\"\n"
	if err := os.WriteFile(actionsPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write actions.yml: %v", err)
	}

	ac := NewActionController(actionsPath, "async", 30*time.Second)

	refs := []string{"defined-action", "missing-action", "also-missing"}
	dangling := ac.LogDanglingReferences(refs)

	if len(dangling) != 2 {
		t.Fatalf("expected 2 dangling references, got %d: %v", len(dangling), dangling)
	}
	got := make(map[string]bool)
	for _, n := range dangling {
		got[n] = true
	}
	if !got["missing-action"] || !got["also-missing"] {
		t.Errorf("expected missing-action and also-missing in dangling list, got %v", dangling)
	}
	if got["defined-action"] {
		t.Errorf("defined-action should not be reported as dangling")
	}
}

func TestActionController_LogDanglingReferences_Empty(t *testing.T) {
	ac := NewActionController("/nonexistent/actions.yml", "async", 30*time.Second)
	if d := ac.LogDanglingReferences(nil); d != nil {
		t.Errorf("expected nil for empty references, got %v", d)
	}
}
