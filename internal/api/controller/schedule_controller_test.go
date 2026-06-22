package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
)

// Timer alias for convenience
type Timer = repository.Timer

// mockScheduleStore implements cache.ScheduleStore for testing
type mockScheduleStore struct {
	addErr    error
	removeErr error
	doc       repository.DataDocument
}

func (m *mockScheduleStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, nil
}

func (m *mockScheduleStore) AddSchedule(s repository.Schedule) (repository.DataDocument, error) {
	if m.addErr != nil {
		return repository.DataDocument{}, m.addErr
	}
	m.doc.Schedules = append(m.doc.Schedules, s)
	return m.doc, nil
}

func (m *mockScheduleStore) RemoveSchedule(id string) (repository.DataDocument, error) {
	if m.removeErr != nil {
		return repository.DataDocument{}, m.removeErr
	}
	for i, s := range m.doc.Schedules {
		if s.ID == id {
			m.doc.Schedules = append(m.doc.Schedules[:i], m.doc.Schedules[i+1:]...)
			return m.doc, nil
		}
	}
	return repository.DataDocument{}, cache.ErrScheduleNotFound
}

func TestScheduleController_AllSchedules(t *testing.T) {
	active := true
	store := &mockScheduleStore{
		doc: repository.DataDocument{
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "container1",
					TargetType: "container",
					Timers: []Timer{
						{
							StartTime: "08:00",
							StopTime:  "18:00",
							Days:      []int{1, 2, 3, 4, 5},
							Active:    &active,
						},
					},
				},
				{
					ID:         "sched2",
					Target:     "group1",
					TargetType: "group",
					Timers:     []Timer{},
				},
			},
		},
	}

	sc := NewScheduleController(store)

	r := gin.New()
	r.GET("/schedules", sc.AllSchedules)

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var schedules []repository.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &schedules); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(schedules) != 2 {
		t.Errorf("expected 2 schedules, got %d", len(schedules))
	}
}

func TestScheduleController_CreateOrUpdateSchedule_Valid(t *testing.T) {
	store := &mockScheduleStore{
		doc: repository.DataDocument{
			Schedules: []repository.Schedule{},
		},
	}

	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	active := true
	schedule := repository.Schedule{
		ID:         "new-sched",
		Target:     "my-container",
		TargetType: "container",
		Timers: []Timer{
			{
				StartTime: "09:30",
				StopTime:  "17:00",
				Days:      []int{1, 2, 3, 4, 5},
				Active:    &active,
			},
		},
	}
	body, _ := json.Marshal(schedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestScheduleController_CreateOrUpdateSchedule_InvalidPayload(t *testing.T) {
	store := &mockScheduleStore{}
	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestScheduleController_CreateOrUpdateSchedule_ValidationError(t *testing.T) {
	store := &mockScheduleStore{}
	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	// Missing required fields (id, target, targetType)
	schedule := map[string]any{
		"timers": []any{},
	}
	body, _ := json.Marshal(schedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestScheduleController_CreateOrUpdateSchedule_StoreError(t *testing.T) {
	store := &mockScheduleStore{
		addErr: errors.New("store error"),
	}
	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	active := true
	schedule := repository.Schedule{
		ID:         "test",
		Target:     "container1",
		TargetType: "container",
		Timers: []Timer{
			{StartTime: "08:00", StopTime: "18:00", Days: []int{1}, Active: &active},
		},
	}
	body, _ := json.Marshal(schedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestScheduleController_DeleteSchedule_Success(t *testing.T) {
	active := true
	store := &mockScheduleStore{
		doc: repository.DataDocument{
			Schedules: []repository.Schedule{
				{ID: "to-delete", Target: "c1", TargetType: "container", Timers: []Timer{{StartTime: "08:00", StopTime: "18:00", Days: []int{1}, Active: &active}}},
			},
		},
	}
	sc := NewScheduleController(store)

	r := gin.New()
	r.DELETE("/schedule/:id", sc.DeleteSchedule)

	req := httptest.NewRequest(http.MethodDelete, "/schedule/to-delete", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestScheduleController_DeleteSchedule_NotFound(t *testing.T) {
	store := &mockScheduleStore{
		doc: repository.DataDocument{
			Schedules: []repository.Schedule{},
		},
	}
	sc := NewScheduleController(store)

	r := gin.New()
	r.DELETE("/schedule/:id", sc.DeleteSchedule)

	req := httptest.NewRequest(http.MethodDelete, "/schedule/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestScheduleController_DeleteSchedule_MissingID(t *testing.T) {
	store := &mockScheduleStore{}
	sc := NewScheduleController(store)

	r := gin.New()
	r.DELETE("/schedule/", sc.DeleteSchedule)

	req := httptest.NewRequest(http.MethodDelete, "/schedule/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestScheduleController_CreateOrUpdateSchedule_WithMultipleTimers(t *testing.T) {
	store := &mockScheduleStore{
		doc: repository.DataDocument{
			Schedules: []repository.Schedule{},
		},
	}

	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	active := true
	schedule := repository.Schedule{
		ID:         "multi-timer",
		Target:     "production-server",
		TargetType: "container",
		Timers: []Timer{
			{
				StartTime: "08:00",
				StopTime:  "12:00",
				Days:      []int{1, 2, 3, 4, 5},
				Active:    &active,
			},
			{
				StartTime: "13:00",
				StopTime:  "18:30",
				Days:      []int{1, 2, 3, 4, 5},
				Active:    &active,
			},
			{
				StartTime: "10:00",
				StopTime:  "14:00",
				Days:      []int{6, 0},
				Active:    &active,
			},
		},
	}
	body, _ := json.Marshal(schedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}
