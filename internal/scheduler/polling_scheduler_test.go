package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
)

func boolPtr(b bool) *bool {
	return &b
}

// MockStore implements cache.ReadOnlyStore for testing
type MockStore struct {
	err error
	doc repository.DataDocument
}

func (m *MockStore) Snapshot() (repository.DataDocument, error) {
	return m.doc, m.err
}

// MockRuntime implements runtime.ContainerRuntime for testing
type MockRuntime struct {
	startErr error
	stopErr  error
	running  map[string]bool
	started  []string
	stopped  []string
	mu       sync.Mutex
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		running: make(map[string]bool),
		started: []string{},
		stopped: []string{},
	}
}

func (m *MockRuntime) IsRunning(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running[name], nil
}

func (m *MockRuntime) Start(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.running[name] = true
	m.started = append(m.started, name)
	return nil
}

func (m *MockRuntime) Stop(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	m.running[name] = false
	m.stopped = append(m.stopped, name)
	return nil
}

// ListContainers returns the list of container names known to the mock runtime.
func (m *MockRuntime) ListContainers(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.running))
	for n := range m.running {
		names = append(names, n)
	}
	return names, nil
}

// Stats returns zero stats for the mock runtime.
func (m *MockRuntime) Stats(_ context.Context, containerName string) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}

func TestNewPollingScheduler(t *testing.T) {
	store := &MockStore{}
	rt := NewMockRuntime()

	scheduler := NewPollingScheduler(store, rt, 30*time.Second, nil)

	if scheduler == nil {
		t.Fatal("expected scheduler to be created")
	}
	if scheduler.loc == nil {
		t.Error("expected location to default to time.Local")
	}
	if scheduler.poll != 30*time.Second {
		t.Errorf("expected poll to be 30s, got %v", scheduler.poll)
	}
}

func TestNewPollingScheduler_WithLocation(t *testing.T) {
	store := &MockStore{}
	rt := NewMockRuntime()
	loc, _ := time.LoadLocation("Europe/Rome")

	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	if scheduler.loc != loc {
		t.Error("expected custom location to be set")
	}
}

