package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// stubRuntime implements runtime.ContainerRuntime for route testing
type stubRuntime struct{}

func (s *stubRuntime) IsRunning(ctx context.Context, name string) (bool, error) { return false, nil }
func (s *stubRuntime) Start(ctx context.Context, name string) error             { return nil }
func (s *stubRuntime) Stop(ctx context.Context, name string) error              { return nil }
func (s *stubRuntime) ListContainers(ctx context.Context) ([]string, error)     { return nil, nil }
func (s *stubRuntime) Stats(ctx context.Context, name string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

// stubStore implements cache.AppStore for route testing
type stubStore struct {
	doc repository.DataDocument
}

func (s *stubStore) GetLastUpdate() int64                       { return 0 }
func (s *stubStore) IsDirty() bool                              { return false }
func (s *stubStore) Snapshot() (repository.DataDocument, error) { return s.doc, nil }
func (s *stubStore) Replace(doc repository.DataDocument) error  { return nil }
func (s *stubStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) RemoveContainer(name string) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) AddGroup(g repository.Group) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) RemoveGroup(name string) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) AddSchedule(sc repository.Schedule) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	return s.doc, nil
}
func (s *stubStore) ClearDirty()            {}
func (s *stubStore) SetLastUpdate(ts int64) {}

// testApp creates a minimal app.App for route setup testing
func testApp() *app.App {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:               8084,
			RequestTimeout:     1000 * time.Millisecond,
			CORSAllowedOrigins: "*",
			ReadTimeout:        30 * time.Second,
			WriteTimeout:       30 * time.Second,
		},
		Data: config.DataConfig{
			ActionsTimeoutSeconds: 30,
		},
	}
	return &app.App{
		Config:  cfg,
		Cache:   &stubStore{},
		Runtime: &stubRuntime{},
		BaseCtx: context.Background(),
	}
}

func TestSetupRoutes_HealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_HomepageEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/homepage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Homepage config file may not exist in test — returns 200 with defaults
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_ConfigurationEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/configuration", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_ContainersEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/containers", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_GroupsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/groups", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_SchedulesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSetupRoutes_ContainerPOST(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := SetupRoutes(testApp(), logger)

	req := httptest.NewRequest(http.MethodPost, "/container", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should get 400 due to invalid JSON body (empty)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for POST without body, got %d", w.Code)
	}
}
