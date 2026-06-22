package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockContainerStore implements cache.ContainerStore for testing
type mockContainerStore struct {
	addErr    error
	removeErr error
	doc       repository.DataDocument
}

func (m *mockContainerStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, nil
}

func (m *mockContainerStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	if m.addErr != nil {
		return repository.DataDocument{}, m.addErr
	}
	m.doc.Containers = append(m.doc.Containers, c)
	return m.doc, nil
}

func (m *mockContainerStore) RemoveContainer(name string) (repository.DataDocument, error) {
	if m.removeErr != nil {
		return repository.DataDocument{}, m.removeErr
	}
	for i, c := range m.doc.Containers {
		if c.Name == name {
			m.doc.Containers = append(m.doc.Containers[:i], m.doc.Containers[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, cache.ErrContainerNotFound
}

// mockContainerRuntimeForContainer implements runtime.ContainerRuntime for testing
type mockContainerRuntimeForContainer struct{}

func (m *mockContainerRuntimeForContainer) IsRunning(ctx context.Context, containerName string) (bool, error) {
	return false, nil
}

func (m *mockContainerRuntimeForContainer) Start(ctx context.Context, containerName string) error {
	return nil
}

func (m *mockContainerRuntimeForContainer) Stop(ctx context.Context, containerName string) error {
	return nil
}

func (m *mockContainerRuntimeForContainer) ListContainers(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockContainerRuntimeForContainer) Stats(ctx context.Context, containerName string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func TestContainerController_AllContainers(t *testing.T) {
	active := true
	running := false
	store := &mockContainerStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "test1", FriendlyName: "Test 1", URL: "http://test1.local", Active: &active, Running: &running},
				{Name: "test2", FriendlyName: "Test 2", URL: "http://test2.local", Active: &active, Running: &running},
			},
		},
	}

	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.GET("/containers", cc.AllContainers)

	req := httptest.NewRequest(http.MethodGet, "/containers", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var containers []repository.Container
	if err := json.Unmarshal(w.Body.Bytes(), &containers); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(containers))
	}
}