func TestDayKey(t *testing.T) {
	testTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	expected := "2024-03-15"

	result := dayKey(testTime)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestContainsInt(t *testing.T) {
	list := []int{0, 1, 2, 3}

	if !containsInt(list, 0) {
		t.Error("expected 0 to be in list")
	}
	if !containsInt(list, 2) {
		t.Error("expected 2 to be in list")
	}
	if containsInt(list, 5) {
		t.Error("expected 5 NOT to be in list")
	}
	if containsInt(nil, 0) {
		t.Error("expected nil list to not contain any value")
	}
}

func TestExpandScheduleTargets_Container(t *testing.T) {
	containers := map[string]repository.Container{
		"c1": {Name: "c1"},
	}
	groups := map[string]repository.Group{}

	sched := repository.Schedule{Target: "c1", TargetType: "container"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 1 || result[0] != "c1" {
		t.Errorf("expected [c1], got %v", result)
	}
}

func TestExpandScheduleTargets_ContainerNotFound(t *testing.T) {
	containers := map[string]repository.Container{}
	groups := map[string]repository.Group{}

	sched := repository.Schedule{Target: "unknown", TargetType: "container"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestExpandScheduleTargets_Group(t *testing.T) {
	containers := map[string]repository.Container{
		"c1": {Name: "c1"},
		"c2": {Name: "c2"},
	}
	groups := map[string]repository.Group{
		"g1": {Name: "g1", Container: []string{"c1", "c2"}, Active: boolPtr(true)},
	}

	sched := repository.Schedule{Target: "g1", TargetType: "group"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 2 {
		t.Errorf("expected 2 containers, got %v", result)
	}
}

func TestExpandScheduleTargets_GroupNotActive(t *testing.T) {
	containers := map[string]repository.Container{
		"c1": {Name: "c1"},
	}
	groups := map[string]repository.Group{
		"g1": {Name: "g1", Container: []string{"c1"}, Active: boolPtr(false)},
	}

	sched := repository.Schedule{Target: "g1", TargetType: "group"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 0 {
		t.Errorf("expected empty result for inactive group, got %v", result)
	}
}

func TestExpandScheduleTargets_GroupNotFound(t *testing.T) {
	containers := map[string]repository.Container{}
	groups := map[string]repository.Group{}

	sched := repository.Schedule{Target: "unknown", TargetType: "group"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestExpandScheduleTargets_EmptyTarget(t *testing.T) {
	containers := map[string]repository.Container{}
	groups := map[string]repository.Group{}

	sched := repository.Schedule{Target: "", TargetType: "container"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 0 {
		t.Errorf("expected empty result for empty target, got %v", result)
	}
}

func TestExpandScheduleTargets_UnknownType(t *testing.T) {
	containers := map[string]repository.Container{"c1": {Name: "c1"}}
	groups := map[string]repository.Group{}

	sched := repository.Schedule{Target: "c1", TargetType: "unknown"}
	result := expandScheduleTargets(sched, containers, groups)

	if len(result) != 0 {
		t.Errorf("expected empty result for unknown type, got %v", result)
	}
}

func TestIsTimerActiveNow_WithinWindow(t *testing.T) {
	now := time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC) // Monday (weekday 1)

	timer := repository.Timer{
		StartTime: "08:00",
		StopTime:  "18:00",
		Days:      []int{1}, // Monday
		Active:    boolPtr(true),
	}

	if !isTimerActiveNow(timer, now) {
		t.Error("expected timer to be active at 10:00 within 08:00-18:00 window on Monday")
	}
}

func TestIsTimerActiveNow_OutsideWindow(t *testing.T) {
	now := time.Date(2024, 3, 18, 7, 0, 0, 0, time.UTC) // Monday 07:00

	timer := repository.Timer{
		StartTime: "08:00",
		StopTime:  "18:00",
		Days:      []int{1}, // Monday
		Active:    boolPtr(true),
	}

	if isTimerActiveNow(timer, now) {
		t.Error("expected timer NOT to be active at 07:00 (before 08:00)")
	}
}

func TestIsTimerActiveNow_WrongDay(t *testing.T) {
	now := time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC) // Monday (weekday 1)

	timer := repository.Timer{
		StartTime: "08:00",
		StopTime:  "18:00",
		Days:      []int{0, 2, 3, 4, 5, 6}, // All days except Monday
		Active:    boolPtr(true),
	}

	if isTimerActiveNow(timer, now) {
		t.Error("expected timer NOT to be active on Monday when Days excludes Monday")
	}
}

func TestIsTimerActiveNow_CrossMidnight(t *testing.T) {
	// Timer from 22:00 to 06:00
	now := time.Date(2024, 3, 19, 2, 0, 0, 0, time.UTC) // Tuesday 02:00

	timer := repository.Timer{
		StartTime: "22:00",
		StopTime:  "06:00",
		Days:      []int{1}, // Monday (when the timer started)
		Active:    boolPtr(true),
	}

	if !isTimerActiveNow(timer, now) {
		t.Error("expected timer to be active at Tuesday 02:00 within Monday 22:00 - Tuesday 06:00 window")
	}
}

func TestIsTimerActiveNow_InvalidStartTime(t *testing.T) {
	now := time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC)

	timer := repository.Timer{
		StartTime: "invalid",
		StopTime:  "18:00",
		Days:      []int{1},
		Active:    boolPtr(true),
	}

	if isTimerActiveNow(timer, now) {
		t.Error("expected false for invalid start time")
	}
}

func TestIsTimerActiveNow_InvalidStopTime(t *testing.T) {
	now := time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC)

	timer := repository.Timer{
		StartTime: "08:00",
		StopTime:  "invalid",
		Days:      []int{1},
		Active:    boolPtr(true),
	}

	if isTimerActiveNow(timer, now) {
		t.Error("expected false for invalid stop time")
	}
}

func TestPollingScheduler_GetSetFlags(t *testing.T) {
	store := &MockStore{}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, nil)

	// Initially empty
	flags := scheduler.getFlags("container1")
	if flags.StartedDayKey != "" || flags.StoppedDayKey != "" {
		t.Error("expected empty flags initially")
	}

	// Set flags
	scheduler.setFlags("container1", DayFlags{StartedDayKey: "2024-03-18", StoppedDayKey: ""})

	flags = scheduler.getFlags("container1")
	if flags.StartedDayKey != "2024-03-18" {
		t.Errorf("expected StartedDayKey '2024-03-18', got '%s'", flags.StartedDayKey)
	}
}

func TestPollingScheduler_Start_ContextCancel(t *testing.T) {
	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{},
		},
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 50*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())

	scheduler.Start(ctx)

	// Let it tick once
	time.Sleep(100 * time.Millisecond)

	// Cancel should stop the scheduler
	cancel()

	// Give time to stop
	time.Sleep(100 * time.Millisecond)
	// If we get here without hanging, context cancellation worked
}

func TestPollingScheduler_Tick_SnapshotError(t *testing.T) {
	store := &MockStore{
		err: context.DeadlineExceeded,
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, nil)

	// Should not panic, just log the error
	scheduler.tick(context.Background())

	// No containers should be started or stopped
	if len(rt.started) != 0 || len(rt.stopped) != 0 {
		t.Error("expected no operations when snapshot fails")
	}
}

func TestPollingScheduler_Tick_StartsContainerWhenTimerActive(t *testing.T) {
	// Use UTC with all-day timer for reproducible tests
	loc := time.UTC

	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6}, // All days
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}

	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	scheduler.tick(context.Background())

	// Container should have been started
	if len(rt.started) != 1 || rt.started[0] != "c1" {
		t.Errorf("expected c1 to be started, got started: %v", rt.started)
	}
}

