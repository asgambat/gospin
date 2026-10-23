package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
			Containers: []repository.Container{{Name: "my-container"}},
			Schedules:  []repository.Schedule{},
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
		doc: repository.DataDocument{
			Containers: []repository.Container{{Name: "container1"}},
		},
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
			Containers: []repository.Container{{Name: "production-server"}},
			Schedules:  []repository.Schedule{},
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

func TestScheduleController_CreateOrUpdateSchedule_PartialTimerAccepted(t *testing.T) {
	active := true

	tests := []struct {
		name  string
		timer Timer
	}{
		{
			name:  "start only",
			timer: Timer{StartTime: "08:00", Days: []int{1, 2, 3}, Active: &active},
		},
		{
			name:  "stop only",
			timer: Timer{StopTime: "18:00", Days: []int{1, 2, 3}, Active: &active},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockScheduleStore{
				doc: repository.DataDocument{
					Containers: []repository.Container{{Name: "container1"}},
				},
			}
			sc := NewScheduleController(store)

			r := gin.New()
			r.POST("/schedule", sc.CreateOrUpdateSchedule)

			schedule := repository.Schedule{
				ID:         "sched-" + tt.name,
				Target:     "container1",
				TargetType: "container",
				Timers:     []Timer{tt.timer},
			}
			body, _ := json.Marshal(schedule)

			req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestScheduleController_CreateOrUpdateSchedule_TimerRequiresStartOrStop(t *testing.T) {
	active := true

	store := &mockScheduleStore{}
	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	schedule := repository.Schedule{
		ID:         "empty-timer",
		Target:     "container1",
		TargetType: "container",
		Timers:     []Timer{{Days: []int{1}, Active: &active}},
	}
	body, _ := json.Marshal(schedule)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "startTime") {
		t.Errorf("expected the error to mention startTime, got %s", w.Body.String())
	}
}

// TestScheduleController_CreateOrUpdateSchedule_TimerRequiresActive pins the
// contract of the active flag: a timer stored with a nil flag would be read as
// active by the scheduler but written back as inactive after a reload, so the API
// rejects the payload instead of guessing.
func TestScheduleController_CreateOrUpdateSchedule_TimerRequiresActive(t *testing.T) {
	store := &mockScheduleStore{}
	sc := NewScheduleController(store)

	r := gin.New()
	r.POST("/schedule", sc.CreateOrUpdateSchedule)

	payload := `{"id":"no-active","target":"container1","targetType":"container","timers":[{"startTime":"08:00","days":[1]}]}`

	req := httptest.NewRequest(http.MethodPost, "/schedule", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Active") {
		t.Errorf("expected the error to mention Active, got %s", w.Body.String())
	}
}

func TestScheduleController_CreateOrUpdateSchedule_InvalidTimeFormat(t *testing.T) {
	active := true

	tests := []struct {
		name  string
		timer Timer
	}{
		{name: "invalid start", timer: Timer{StartTime: "not-a-time", StopTime: "18:00", Days: []int{1}, Active: &active}},
		{name: "out of range stop", timer: Timer{StartTime: "08:00", StopTime: "25:00", Days: []int{1}, Active: &active}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockScheduleStore{}
			sc := NewScheduleController(store)

			r := gin.New()
			r.POST("/schedule", sc.CreateOrUpdateSchedule)

			schedule := repository.Schedule{
				ID:         "bad-time",
				Target:     "container1",
				TargetType: "container",
				Timers:     []Timer{tt.timer},
			}
			body, _ := json.Marshal(schedule)

			req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestScheduleController_CreateOrUpdateSchedule_TargetMustExist(t *testing.T) {
	active := true

	post := func(t *testing.T, doc repository.DataDocument, sched repository.Schedule) (int, string) {
		t.Helper()

		store := &mockScheduleStore{doc: doc}
		sc := NewScheduleController(store)

		r := gin.New()
		r.POST("/schedule", sc.CreateOrUpdateSchedule)

		body, err := json.Marshal(sched)
		if err != nil {
			t.Fatalf("failed to marshal schedule: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	t.Run("container target exists", func(t *testing.T) {
		doc := repository.DataDocument{Containers: []repository.Container{{Name: "nginx"}}}
		sched := repository.Schedule{
			ID: "ok", Target: "nginx", TargetType: "container",
			Timers: []Timer{{StartTime: "08:00", Days: []int{1}, Active: &active}},
		}

		code, body := post(t, doc, sched)
		if code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", code, body)
		}
	})

	t.Run("group target exists", func(t *testing.T) {
		doc := repository.DataDocument{
			Containers: []repository.Container{{Name: "nginx"}},
			Groups:     []repository.Group{{Name: "web"}},
		}
		sched := repository.Schedule{
			ID: "ok-group", Target: "web", TargetType: "group",
			Timers: []Timer{{StopTime: "23:00", Days: []int{1}, Active: &active}},
		}

		code, body := post(t, doc, sched)
		if code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", code, body)
		}
	})

	t.Run("missing container target", func(t *testing.T) {
		sched := repository.Schedule{
			ID: "orphan", Target: "ghost", TargetType: "container",
			Timers: []Timer{{StartTime: "08:00", Days: []int{1}, Active: &active}},
		}

		code, body := post(t, repository.DataDocument{}, sched)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", code, body)
		}
		if !strings.Contains(body, "not found") {
			t.Errorf("expected a 'not found' error message, got %s", body)
		}
	})

	t.Run("missing group target", func(t *testing.T) {
		sched := repository.Schedule{
			ID: "orphan-group", Target: "ghosts", TargetType: "group",
			Timers: []Timer{{StartTime: "08:00", Days: []int{1}, Active: &active}},
		}

		code, body := post(t, repository.DataDocument{}, sched)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", code, body)
		}
		if !strings.Contains(body, "not found") {
			t.Errorf("expected a 'not found' error message, got %s", body)
		}
	})
}