func TestContainerController_CreateOrUpdateContainer_Valid(t *testing.T) {
	store := &mockContainerStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{},
		},
	}

	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.POST("/container", cc.CreateOrUpdateContainer)

	active := true
	running := false
	container := repository.Container{
		Name:         "new-container",
		FriendlyName: "New Container",
		URL:          "http://new.local",
		Active:       &active,
		Running:      &running,
	}
	body, _ := json.Marshal(container)

	req := httptest.NewRequest(http.MethodPost, "/container", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContainerController_CreateOrUpdateContainer_InvalidPayload(t *testing.T) {
	store := &mockContainerStore{}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.POST("/container", cc.CreateOrUpdateContainer)

	req := httptest.NewRequest(http.MethodPost, "/container", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestContainerController_CreateOrUpdateContainer_ValidationError(t *testing.T) {
	store := &mockContainerStore{}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.POST("/container", cc.CreateOrUpdateContainer)

	// Missing required fields
	container := map[string]any{
		"name": "test",
		// missing friendly_name, url, active, running
	}
	body, _ := json.Marshal(container)

	req := httptest.NewRequest(http.MethodPost, "/container", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for validation error, got %d", w.Code)
	}
}

func TestContainerController_CreateOrUpdateContainer_StoreError(t *testing.T) {
	store := &mockContainerStore{
		addErr: errors.New("store error"),
	}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.POST("/container", cc.CreateOrUpdateContainer)

	active := true
	running := false
	container := repository.Container{
		Name:         "test",
		FriendlyName: "Test",
		URL:          "http://test.local",
		Active:       &active,
		Running:      &running,
	}
	body, _ := json.Marshal(container)

	req := httptest.NewRequest(http.MethodPost, "/container", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestContainerController_DeleteContainer_Success(t *testing.T) {
	active := true
	running := false
	store := &mockContainerStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "to-delete", FriendlyName: "To Delete", URL: "http://del.local", Active: &active, Running: &running},
			},
		},
	}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.DELETE("/container/:name", cc.DeleteContainer)

	req := httptest.NewRequest(http.MethodDelete, "/container/to-delete", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestContainerController_DeleteContainer_NotFound(t *testing.T) {
	store := &mockContainerStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{},
		},
	}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	r.DELETE("/container/:name", cc.DeleteContainer)

	req := httptest.NewRequest(http.MethodDelete, "/container/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestContainerController_DeleteContainer_MissingName(t *testing.T) {
	store := &mockContainerStore{}
	cc := NewContainerController(context.Background(), store, &mockContainerRuntimeForContainer{}, "", "")

	r := gin.New()
	// Route without :name param
	r.DELETE("/container/", cc.DeleteContainer)

	req := httptest.NewRequest(http.MethodDelete, "/container/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// mockRuntime allows configuring IsRunning responses for testing Ready()
type mockRuntime struct {
	err     error
	running bool
}

func (m *mockRuntime) IsRunning(ctx context.Context, containerName string) (bool, error) {
	return m.running, m.err
}
func (m *mockRuntime) Start(ctx context.Context, containerName string) error { return nil }
func (m *mockRuntime) Stop(ctx context.Context, containerName string) error  { return nil }
func (m *mockRuntime) ListContainers(ctx context.Context) ([]string, error)  { return []string{}, nil }
func (m *mockRuntime) Stats(ctx context.Context, containerName string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func TestContainerController_Ready_MissingName(t *testing.T) {
	store := &mockContainerStore{}
	cc := NewContainerController(context.Background(), store, &mockRuntime{running: true}, "", "")

	r := gin.New()
	// register a route that does not provide :name so Param("name") is empty
	r.GET("/container/ready", cc.Ready)

	req := httptest.NewRequest(http.MethodGet, "/container/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestContainerController_Ready_NotFound(t *testing.T) {
	store := &mockContainerStore{doc: repository.DataDocument{Containers: []repository.Container{}}}
	cc := NewContainerController(context.Background(), store, &mockRuntime{running: true}, "", "")

	r := gin.New()
	r.GET("/container/:name/ready", cc.Ready)

	req := httptest.NewRequest(http.MethodGet, "/container/nonexistent/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestContainerController_Ready_RuntimeErrorAndNotRunning(t *testing.T) {
	active := true
	running := false
	// runtime returns error
	store := &mockContainerStore{doc: repository.DataDocument{Containers: []repository.Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Active: &active, Running: &running}}}}
	cc := NewContainerController(context.Background(), store, &mockRuntime{running: false, err: errors.New("rt error")}, "", "")

	r := gin.New()
	r.GET("/container/:name/ready", cc.Ready)

	req := httptest.NewRequest(http.MethodGet, "/container/c1/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 on runtime error, got %d", w.Code)
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if v, ok := resp["ready"]; !ok || v != false {
		t.Errorf("expected ready=false on runtime error, got %v", resp)
	}

	// runtime returns not running (false, nil)
	cc = NewContainerController(context.Background(), store, &mockRuntime{running: false, err: nil}, "", "")
	r = gin.New()
	r.GET("/container/:name/ready", cc.Ready)
	req = httptest.NewRequest(http.MethodGet, "/container/c1/ready", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 when not running, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if v, ok := resp["ready"]; !ok || v != false {
		t.Errorf("expected ready=false when not running, got %v", resp)
	}
}

func TestContainerController_Ready_EmptyURL(t *testing.T) {
	active := true
	running := true
	store := &mockContainerStore{doc: repository.DataDocument{Containers: []repository.Container{{Name: "c2", FriendlyName: "C2", URL: "", Active: &active, Running: &running}}}}
	cc := NewContainerController(context.Background(), store, &mockRuntime{running: true}, "", "")

	r := gin.New()
	r.GET("/container/:name/ready", cc.Ready)

	req := httptest.NewRequest(http.MethodGet, "/container/c2/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for empty URL, got %d", w.Code)
	}
}

func TestContainerController_Ready_HTTPCheck(t *testing.T) {
	// Start a test server that returns 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	active := true
	running := true
	// Use the test server URL as container URL
	store := &mockContainerStore{doc: repository.DataDocument{Containers: []repository.Container{{Name: "c3", FriendlyName: "C3", URL: ts.URL, Active: &active, Running: &running}}}}
	cc := NewContainerController(context.Background(), store, &mockRuntime{running: true}, "", "")

	r := gin.New()
	r.GET("/container/:name/ready", cc.Ready)

	req := httptest.NewRequest(http.MethodGet, "/container/c3/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for http check, got %d", w.Code)
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if v, ok := resp["ready"]; !ok || v != true {
		t.Errorf("expected ready=true for http 200, got %v", resp)
	}

	// Start a server that returns 500
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts2.Close()

	store = &mockContainerStore{doc: repository.DataDocument{Containers: []repository.Container{{Name: "c4", FriendlyName: "C4", URL: ts2.URL, Active: &active, Running: &running}}}}
	cc = NewContainerController(context.Background(), store, &mockRuntime{running: true}, "", "")
	r = gin.New()
	r.GET("/container/:name/ready", cc.Ready)
	req = httptest.NewRequest(http.MethodGet, "/container/c4/ready", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for http non-200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if v, ok := resp["ready"]; !ok || v != false {
		t.Errorf("expected ready=false for http non-200, got %v", resp)
	}
}
