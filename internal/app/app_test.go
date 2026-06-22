package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// mockRepository implements repository.Repository for testing
type mockRepository struct {
	watcherErr     error
	saveErr        error
	doc            repository.DataDocument
	watcherStarted bool
}

func (m *mockRepository) Load(ctx context.Context) (*repository.DataDocument, error) {
	return &m.doc, nil
}

func (m *mockRepository) Save(ctx context.Context, doc *repository.DataDocument) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if doc != nil {
		m.doc = *doc
	}
	return nil
}

func (m *mockRepository) StartWatcher(ctx context.Context, store repository.CacheStore) error {
	if m.watcherErr != nil {
		return m.watcherErr
	}
	m.watcherStarted = true
	return nil
}

// mockAppStore implements cache.AppStore for testing
type mockAppStore struct {
	doc        repository.DataDocument
	dirty      bool
	lastUpdate int64
}

func (m *mockAppStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, nil
}

func (m *mockAppStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	m.dirty = true
	m.doc.Containers = append(m.doc.Containers, c)
	return m.doc, nil
}

func (m *mockAppStore) RemoveContainer(name string) (repository.DataDocument, error) {
	m.dirty = true
	return m.doc, nil
}

func (m *mockAppStore) AddGroup(g repository.Group) (repository.DataDocument, error) {
	m.dirty = true
	m.doc.Groups = append(m.doc.Groups, g)
	return m.doc, nil
}

func (m *mockAppStore) RemoveGroup(name string) (repository.DataDocument, error) {
	m.dirty = true
	return m.doc, nil
}

func (m *mockAppStore) AddSchedule(s repository.Schedule) (repository.DataDocument, error) {
	m.dirty = true
	m.doc.Schedules = append(m.doc.Schedules, s)
	return m.doc, nil
}

func (m *mockAppStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	m.dirty = true
	return m.doc, nil
}

func (m *mockAppStore) Replace(doc repository.DataDocument) error {
	m.doc = doc
	m.dirty = false
	return nil
}

func (m *mockAppStore) IsDirty() bool {
	return m.dirty
}

func (m *mockAppStore) ClearDirty() {
	m.dirty = false
}

func (m *mockAppStore) GetLastUpdate() int64 {
	return m.lastUpdate
}

func (m *mockAppStore) SetLastUpdate(ts int64) {
	m.lastUpdate = ts
}

// mockContainerRuntime implements runtime.ContainerRuntime for testing
type mockRuntimeForApp struct {
	runningContainers map[string]bool
}

func newMockRuntimeForApp() *mockRuntimeForApp {
	return &mockRuntimeForApp{
		runningContainers: make(map[string]bool),
	}
}

func (m *mockRuntimeForApp) IsRunning(ctx context.Context, name string) (bool, error) {
	return m.runningContainers[name], nil
}

func (m *mockRuntimeForApp) Start(ctx context.Context, name string) error {
	m.runningContainers[name] = true
	return nil
}

func (m *mockRuntimeForApp) Stop(ctx context.Context, name string) error {
	m.runningContainers[name] = false
	return nil
}

func (m *mockRuntimeForApp) ListContainers(ctx context.Context) ([]string, error) {
	names := make([]string, 0, len(m.runningContainers))
	for n := range m.runningContainers {
		names = append(names, n)
	}
	return names, nil
}

func (m *mockRuntimeForApp) Stats(ctx context.Context, containerName string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func TestNew_Success(t *testing.T) {
	cfg := &config.Config{}
	repo := &mockRepository{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, err := New(cfg, repo, store, rt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if app == nil {
		t.Fatal("expected non-nil app")
	}

	if app.Config != cfg {
		t.Error("config not set correctly")
	}
	if app.Repo == nil {
		t.Error("repo should not be nil")
	}
	if app.Cache == nil {
		t.Error("cache should not be nil")
	}
	if app.Runtime == nil {
		t.Error("runtime should not be nil")
	}
	if app.BaseCtx == nil {
		t.Error("BaseCtx should not be nil")
	}
	if app.Cancel == nil {
		t.Error("Cancel should not be nil")
	}
}

func TestNew_NilConfig(t *testing.T) {
	repo := &mockRepository{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, err := New(nil, repo, store, rt)
	if err == nil {
		t.Error("expected error for nil config")
	}
	if app != nil {
		t.Error("expected nil app on error")
	}
	if !errors.Is(err, err) || err.Error() != "config is nil" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_NilRepo(t *testing.T) {
	cfg := &config.Config{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, err := New(cfg, nil, store, rt)
	if err == nil {
		t.Error("expected error for nil repo")
	}
	if app != nil {
		t.Error("expected nil app on error")
	}
}

func TestNew_NilStore(t *testing.T) {
	cfg := &config.Config{}
	repo := &mockRepository{}
	rt := newMockRuntimeForApp()

	app, err := New(cfg, repo, nil, rt)
	if err == nil {
		t.Error("expected error for nil store")
	}
	if app != nil {
		t.Error("expected nil app on error")
	}
}

func TestNew_NilRuntime(t *testing.T) {
	cfg := &config.Config{}
	repo := &mockRepository{}
	store := &mockAppStore{}

	app, err := New(cfg, repo, store, nil)
	if err == nil {
		t.Error("expected error for nil runtime")
	}
	if app != nil {
		t.Error("expected nil app on error")
	}
}

func TestApp_Shutdown(t *testing.T) {
	cfg := &config.Config{}
	repo := &mockRepository{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, _ := New(cfg, repo, store, rt)

	// Verify context is not done before shutdown
	select {
	case <-app.BaseCtx.Done():
		t.Error("context should not be done before shutdown")
	default:
		// OK
	}

	// Shutdown
	app.Shutdown()

	// Verify context is done after shutdown
	select {
	case <-app.BaseCtx.Done():
		// OK
	default:
		t.Error("context should be done after shutdown")
	}
}

func TestApp_Shutdown_Nil(t *testing.T) {
	// Should not panic
	var app *App
	app.Shutdown()
}

func TestApp_Shutdown_NilCancel(t *testing.T) {
	// Should not panic
	app := &App{
		Cancel: nil,
	}
	app.Shutdown()
}

func TestApp_ContextCancellation(t *testing.T) {
	cfg := &config.Config{}
	repo := &mockRepository{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, _ := New(cfg, repo, store, rt)

	// Create a goroutine that waits on the context
	done := make(chan bool, 1)
	go func() {
		<-app.BaseCtx.Done()
		done <- true
	}()

	// Shutdown should trigger context cancellation
	app.Shutdown()

	// Wait for goroutine to receive cancellation (with timeout)
	select {
	case <-done:
		// OK - goroutine received cancellation
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine should have received cancellation within timeout")
	}
}

func TestApp_StartWatchers_Success(t *testing.T) {
	cfg := &config.Config{
		Data: config.DataConfig{PersistInterval: 10, SchedulingEnabled: false},
	}
	repo := &mockRepository{}
	store := &mockAppStore{}
	rt := newMockRuntimeForApp()

	app, err := New(cfg, repo, store, rt)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	app.StartWatchers()

	if !repo.watcherStarted {
		t.Error("expected repo watcher to be started")
	}

	if app.persistDone == nil {
		t.Error("expected persistDone channel to be set after StartWatchers")
	}

	// Shutdown to clean up scheduler goroutine
	app.Shutdown()
}