func TestPollingScheduler_Tick_StopsContainerWhenOutsideTimerWindow(t *testing.T) {
	loc := time.UTC

	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "01:00",
							StopTime:  "02:00",
							Days:      []int{0, 1, 2, 3, 4, 5, 6}, // All days
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}

	rt := NewMockRuntime()
	rt.running["c1"] = true // Container is currently running
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	now := time.Now().In(loc)
	// Only run if we're outside 01:00-02:00
	if now.Hour() >= 2 || now.Hour() < 1 {
		// First, simulate that start was already evaluated today
		todayKey := dayKey(now)
		scheduler.setFlags("c1", DayFlags{StartedDayKey: todayKey})

		scheduler.tick(context.Background())

		// Container should have been stopped
		if len(rt.stopped) != 1 || rt.stopped[0] != "c1" {
			t.Errorf("expected c1 to be stopped, got stopped: %v", rt.stopped)
		}
	} else {
		t.Skip("Skipping test - cannot run during 01:00-02:00 window")
	}
}

func TestPollingScheduler_Tick_InactiveContainer(t *testing.T) {
	loc := time.UTC

	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(false)}, // Inactive
			},
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6}, // All days
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}

	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	scheduler.tick(context.Background())

	// Inactive container should NOT be started
	if len(rt.started) != 0 {
		t.Errorf("expected no containers started for inactive container, got: %v", rt.started)
	}
}

func TestPollingScheduler_Tick_InactiveTimer(t *testing.T) {
	loc := time.UTC

	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6}, // All days
							Active:    boolPtr(false),             // Inactive timer
						},
					},
				},
			},
		},
	}

	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	scheduler.tick(context.Background())

	// Inactive timer should NOT trigger start
	if len(rt.started) != 0 {
		t.Errorf("expected no containers started for inactive timer, got: %v", rt.started)
	}
}

func TestPollingScheduler_Tick_GroupTargetType(t *testing.T) {
	loc := time.UTC

	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
				{Name: "c2", Active: boolPtr(true)},
			},
			Groups: []repository.Group{
				{Name: "g1", Container: []string{"c1", "c2"}, Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "sched1",
					Target:     "g1",
					TargetType: "group",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6}, // All days
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}

	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, loc)

	scheduler.tick(context.Background())

	// Both containers in the group should be started
	if len(rt.started) != 2 {
		t.Errorf("expected 2 containers started for group, got: %v", rt.started)
	}
}

