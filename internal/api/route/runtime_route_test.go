package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// mockContainerRuntime implements runtime.ContainerRuntime for testing
type mockContainerRuntime struct {
	statsCtxUsed context.Context
	statsDelay   time.Duration
	mu           sync.RWMutex
}

func (m *mockContainerRuntime) IsRunning(ctx context.Context, name string) (bool, error) {
	return true, nil
}
func (m *mockContainerRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *mockContainerRuntime) Stop(ctx context.Context, name string) error  { return nil }
func (m *mockContainerRuntime) ListContainers(ctx context.Context) ([]string, error) {
	return []string{"test-container"}, nil
}
func (m *mockContainerRuntime) Stats(ctx context.Context, containerName string) (runtime.ContainerStats, error) {
	m.mu.Lock()
	m.statsCtxUsed = ctx
	delay := m.statsDelay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
			return runtime.ContainerStats{CPUPercent: 10.0, MemoryMB: 100.0}, nil
		case <-ctx.Done():
			return runtime.ContainerStats{}, ctx.Err()
		}
	}
	return runtime.ContainerStats{CPUPercent: 10.0, MemoryMB: 100.0}, nil
}

// mockAppStore implements cache.AppStore for testing (minimal, no-op implementations)
type mockAppStore struct{}

func (m *mockAppStore) GetLastUpdate() int64 { return 0 }
func (m *mockAppStore) IsDirty() bool        { return false }
func (m *mockAppStore) Snapshot() (repository.DataDocument, error) {
	doc := repository.DataDocument{}
	active := true
	doc.Containers = []repository.Container{{Name: "test-container", FriendlyName: "test-container", URL: "http://example.local", Active: &active}}
	return doc, nil
}
func (m *mockAppStore) Replace(doc repository.DataDocument) error { return nil }

func (m *mockAppStore) AddContainer(container repository.Container) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}
func (m *mockAppStore) RemoveContainer(name string) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}

func (m *mockAppStore) AddGroup(group repository.Group) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}
func (m *mockAppStore) RemoveGroup(name string) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}

func (m *mockAppStore) AddSchedule(schedule repository.Schedule) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}
func (m *mockAppStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}

func (m *mockAppStore) ClearDirty()            {}
func (m *mockAppStore) SetLastUpdate(ts int64) {}

func TestRuntimeRoute_StatsEndpointHasLongerTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRT := &mockContainerRuntime{statsDelay: 500 * time.Millisecond}
	mockStore := &mockAppStore{}

	r := gin.New()
	group := r.Group("/api")

	cfg := &config.Config{Server: config.ServerConfig{ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, RequestTimeout: 100 * time.Millisecond}}

	appCtx := &app.App{Config: cfg, Cache: mockStore, Runtime: mockRT, BaseCtx: context.Background()}
	NewRuntimeRouter(appCtx, group)

	req, _ := http.NewRequest(http.MethodGet, "/api/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestRuntimeRoute_DefaultTimeoutAppliedToOtherRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRT := &mockContainerRuntime{}
	mockStore := &mockAppStore{}

	r := gin.New()
	group := r.Group("/api")

	cfg := &config.Config{Server: config.ServerConfig{RequestTimeout: 50 * time.Millisecond, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}}
	appCtx := &app.App{Config: cfg, Cache: mockStore, Runtime: mockRT, BaseCtx: context.Background()}
	NewRuntimeRouter(appCtx, group)

	req, _ := http.NewRequest(http.MethodGet, "/api/runtime/containers", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRuntimeRoute_StatsTimeoutIsIndependentFromDefaultTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRT := &mockContainerRuntime{statsDelay: 200 * time.Millisecond}
	mockStore := &mockAppStore{}

	r := gin.New()
	group := r.Group("/api")

	cfg := &config.Config{Server: config.ServerConfig{RequestTimeout: 50 * time.Millisecond, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}}
	appCtx := &app.App{Config: cfg, Cache: mockStore, Runtime: mockRT, BaseCtx: context.Background()}
	NewRuntimeRouter(appCtx, group)

	req, _ := http.NewRequest(http.MethodGet, "/api/runtime/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("stats endpoint should have succeeded with its own timeout, got status %d, body: %s", w.Code, w.Body.String())
	}
}

func TestRuntimeRoute_StatsContextDeadlineIsApproximately30Seconds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedDeadline time.Time
	var hasDeadline bool

	mockRT := &mockContainerRuntime{}
	mockStore := &mockAppStore{}

	r := gin.New()
	group := r.Group("/api")

	cfg := &config.Config{Server: config.ServerConfig{RequestTimeout: 100 * time.Millisecond, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}}
	appCtx := &app.App{Config: cfg, Cache: mockStore, Runtime: mockRT, BaseCtx: context.Background()}
	NewRuntimeRouter(appCtx, group)

	mockRT2 := &mockContainerRuntime{}

	r2 := gin.New()
	r2.GET("/test-stats", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	r3 := gin.New()
	group3 := r3.Group("/api")
	appCtx3 := &app.App{Config: cfg, Cache: mockStore, Runtime: mockRT2, BaseCtx: context.Background()}
	NewRuntimeRouter(appCtx3, group3)

	req, _ := http.NewRequest(http.MethodGet, "/api/runtime/stats", nil)
	w := httptest.NewRecorder()

	startTime := time.Now()
	r3.ServeHTTP(w, req)

	mockRT2.mu.Lock()
	if mockRT2.statsCtxUsed != nil {
		capturedDeadline, hasDeadline = mockRT2.statsCtxUsed.Deadline()
	}
	mockRT2.mu.Unlock()

	if !hasDeadline {
		t.Error("expected stats context to have a deadline")
		return
	}

	expectedDeadline := startTime.Add(30 * time.Second)
	tolerance := 2 * time.Second

	if capturedDeadline.Before(expectedDeadline.Add(-tolerance)) || capturedDeadline.After(expectedDeadline.Add(tolerance)) {
		t.Errorf("stats context deadline should be ~30s, got deadline at %v (expected around %v)", capturedDeadline, expectedDeadline)
	}
}
