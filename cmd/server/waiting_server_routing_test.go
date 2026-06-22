package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockContainerStore implements cache.AppStore for testing purposes.
type mockContainerStore struct {
	doc repository.DataDocument
}

func (m *mockContainerStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, nil
}

func (m *mockContainerStore) AddContainer(container repository.Container) (repository.DataDocument, error) {
	m.doc.Containers = append(m.doc.Containers, container)
	return m.doc, nil
}

func (m *mockContainerStore) RemoveContainer(name string) (repository.DataDocument, error) {
	for i, c := range m.doc.Containers {
		if c.Name == name {
			m.doc.Containers = append(m.doc.Containers[:i], m.doc.Containers[i+1:]...)
			break
		}
	}
	return m.doc, nil
}

func (m *mockContainerStore) GetLastUpdate() int64 {
	return time.Now().Unix()
}

func (m *mockContainerStore) IsDirty() bool {
	return false
}

func (m *mockContainerStore) Replace(doc repository.DataDocument) error {
	m.doc = doc
	return nil
}

func (m *mockContainerStore) AddGroup(group repository.Group) (repository.DataDocument, error) {
	m.doc.Groups = append(m.doc.Groups, group)
	return m.doc, nil
}

func (m *mockContainerStore) RemoveGroup(name string) (repository.DataDocument, error) {
	for i, g := range m.doc.Groups {
		if g.Name == name {
			m.doc.Groups = append(m.doc.Groups[:i], m.doc.Groups[i+1:]...)
			break
		}
	}
	return m.doc, nil
}

func (m *mockContainerStore) AddSchedule(schedule repository.Schedule) (repository.DataDocument, error) {
	m.doc.Schedules = append(m.doc.Schedules, schedule)
	return m.doc, nil
}

func (m *mockContainerStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	for i, s := range m.doc.Schedules {
		if s.ID == id {
			m.doc.Schedules = append(m.doc.Schedules[:i], m.doc.Schedules[i+1:]...)
			break
		}
	}
	return m.doc, nil
}

func (m *mockContainerStore) ClearDirty() {}

func (m *mockContainerStore) SetLastUpdate(ts int64) {}

// Verify mockContainerStore implements cache.AppStore
var _ cache.AppStore = (*mockContainerStore)(nil)

// newTestAppCtx creates an *app.App for testing with the given runtime and store
func newTestAppCtx(rt runtime.ContainerRuntime, store cache.AppStore) *app.App {
	return &app.App{
		Config:  &config.Config{},
		Cache:   store,
		Runtime: rt,
		BaseCtx: context.Background(),
	}
}

// mockContainerRuntime implements runtime.ContainerRuntime for testing purposes.
type mockContainerRuntime struct {
	runningContainers map[string]bool
}

func newMockRuntime() *mockContainerRuntime {
	return &mockContainerRuntime{
		runningContainers: make(map[string]bool),
	}
}

func (m *mockContainerRuntime) IsRunning(_ context.Context, containerName string) (bool, error) {
	running, exists := m.runningContainers[containerName]
	if !exists {
		return false, nil
	}
	return running, nil
}

func (m *mockContainerRuntime) Start(_ context.Context, containerName string) error {
	m.runningContainers[containerName] = true
	return nil
}

func (m *mockContainerRuntime) Stop(_ context.Context, containerName string) error {
	m.runningContainers[containerName] = false
	return nil
}

func (m *mockContainerRuntime) ListContainers(_ context.Context) ([]string, error) {
	var names []string
	for name := range m.runningContainers {
		names = append(names, name)
	}
	return names, nil
}

func (m *mockContainerRuntime) Stats(_ context.Context, containerName string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

// Verify mockContainerRuntime implements runtime.ContainerRuntime
var _ runtime.ContainerRuntime = (*mockContainerRuntime)(nil)

func boolPtr(b bool) *bool {
	return &b
}

// newTestStore creates a mock store with a test container.
func newTestStore() *mockContainerStore {
	return &mockContainerStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{
					Name:         "Deluge",
					FriendlyName: "Deluge Torrent",
					URL:          "http://deluge.local:8112",
					Active:       boolPtr(true),
					Running:      boolPtr(false),
				},
			},
		},
	}
}

// setupWaitingServerRoutes configures the routes for the waiting server.
// Uses leading slash for consistency with Gin best practices.
func setupWaitingServerRoutes(r *gin.Engine, rc *controller.RuntimeController, cc *controller.ContainerController) {
	r.GET("/container/:name/ready", cc.Ready)
	r.GET("/:name", rc.WaitingPage)
}