func TestExpandScheduleTargets_GroupWithEmptyContainerNames(t *testing.T) {
	containers := map[string]repository.Container{
		"c1": {Name: "c1"},
	}
	groups := map[string]repository.Group{
		"g1": {Name: "g1", Container: []string{"c1", "", "c2"}, Active: boolPtr(true)},
	}

	sched := repository.Schedule{Target: "g1", TargetType: "group"}
	result := expandScheduleTargets(sched, containers, groups)

	// Should skip empty string
	found := false
	for _, name := range result {
		if name == "" {
			found = true
		}
	}
	if found {
		t.Error("expected empty container names to be filtered out")
	}
}

// ==================== Concurrency Tests ====================

// TestPollingScheduler_ConcurrentGetSetFlags verifies that concurrent access to
// the flags map is thread-safe. This test should be run with -race flag.
func TestPollingScheduler_ConcurrentGetSetFlags(t *testing.T) {
	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
				{Name: "c2", Active: boolPtr(true)},
				{Name: "c3", Active: boolPtr(true)},
			},
		},
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, time.Local)

	var wg sync.WaitGroup
	const numGoroutines = 50

	// Concurrent reads and writes to flags
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		var containerName string
		switch i % 3 {
		case 1:
			containerName = "c2"
		case 2:
			containerName = "c3"
		default:
			containerName = "c1"
		}

		// Concurrent getFlags
		go func(name string) {
			defer wg.Done()
			_ = scheduler.getFlags(name)
		}(containerName)

		// Concurrent setFlags
		go func(name string, idx int) {
			defer wg.Done()
			scheduler.setFlags(name, DayFlags{
				StartedDayKey: "2026-02-01",
				StoppedDayKey: "2026-02-01",
			})
		}(containerName, i)
	}

	wg.Wait()
}

// TestPollingScheduler_ConcurrentTick verifies that multiple tick executions
// can run concurrently without race conditions. This simulates scenarios where
// a tick takes longer than the poll interval.
func TestPollingScheduler_ConcurrentTick(t *testing.T) {
	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
				{Name: "c2", Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6},
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 30*time.Second, time.Local)

	var wg sync.WaitGroup
	const numTicks = 20

	ctx := context.Background()
	for i := 0; i < numTicks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.tick(ctx)
		}()
	}

	wg.Wait()
}

// TestPollingScheduler_StartWithContextCancellation verifies that the scheduler
// properly handles context cancellation during concurrent operations.
func TestPollingScheduler_StartWithContextCancellation(t *testing.T) {
	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
			},
			Schedules: []repository.Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []repository.Timer{
						{
							StartTime: "00:00",
							StopTime:  "23:59",
							Days:      []int{0, 1, 2, 3, 4, 5, 6},
							Active:    boolPtr(true),
						},
					},
				},
			},
		},
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 10*time.Millisecond, time.Local)

	ctx, cancel := context.WithCancel(context.Background())

	// Start scheduler
	scheduler.Start(ctx)

	// Let it run a few ticks
	time.Sleep(50 * time.Millisecond)

	// Cancel while scheduler might be in the middle of tick
	cancel()

	// Give time for graceful shutdown
	time.Sleep(30 * time.Millisecond)

	// Scheduler should have stopped - no panics or race conditions
}

// TestPollingScheduler_ConcurrentStartMultipleTimes verifies that calling Start
// multiple times concurrently does not cause issues (even though it creates
// multiple goroutines - this tests for race conditions in initialization).
func TestPollingScheduler_ConcurrentStartMultipleTimes(t *testing.T) {
	store := &MockStore{
		doc: repository.DataDocument{
			Containers: []repository.Container{
				{Name: "c1", Active: boolPtr(true)},
			},
		},
	}
	rt := NewMockRuntime()
	scheduler := NewPollingScheduler(store, rt, 100*time.Millisecond, time.Local)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	// Start multiple times concurrently (not recommended in production, but should not panic)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.Start(ctx)
		}()
	}
	wg.Wait()

	// Let schedulers run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel all
	cancel()
	time.Sleep(50 * time.Millisecond)
}
