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

	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

// cleanupStore is a minimal cache.ContainerStore for the orphan-cleanup tests.
type cleanupStore struct {
	doc         *repository.DataDocument
	snapshotErr error
	removeErr   error
	removed     []string
}

func (m *cleanupStore) document() repository.DataDocument {
	if m.doc == nil {
		return repository.DataDocument{}
	}
	return *m.doc
}

func (m *cleanupStore) Snapshot() (repository.DataDocument, error) {
	if m.snapshotErr != nil {
		return repository.DataDocument{}, m.snapshotErr
	}
	return m.document(), nil
}

func (m *cleanupStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	doc := m.document()
	doc.Containers = append(doc.Containers, c)
	m.doc = &doc
	return doc, nil
}

func (m *cleanupStore) RemoveContainer(name string) (repository.DataDocument, error) {
	if m.removeErr != nil {
		return repository.DataDocument{}, m.removeErr
	}
	doc := m.document()
	for i := range doc.Containers {
		if doc.Containers[i].Name == name {
			doc.Containers = append(doc.Containers[:i], doc.Containers[i+1:]...)
			m.doc = &doc
			m.removed = append(m.removed, name)
			return doc, nil
		}
	}
	return repository.DataDocument{}, errors.New("container not found")
}

// cleanupRuntime is a minimal runtime.ContainerRuntime listing a fixed set of names.
type cleanupRuntime struct {
	listErr error
	names   []string
}

func (m *cleanupRuntime) IsRunning(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *cleanupRuntime) Start(_ context.Context, _ string) error             { return nil }
func (m *cleanupRuntime) Stop(_ context.Context, _ string) error              { return nil }
func (m *cleanupRuntime) ListContainers(_ context.Context) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.names, nil
}
func (m *cleanupRuntime) Stats(_ context.Context, _ string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func newCleanupRouter(store *cleanupStore, rt *cleanupRuntime) *gin.Engine {
	cc := NewContainerController(context.Background(), store, rt, "", "")

	r := gin.New()
	r.GET("/containers/orphans", cc.ListOrphanContainers)
	r.POST("/containers/orphans/remove", cc.RemoveOrphanContainers)
	return r
}

func boolPtrCleanup(b bool) *bool { return &b }

func TestContainerController_ListOrphanContainers(t *testing.T) {
	store := &cleanupStore{
		doc: &repository.DataDocument{
			Containers: []repository.Container{
				{Name: "gone1", FriendlyName: "Gone 1", URL: "http://gone1.local", Active: boolPtrCleanup(true)},
				{Name: "alive", FriendlyName: "Alive", URL: "http://alive.local", Active: boolPtrCleanup(true)},
				{Name: "gone2", FriendlyName: "Gone 2", URL: "http://gone2.local", Active: boolPtrCleanup(true)},
			},
		},
	}
	rt := &cleanupRuntime{names: []string{"alive", "stopped-but-present"}}

	w := httptest.NewRecorder()
	newCleanupRouter(store, rt).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/orphans", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var orphans []repository.Container
	if err := json.Unmarshal(w.Body.Bytes(), &orphans); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphan containers, got %d: %s", len(orphans), w.Body.String())
	}
	if orphans[0].Name != "gone1" || orphans[1].Name != "gone2" {
		t.Errorf("expected registry order [gone1 gone2], got [%s %s]", orphans[0].Name, orphans[1].Name)
	}
	if orphans[0].FriendlyName != "Gone 1" {
		t.Errorf("expected the preview to carry the registry entry, got %q", orphans[0].FriendlyName)
	}
	if len(store.removed) != 0 {
		t.Errorf("preview must not delete anything, deleted: %v", store.removed)
	}
}