// TestWaitingServerRouting_ContainerReady verifies that /container/:name/ready
// is handled by the Ready handler and returns JSON.
func TestWaitingServerRouting_ContainerReady(t *testing.T) {
	store := newTestStore()
	rt := newMockRuntime()
	rt.runningContainers["Deluge"] = false

	testApp := newTestAppCtx(rt, store)
	rc := controller.NewRuntimeController(testApp)
	cc := controller.NewContainerController(testApp.BaseCtx, testApp.Cache, testApp.Runtime, "", "")

	r := gin.New()
	setupWaitingServerRoutes(r, rc, cc)

	req := httptest.NewRequest(http.MethodGet, "/container/Deluge/ready", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	body := w.Body.String()

	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected JSON response from Ready handler, got Content-Type=%s", contentType)
	}

	if !strings.Contains(body, `"ready"`) {
		t.Errorf("expected response body to contain 'ready' field, got: %s", body)
	}

	t.Logf("/container/Deluge/ready responded with Content-Type=%s, body=%s", contentType, body)
}

// TestWaitingServerRouting_WaitingPage verifies that /:name route works correctly
// and serves the waiting page for a container.
func TestWaitingServerRouting_WaitingPage(t *testing.T) {
	store := newTestStore()
	rt := newMockRuntime()

	testApp := newTestAppCtx(rt, store)
	rc := controller.NewRuntimeController(testApp)
	cc := controller.NewContainerController(testApp.BaseCtx, testApp.Cache, testApp.Runtime, "", "")

	r := gin.New()
	setupWaitingServerRoutes(r, rc, cc)

	req := httptest.NewRequest(http.MethodGet, "/Deluge", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	body := w.Body.String()

	// WaitingPage serves HTML content
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected HTML response from WaitingPage handler, got Content-Type=%s", contentType)
	}

	t.Logf("/Deluge responded with Content-Type=%s, body length=%d", contentType, len(body))
}

// TestWaitingServerRouting_BothRoutesWork verifies that both routes work correctly
// in the same router configuration without conflict.
func TestWaitingServerRouting_BothRoutesWork(t *testing.T) {
	store := newTestStore()
	rt := newMockRuntime()
	rt.runningContainers["Deluge"] = false

	testApp := newTestAppCtx(rt, store)
	rc := controller.NewRuntimeController(testApp)
	cc := controller.NewContainerController(testApp.BaseCtx, testApp.Cache, testApp.Runtime, "", "")

	r := gin.New()
	setupWaitingServerRoutes(r, rc, cc)

	tests := []struct {
		name           string
		path           string
		expectedFields []string
		expectedJSON   bool
	}{
		{
			name:           "Ready endpoint returns JSON",
			path:           "/container/Deluge/ready",
			expectedJSON:   true,
			expectedFields: []string{`"ready"`},
		},
		{
			name:         "WaitingPage endpoint returns HTML",
			path:         "/Deluge",
			expectedJSON: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			contentType := w.Header().Get("Content-Type")
			body := w.Body.String()

			if tc.expectedJSON {
				if !strings.Contains(contentType, "application/json") {
					t.Errorf("path %s: expected JSON, got Content-Type=%s", tc.path, contentType)
				}
				for _, field := range tc.expectedFields {
					if !strings.Contains(body, field) {
						t.Errorf("path %s: expected body to contain %s, got: %s", tc.path, field, body)
					}
				}
			} else {
				if !strings.Contains(contentType, "text/html") {
					t.Errorf("path %s: expected HTML, got Content-Type=%s", tc.path, contentType)
				}
			}
		})
	}
}

// TestWaitingServerRouting_HandlerIsolation verifies that each route calls
// the correct handler using mock handlers.
func TestWaitingServerRouting_HandlerIsolation(t *testing.T) {
	var readyHandlerCalled bool
	var waitingPageCalled bool

	r := gin.New()

	r.GET("/container/:name/ready", func(c *gin.Context) {
		readyHandlerCalled = true
		c.JSON(http.StatusOK, gin.H{"ready": true, "handler": "Ready"})
	})

	r.GET("/:name", func(c *gin.Context) {
		waitingPageCalled = true
		c.String(http.StatusOK, "WaitingPage")
	})

	t.Run("Ready endpoint calls Ready handler", func(t *testing.T) {
		readyHandlerCalled = false
		waitingPageCalled = false

		req := httptest.NewRequest(http.MethodGet, "/container/Deluge/ready", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if !readyHandlerCalled {
			t.Error("Ready handler was not called for /container/Deluge/ready")
		}
		if waitingPageCalled {
			t.Error("WaitingPage handler should not be called for /container/Deluge/ready")
		}
	})

	t.Run("WaitingPage endpoint calls WaitingPage handler", func(t *testing.T) {
		readyHandlerCalled = false
		waitingPageCalled = false

		req := httptest.NewRequest(http.MethodGet, "/Deluge", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if !waitingPageCalled {
			t.Error("WaitingPage handler was not called for /Deluge")
		}
		if readyHandlerCalled {
			t.Error("Ready handler should not be called for /Deluge")
		}
	})
}
