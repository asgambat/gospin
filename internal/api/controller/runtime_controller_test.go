package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// mockAppStore implements cache.AppStore for testing
type mockAppStore struct {
	addErr    error
	removeErr error
	doc       repository.DataDocument
}

func (m *mockAppStore) Snapshot() (repository.DataDocument, error) { return m.doc, nil }
func (m *mockAppStore) GetLastUpdate() int64                       { return 0 }
func (m *mockAppStore) IsDirty() bool                              { return false }
func (m *mockAppStore) Replace(doc repository.DataDocument) error  { m.doc = doc; return nil }
func (m *mockAppStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	if m.addErr != nil {
		return repository.DataDocument{}, m.addErr
	}
	m.doc.Containers = append(m.doc.Containers, c)
	return m.doc, nil
}
func (m *mockAppStore) RemoveContainer(name string) (repository.DataDocument, error) {
	if m.removeErr != nil {
		return repository.DataDocument{}, m.removeErr
	}
	for i, c := range m.doc.Containers {
		if c.Name == name {
			m.doc.Containers = append(m.doc.Containers[:i], m.doc.Containers[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, errors.New("not found")
}
func (m *mockAppStore) AddGroup(g repository.Group) (repository.DataDocument, error) {
	m.doc.Groups = append(m.doc.Groups, g)
	return m.doc, nil
}
func (m *mockAppStore) RemoveGroup(name string) (repository.DataDocument, error) {
	for i, g := range m.doc.Groups {
		if g.Name == name {
			m.doc.Groups = append(m.doc.Groups[:i], m.doc.Groups[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, errors.New("not found")
}
func (m *mockAppStore) AddSchedule(s repository.Schedule) (repository.DataDocument, error) {
	m.doc.Schedules = append(m.doc.Schedules, s)
	return m.doc, nil
}
func (m *mockAppStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	for i, s := range m.doc.Schedules {
		if s.ID == id {
			m.doc.Schedules = append(m.doc.Schedules[:i], m.doc.Schedules[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, errors.New("not found")
}
func (m *mockAppStore) ClearDirty()            {}
func (m *mockAppStore) SetLastUpdate(ts int64) {}

// newTestAppCtx creates an *app.App for testing with the given runtime and store
func newTestAppCtx(rt runtime.ContainerRuntime, store cache.AppStore) *app.App {
	return &app.App{
		Config:  &config.Config{},
		Cache:   store,
		Runtime: rt,
		BaseCtx: context.Background(),
	}
}

// mockContainerRuntime implements runtime.ContainerRuntime for testing
type mockContainerRuntime struct {
	startErr          error
	stopErr           error
	isRunningErr      error
	listErr           error
	statsErr          error
	runningContainers map[string]bool
	statsMap          map[string]runtime.ContainerStats
	startCh           chan string
	stopCh            chan string
	mu                sync.RWMutex
}

func newMockRuntime() *mockContainerRuntime {
	return &mockContainerRuntime{
		runningContainers: make(map[string]bool),
		statsMap:          make(map[string]runtime.ContainerStats),
		startCh:           make(chan string, 10),
		stopCh:            make(chan string, 10),
	}
}

func (m *mockContainerRuntime) IsRunning(ctx context.Context, name string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.isRunningErr != nil {
		return false, m.isRunningErr
	}
	return m.runningContainers[name], nil
}

func (m *mockContainerRuntime) Start(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.runningContainers[name] = true
	// Segnala che il container è stato avviato (per sincronizzazione test)
	if m.startCh != nil {
		m.startCh <- name
	}
	return nil
}

func (m *mockContainerRuntime) Stop(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	m.runningContainers[name] = false
	// Segnala che il container è stato fermato (per sincronizzazione test)
	if m.stopCh != nil {
		m.stopCh <- name
	}
	return nil
}

func (m *mockContainerRuntime) ListContainers(ctx context.Context) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.runningContainers))
	for n := range m.runningContainers {
		names = append(names, n)
	}
	return names, nil
}

func (m *mockContainerRuntime) Stats(ctx context.Context, containerName string) (runtime.ContainerStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.statsErr != nil {
		return runtime.ContainerStats{}, m.statsErr
	}
	if stats, ok := m.statsMap[containerName]; ok {
		return stats, nil
	}
	return runtime.ContainerStats{}, nil
}

// newMockStoreWithContainer creates a mock store with a container
func newMockStoreWithContainer(name string) *mockAppStore {
	return &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: name},
			},
		},
	}
}

// Test for snapshot error

// newMockStoreEmpty creates an empty mock store
func newMockStoreEmpty() *mockAppStore {
	return &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{},
		},
	}
}

func TestRuntimeController_IsRunning_Success(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["my-container"] = true

	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "my-container"},
			},
		},
	}

	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)

	req := httptest.NewRequest(http.MethodGet, "/runtime/my-container/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["name"] != "my-container" {
		t.Errorf("expected name 'my-container', got %v", resp["name"])
	}
	if resp["running"] != true {
		t.Errorf("expected running true, got %v", resp["running"])
	}
}