func TestContainerController_ListOrphanContainers_RuntimeError(t *testing.T) {
	store := &cleanupStore{}
	rt := &cleanupRuntime{listErr: errors.New("docker unavailable")}

	w := httptest.NewRecorder()
	newCleanupRouter(store, rt).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/orphans", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContainerController_ListOrphanContainers_SnapshotError(t *testing.T) {
	store := &cleanupStore{snapshotErr: errors.New("boom")}
	rt := &cleanupRuntime{}

	w := httptest.NewRecorder()
	newCleanupRouter(store, rt).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/orphans", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContainerController_RemoveOrphanContainers_RemovesOnlyMissing(t *testing.T) {
	store := &cleanupStore{
		doc: &repository.DataDocument{
			Containers: []repository.Container{
				{Name: "gone1", Active: boolPtrCleanup(true)},
				{Name: "alive", Active: boolPtrCleanup(true)},
				{Name: "gone2", Active: boolPtrCleanup(true)},
			},
		},
	}
	rt := &cleanupRuntime{names: []string{"alive"}}

	body, _ := json.Marshal(map[string][]string{"names": {"gone1", "alive", "unknown", "gone2", "gone1"}})
	req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newCleanupRouter(store, rt).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp removeOrphansResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Removed) != 2 || resp.Removed[0] != "gone1" || resp.Removed[1] != "gone2" {
		t.Errorf("expected removed [gone1 gone2], got %v", resp.Removed)
	}
	if len(resp.Skipped) != 2 {
		t.Errorf("expected alive and unknown to be skipped, got %v", resp.Skipped)
	}
	if len(store.removed) != 2 {
		t.Errorf("expected 2 deletions in the store, got %v", store.removed)
	}
	remaining := store.document().Containers
	if len(remaining) != 1 || remaining[0].Name != "alive" {
		t.Errorf("expected only 'alive' to remain, got %+v", remaining)
	}
}

func TestContainerController_RemoveOrphanContainers_RejectsEmptyNames(t *testing.T) {
	store := &cleanupStore{}
	rt := &cleanupRuntime{}

	for _, payload := range []string{`{"names":[]}`, `{"names":["  ",""]}`, `{}`} {
		req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		newCleanupRouter(store, rt).ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("payload %s: expected status 400, got %d: %s", payload, w.Code, w.Body.String())
		}
	}
}

func TestContainerController_RemoveOrphanContainers_InvalidPayload(t *testing.T) {
	store := &cleanupStore{}
	rt := &cleanupRuntime{}

	req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newCleanupRouter(store, rt).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestContainerController_RemoveOrphanContainers_RuntimeError(t *testing.T) {
	store := &cleanupStore{
		doc: &repository.DataDocument{Containers: []repository.Container{{Name: "gone1"}}},
	}
	rt := &cleanupRuntime{listErr: errors.New("docker unavailable")}

	body, _ := json.Marshal(map[string][]string{"names": {"gone1"}})
	req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newCleanupRouter(store, rt).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.removed) != 0 {
		t.Errorf("nothing should be deleted when the runtime check fails, deleted: %v", store.removed)
	}
}

func TestContainerController_RemoveOrphanContainers_StoreError(t *testing.T) {
	store := &cleanupStore{
		doc:       &repository.DataDocument{Containers: []repository.Container{{Name: "gone1"}}},
		removeErr: errors.New("cache failure"),
	}
	rt := &cleanupRuntime{}

	body, _ := json.Marshal(map[string][]string{"names": {"gone1"}})
	req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newCleanupRouter(store, rt).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestContainerController_OrphansWithMemoryRuntime covers the development runtime:
// it has no container daemon and mirrors the registry, so a container registered
// after start-up must never be reported (or deleted) as missing.
func TestContainerController_OrphansWithMemoryRuntime(t *testing.T) {
	store := &cleanupStore{
		doc: &repository.DataDocument{
			Containers: []repository.Container{
				{Name: "created-after-boot", FriendlyName: "New", URL: "http://new.local", Active: boolPtrCleanup(true)},
			},
		},
	}
	rt := runtime.NewMemoryRuntimeWithRegistry(nil, store)
	cc := NewContainerController(context.Background(), store, rt, "", "")

	r := gin.New()
	r.GET("/containers/orphans", cc.ListOrphanContainers)
	r.POST("/containers/orphans/remove", cc.RemoveOrphanContainers)

	t.Run("no orphans are reported", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/orphans", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var orphans []repository.Container
		if err := json.Unmarshal(w.Body.Bytes(), &orphans); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if len(orphans) != 0 {
			t.Errorf("expected no orphans, got %+v", orphans)
		}
	})

	t.Run("removal is skipped", func(t *testing.T) {
		body, _ := json.Marshal(removeOrphansRequest{Names: []string{"created-after-boot"}})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/containers/orphans/remove", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp removeOrphansResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if len(resp.Removed) != 0 {
			t.Errorf("expected nothing to be removed, got %v", resp.Removed)
		}
		if len(resp.Skipped) != 1 || resp.Skipped[0] != "created-after-boot" {
			t.Errorf("expected the container to be skipped, got %v", resp.Skipped)
		}
		if len(store.removed) != 0 {
			t.Errorf("expected the registry to be untouched, got removed: %v", store.removed)
		}
	})
}
