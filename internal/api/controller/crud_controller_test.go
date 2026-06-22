package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
)

// mockCrudService implements CrudService[repository.Container]
type mockCrudService struct {
	removeErr error
	removed   []repository.Container
}

func (m *mockCrudService) All() ([]repository.Container, error) { return nil, nil }
func (m *mockCrudService) Add(item repository.Container) ([]repository.Container, error) {
	return nil, nil
}
func (m *mockCrudService) Remove(name string) ([]repository.Container, error) {
	if m.removeErr != nil {
		return nil, m.removeErr
	}
	return m.removed, nil
}

func TestCrudController_Delete_MissingName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cc := &CrudController[repository.Container]{Service: &mockCrudService{}}

	r := gin.New()
	// Register route without :name to simulate missing name param
	r.DELETE("/resource/", cc.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/resource/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCrudController_Delete_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	removed := []repository.Container{{Name: "foo"}}
	svc := &mockCrudService{removed: removed}
	cc := &CrudController[repository.Container]{Service: svc}

	r := gin.New()
	r.DELETE("/resource/:name", cc.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/resource/foo", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp []repository.Container
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp) != 1 || resp[0].Name != "foo" {
		t.Errorf("unexpected response body: %v", resp)
	}
}

func TestCrudController_Delete_NotFoundAndError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// NotFound case
	svcNotFound := &mockCrudService{removeErr: cache.ErrContainerNotFound}
	cc1 := &CrudController[repository.Container]{Service: svcNotFound}
	r1 := gin.New()
	r1.DELETE("/resource/:name", cc1.Delete)
	req1 := httptest.NewRequest(http.MethodDelete, "/resource/x", nil)
	w1 := httptest.NewRecorder()
	r1.ServeHTTP(w1, req1)
	if w1.Code != http.StatusNotFound {
		t.Errorf("expected 404 for not found, got %d", w1.Code)
	}

	// Internal error case
	svcErr := &mockCrudService{removeErr: errors.New("boom")}
	cc2 := &CrudController[repository.Container]{Service: svcErr}
	r2 := gin.New()
	r2.DELETE("/resource/:name", cc2.Delete)
	req2 := httptest.NewRequest(http.MethodDelete, "/resource/x", nil)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for internal error, got %d", w2.Code)
	}
}