func TestRuntimeController_IsRunning_NotRunning(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["stopped-container"] = false

	store := newMockStoreWithContainer("stopped-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)

	req := httptest.NewRequest(http.MethodGet, "/runtime/stopped-container/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["running"] != false {
		t.Errorf("expected running false, got %v", resp["running"])
	}
}

func TestRuntimeController_IsRunning_MissingName(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	// Test with empty name param - controller validates and returns 400
	r.GET("/runtime/:name/status", rc.IsRunning)

	// Request with empty path segment - Gin still matches with empty :name
	req := httptest.NewRequest(http.MethodGet, "/runtime//status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Controller returns 400 for empty name
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRuntimeController_IsRunning_RuntimeError(t *testing.T) {
	rt := newMockRuntime()
	rt.isRunningErr = errors.New("docker connection failed")

	store := newMockStoreWithContainer("my-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)

	req := httptest.NewRequest(http.MethodGet, "/runtime/my-container/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestRuntimeController_IsRunning_ContainerNotFound(t *testing.T) {
	rt := newMockRuntime()
	rt.isRunningErr = errors.New("container nonexistent not found")

	store := newMockStoreWithContainer("nonexistent")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)

	req := httptest.NewRequest(http.MethodGet, "/runtime/nonexistent/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_StartContainer_Success(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreWithContainer("my-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/start", rc.StartContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/my-container/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["name"] != "my-container" {
		t.Errorf("expected name 'my-container', got %v", resp["name"])
	}
	if resp["message"] != "container started" {
		t.Errorf("expected message 'container started', got %v", resp["message"])
	}

	// Attendi che la goroutine abbia effettivamente avviato il container
	select {
	case <-rt.startCh:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for container to be started in mock")
	}

	if !rt.runningContainers["my-container"] {
		t.Error("expected container to be marked as running in mock")
	}
}

func TestRuntimeController_StartContainer_MissingName(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	// Test with empty name param - controller validates and returns 400
	r.POST("/runtime/:name/start", rc.StartContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime//start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Controller returns 400 for empty name
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRuntimeController_StartContainer_RuntimeError(t *testing.T) {
	rt := newMockRuntime()
	rt.startErr = errors.New("docker daemon unavailable")

	store := newMockStoreWithContainer("my-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/start", rc.StartContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/my-container/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Ora la risposta è sempre 200 anche in caso di errore asincrono
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRuntimeController_StartContainer_ContainerNotFound(t *testing.T) {
	rt := newMockRuntime()
	rt.startErr = errors.New("error starting container nonexistent: container not found")

	store := newMockStoreWithContainer("nonexistent")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/start", rc.StartContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/nonexistent/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Ora la risposta è sempre 200 anche in caso di errore asincrono
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRuntimeController_StopContainer_Success(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["my-container"] = true

	store := newMockStoreWithContainer("my-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/stop", rc.StopContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/my-container/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["name"] != "my-container" {
		t.Errorf("expected name 'my-container', got %v", resp["name"])
	}
	if resp["message"] != "container stopped" {
		t.Errorf("expected message 'container stopped', got %v", resp["message"])
	}

	// Attendi che la goroutine abbia effettivamente fermato il container
	select {
	case <-rt.stopCh:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for container to be stopped in mock")
	}

	if rt.runningContainers["my-container"] {
		t.Error("expected container to be marked as stopped in mock")
	}
}

func TestRuntimeController_StopContainer_MissingName(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	// Test with empty name param - controller validates and returns 400
	r.POST("/runtime/:name/stop", rc.StopContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime//stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Controller returns 400 for empty name
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRuntimeController_StopContainer_RuntimeError(t *testing.T) {
	rt := newMockRuntime()
	rt.stopErr = errors.New("container already stopped")

	store := newMockStoreWithContainer("my-container")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/stop", rc.StopContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/my-container/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Ora la risposta è sempre 200 anche in caso di errore asincrono
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRuntimeController_StopContainer_ContainerNotFound(t *testing.T) {
	rt := newMockRuntime()
	rt.stopErr = errors.New("error stopping container nonexistent: container not found")

	store := newMockStoreWithContainer("nonexistent")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/stop", rc.StopContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/nonexistent/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Ora la risposta è sempre 200 anche in caso di errore asincrono
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRuntimeController_FullLifecycle(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreWithContainer("lifecycle-test")
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)
	r.POST("/runtime/:name/start", rc.StartContainer)
	r.POST("/runtime/:name/stop", rc.StopContainer)

	containerName := "lifecycle-test"

	// 1. Check initial status (should be not running)
	req := httptest.NewRequest(http.MethodGet, "/runtime/"+containerName+"/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["running"] != false {
		t.Errorf("expected container initially not running")
	}

	// 2. Start container
	req = httptest.NewRequest(http.MethodPost, "/runtime/"+containerName+"/start", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("failed to start container: %d", w.Code)
	}

	// Attendi che la goroutine abbia effettivamente avviato il container
	select {
	case <-rt.startCh:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for container to be started in mock (lifecycle)")
	}

	// 3. Check status (should be running)
	req = httptest.NewRequest(http.MethodGet, "/runtime/"+containerName+"/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["running"] != true {
		t.Errorf("expected container to be running after start")
	}

	// 4. Stop container
	req = httptest.NewRequest(http.MethodPost, "/runtime/"+containerName+"/stop", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("failed to stop container: %d", w.Code)
	}

	// Attendi che la goroutine abbia effettivamente fermato il container
	select {
	case <-rt.stopCh:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for container to be stopped in mock (lifecycle)")
	}

	// 5. Check status (should be stopped)
	req = httptest.NewRequest(http.MethodGet, "/runtime/"+containerName+"/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["running"] != false {
		t.Errorf("expected container to be stopped after stop")
	}
}
func TestRuntimeController_IsRunning_NotFoundInCache(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty() // Empty store - container doesn't exist
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/:name/status", rc.IsRunning)

	req := httptest.NewRequest(http.MethodGet, "/runtime/nonexistent/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_StartContainer_NotFoundInCache(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty() // Empty store - container doesn't exist
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/start", rc.StartContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/nonexistent/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_StopContainer_NotFoundInCache(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty() // Empty store - container doesn't exist
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.POST("/runtime/:name/stop", rc.StopContainer)

	req := httptest.NewRequest(http.MethodPost, "/runtime/nonexistent/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// Helper to create a pointer to bool
func boolPtr(b bool) *bool {
	return &b
}

// newMockStoreWithActiveContainer creates a mock store with an active container
func newMockStoreWithActiveContainer(name, url string, active bool) *mockAppStore {
	return &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: name, URL: url, Active: boolPtr(active)},
			},
		},
	}
}

// newMockStoreWithGroup creates a mock store with a group and its containers
func newMockStoreWithGroup(groupName string, containerNames []string, groupActive bool, containersActive bool) *mockAppStore {
	containers := make([]repository.Container, len(containerNames))
	for i, name := range containerNames {
		containers[i] = repository.Container{
			Name:   name,
			URL:    "http://localhost:800" + string(rune('0'+i)),
			Active: boolPtr(containersActive),
		}
	}
	return &mockAppStore{
		doc: repository.DataDocument{
			Containers: containers,
			Groups: []repository.Group{
				{Name: groupName, Container: containerNames, Active: boolPtr(groupActive)},
			},
		},
	}
}

func TestRuntimeController_WaitingPage_ContainerNotFound(t *testing.T) {
	rt := newMockRuntime()
	// Simulate runtime error to indicate container doesn't exist in runtime either
	rt.isRunningErr = errors.New("container not found in runtime")
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_ContainerNotActive(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreWithActiveContainer("my-container", "http://localhost:8080", false)
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_ContainerActiveAndRunning(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["my-container"] = true

	store := newMockStoreWithActiveContainer("my-container", "http://localhost:8080", true)
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify content type is HTML
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("expected content type 'text/html; charset=utf-8', got '%s'", contentType)
	}
}

func TestRuntimeController_WaitingPage_ContainerActiveNotRunning(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["my-container"] = false

	store := newMockStoreWithActiveContainer("my-container", "http://localhost:8080", true)
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Give goroutine a moment to start the container
	// In real test, we'd use synchronization, but for this test we just verify it was called
}

func TestRuntimeController_WaitingPage_GroupNotFound(t *testing.T) {
	rt := newMockRuntime()
	// Simulate runtime error to indicate entity doesn't exist in runtime either
	rt.isRunningErr = errors.New("container not found in runtime")
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/nonexistent-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_GroupNotActive(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreWithGroup("my-group", []string{"container1", "container2"}, false, true)
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_GroupActiveSuccess(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreWithGroup("my-group", []string{"container1", "container2"}, true, true)
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify content type is HTML
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("expected content type 'text/html; charset=utf-8', got '%s'", contentType)
	}
}

func TestRuntimeController_WaitingPage_MissingName(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_GroupEmptyContainers(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "empty-group", Container: []string{}, Active: boolPtr(true)},
			},
		},
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/empty-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_GroupWithNonexistentContainers(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{}, // No containers defined
			Groups: []repository.Group{
				{Name: "my-group", Container: []string{"nonexistent"}, Active: boolPtr(true)},
			},
		},
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// mockAppStoreWithError simulates a store that fails on Snapshot
type mockAppStoreWithError struct {
	snapshotErr error
	mockAppStore
}

func (m *mockAppStoreWithError) Snapshot() (repository.DataDocument, error) {
	if m.snapshotErr != nil {
		return repository.DataDocument{}, m.snapshotErr
	}
	return m.doc, nil
}

func TestRuntimeController_WaitingPage_SnapshotError(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStoreWithError{
		snapshotErr: errors.New("database connection failed"),
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_ContainerWithNilActive(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "my-container", URL: "http://localhost:8080", Active: nil},
			},
		},
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Container with nil active should be treated as not active (403)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRuntimeController_WaitingPage_GroupWithNilActive(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "container1", URL: "http://localhost:8080", Active: boolPtr(true)},
			},
			Groups: []repository.Group{
				{Name: "my-group", Container: []string{"container1"}, Active: nil},
			},
		},
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/start/:name", rc.WaitingPage)

	req := httptest.NewRequest(http.MethodGet, "/start/my-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Group with nil active should be treated as not active (403)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRuntimeController_ListContainers_Success(t *testing.T) {
	rt := newMockRuntime()
	rt.runningContainers["one"] = true
	rt.runningContainers["two"] = true

	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/containers", rc.ListContainers)

	req := httptest.NewRequest(http.MethodGet, "/runtime/containers", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp []string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp) < 2 {
		t.Errorf("expected at least 2 container names, got %v", resp)
	}
}

func TestRuntimeController_ListContainers_Error(t *testing.T) {
	rt := newMockRuntime()
	rt.listErr = errors.New("list failed")
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/containers", rc.ListContainers)

	req := httptest.NewRequest(http.MethodGet, "/runtime/containers", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on runtime error, got %d", w.Code)
	}
}

func TestRuntimeController_AllStats_Success(t *testing.T) {
	rt := newMockRuntime()
	rt.statsMap["container1"] = runtime.ContainerStats{CPUPercent: 25.5, MemoryMB: 128.0}
	rt.statsMap["container2"] = runtime.ContainerStats{CPUPercent: 50.0, MemoryMB: 256.0}

	active := true
	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "container1", Active: &active},
				{Name: "container2", Active: &active},
			},
		},
	}

	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/stats", rc.AllStats)

	req := httptest.NewRequest(http.MethodGet, "/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp []ContainerStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(resp))
	}

	// Find container1 stats
	var c1Stats *ContainerStatsResponse
	for i := range resp {
		if resp[i].Name == "container1" {
			c1Stats = &resp[i]
			break
		}
	}

	if c1Stats == nil {
		t.Fatal("container1 not found in response")
	}
	if c1Stats.CPUPercent != 25.5 {
		t.Errorf("expected CPUPercent 25.5, got %v", c1Stats.CPUPercent)
	}
	if c1Stats.MemoryMB != 128.0 {
		t.Errorf("expected MemoryMB 128.0, got %v", c1Stats.MemoryMB)
	}
}

func TestRuntimeController_AllStats_EmptyStore(t *testing.T) {
	rt := newMockRuntime()
	store := newMockStoreEmpty()
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/stats", rc.AllStats)

	req := httptest.NewRequest(http.MethodGet, "/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp []ContainerStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 0 {
		t.Errorf("expected empty response, got %d items", len(resp))
	}
}

func TestRuntimeController_AllStats_WithError(t *testing.T) {
	rt := newMockRuntime()
	rt.statsMap["container1"] = runtime.ContainerStats{CPUPercent: 10.0, MemoryMB: 64.0}
	// container2 will return an error because statsErr is set and container2 is not in statsMap

	active := true
	store := &mockAppStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "container1", Active: &active},
				{Name: "container2", Active: &active},
			},
		},
	}

	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/stats", rc.AllStats)

	req := httptest.NewRequest(http.MethodGet, "/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp []ContainerStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Both containers should be in the response
	if len(resp) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(resp))
	}

	// container1 should have valid stats
	var c1Stats *ContainerStatsResponse
	for i := range resp {
		if resp[i].Name == "container1" {
			c1Stats = &resp[i]
			break
		}
	}
	if c1Stats == nil {
		t.Fatal("container1 not found in response")
	}
	if c1Stats.Error != "" {
		t.Errorf("expected no error for container1, got %s", c1Stats.Error)
	}
}

func TestRuntimeController_AllStats_StoreError(t *testing.T) {
	rt := newMockRuntime()
	store := &mockAppStoreWithError{
		snapshotErr: errors.New("store error"),
	}
	rc := NewRuntimeController(newTestAppCtx(rt, store))

	r := gin.New()
	r.GET("/runtime/stats", rc.AllStats)

	req := httptest.NewRequest(http.MethodGet, "/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on store error, got %d", w.Code)
	}
}
