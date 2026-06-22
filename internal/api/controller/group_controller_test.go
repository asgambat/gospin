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

// mockGroupStore implements cache.GroupStore for testing
type mockGroupStore struct {
	addErr    error
	removeErr error
	doc       repository.DataDocument
}

func (m *mockGroupStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, nil
}

func (m *mockGroupStore) AddGroup(g repository.Group) (repository.DataDocument, error) {
	if m.addErr != nil {
		return repository.DataDocument{}, m.addErr
	}
	m.doc.Groups = append(m.doc.Groups, g)
	return m.doc, nil
}

func (m *mockGroupStore) RemoveGroup(name string) (repository.DataDocument, error) {
	if m.removeErr != nil {
		return repository.DataDocument{}, m.removeErr
	}
	for i, g := range m.doc.Groups {
		if g.Name == name {
			m.doc.Groups = append(m.doc.Groups[:i], m.doc.Groups[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, cache.ErrGroupNotFound
}

// mockGroupRuntime implements runtime.ContainerRuntime for testing
type mockGroupRuntime struct {
	startErr error
	stopErr  error
}

func (m *mockGroupRuntime) IsRunning(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *mockGroupRuntime) Start(_ context.Context, _ string) error {
	return m.startErr
}

func (m *mockGroupRuntime) Stop(_ context.Context, _ string) error {
	return m.stopErr
}

func (m *mockGroupRuntime) ListContainers(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockGroupRuntime) Stats(_ context.Context, _ string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func TestGroupController_AllGroups(t *testing.T) {
	active := true
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1"},
			},
			Groups: []repository.Group{
				{Name: "group1", Container: []string{"c1", "c2"}, Active: &active},
				{Name: "group2", Container: []string{"c3"}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}

	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.GET("/groups", gc.AllGroups)

	req := httptest.NewRequest(http.MethodGet, "/groups", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var groups []repository.Group
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
	// After sanitization: group1 should keep only c1, group2 should have no containers
	if len(groups[0].Container) != 1 || groups[0].Container[0] != "c1" {
		t.Errorf("expected group1 to contain only c1, got %v", groups[0].Container)
	}
	if len(groups[1].Container) != 0 {
		t.Errorf("expected group2 to have no containers after sanitization, got %v", groups[1].Container)
	}
}

func TestGroupController_CreateOrUpdateGroup_Valid(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{},
		},
	}
	rt := &mockGroupRuntime{}

	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group", gc.CreateOrUpdateGroup)

	active := true
	group := repository.Group{
		Name:      "new-group",
		Container: []string{"c1", "c2"},
		Active:    &active,
	}
	body, _ := json.Marshal(group)

	req := httptest.NewRequest(http.MethodPost, "/group", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGroupController_CreateOrUpdateGroup_InvalidPayload(t *testing.T) {
	store := &mockGroupStore{}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group", gc.CreateOrUpdateGroup)

	req := httptest.NewRequest(http.MethodPost, "/group", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGroupController_CreateOrUpdateGroup_ValidationError(t *testing.T) {
	store := &mockGroupStore{}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group", gc.CreateOrUpdateGroup)

	// Missing required fields (name, active)
	group := map[string]any{
		"container": []string{"c1"},
	}
	body, _ := json.Marshal(group)

	req := httptest.NewRequest(http.MethodPost, "/group", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGroupController_CreateOrUpdateGroup_StoreError(t *testing.T) {
	store := &mockGroupStore{
		addErr: errors.New("store error"),
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group", gc.CreateOrUpdateGroup)

	active := true
	group := repository.Group{
		Name:      "test",
		Container: []string{"c1"},
		Active:    &active,
	}
	body, _ := json.Marshal(group)

	req := httptest.NewRequest(http.MethodPost, "/group", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestGroupController_DeleteGroup_Success(t *testing.T) {
	active := true
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "to-delete", Container: []string{}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.DELETE("/group/:name", gc.DeleteGroup)

	req := httptest.NewRequest(http.MethodDelete, "/group/to-delete", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestGroupController_DeleteGroup_NotFound(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.DELETE("/group/:name", gc.DeleteGroup)

	req := httptest.NewRequest(http.MethodDelete, "/group/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGroupController_DeleteGroup_MissingName(t *testing.T) {
	store := &mockGroupStore{}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.DELETE("/group/", gc.DeleteGroup)

	req := httptest.NewRequest(http.MethodDelete, "/group/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGroupController_StartGroup_Success(t *testing.T) {
	active := true
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "test-group", Container: []string{"c1", "c2"}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/test-group/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGroupController_StartGroup_EmptyName(t *testing.T) {
	active := true
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "", Container: []string{"c1"}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group//start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Controller validates name param and returns 400 for empty string
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGroupController_StartGroup_NotFound(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/nonexistent/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGroupController_StartGroup_InactiveGroup(t *testing.T) {
	active := false
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "inactive-group", Container: []string{"c1"}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/inactive-group/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestGroupController_StartGroup_NilActiveGroup(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "nil-active-group", Container: []string{"c1"}, Active: nil},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/nil-active-group/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestGroupController_StopGroup_Success(t *testing.T) {
	active := true
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "test-group", Container: []string{"c1", "c2"}, Active: &active},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/stop", gc.StopGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/test-group/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGroupController_StopGroup_EmptyName(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{
				{Name: "", Container: []string{"c1"}},
			},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/stop", gc.StopGroup)

	req := httptest.NewRequest(http.MethodPost, "/group//stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Controller validates name param and returns 400 for empty string
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestGroupController_StopGroup_NotFound(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{},
		},
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/stop", gc.StopGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/nonexistent/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGroupController_DeleteGroup_StoreError(t *testing.T) {
	store := &mockGroupStore{
		doc: repository.DataDocument{
			Groups: []repository.Group{},
		},
		removeErr: errors.New("store error"),
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.DELETE("/group/:name", gc.DeleteGroup)

	req := httptest.NewRequest(http.MethodDelete, "/group/some-group", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// mockGroupStoreWithSnapshotError implements cache.GroupStore for testing snapshot errors
type mockGroupStoreWithSnapshotError struct {
	snapshotErr error
	mockGroupStore
}

func (m *mockGroupStoreWithSnapshotError) Snapshot() (repository.DataDocument, error) {
	if m.snapshotErr != nil {
		return repository.DataDocument{}, m.snapshotErr
	}
	return m.mockGroupStore.Snapshot()
}

func TestGroupController_StartGroup_SnapshotError(t *testing.T) {
	store := &mockGroupStoreWithSnapshotError{
		snapshotErr: errors.New("snapshot error"),
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/start", gc.StartGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/test-group/start", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestGroupController_StopGroup_SnapshotError(t *testing.T) {
	store := &mockGroupStoreWithSnapshotError{
		snapshotErr: errors.New("snapshot error"),
	}
	rt := &mockGroupRuntime{}
	gc := NewGroupController(context.Background(), store, rt)

	r := gin.New()
	r.POST("/group/:name/stop", gc.StopGroup)

	req := httptest.NewRequest(http.MethodPost, "/group/test-group/stop", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}
