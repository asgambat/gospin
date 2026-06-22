package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bassista/go_spin/internal/repository"
)

func boolPtr(b bool) *bool {
	return &b
}

func createTestDocument() repository.DataDocument {
	return repository.DataDocument{
		Metadata: repository.Metadata{LastUpdate: 1000},
		Containers: []repository.Container{
			{Name: "container1", FriendlyName: "Container 1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)},
		},
		Order: []string{"container1"},
		Groups: []repository.Group{
			{Name: "group1", Container: []string{"container1"}, Active: boolPtr(true)},
		},
		GroupOrder: []string{"group1"},
		Schedules: []repository.Schedule{
			{ID: "schedule1", Target: "container1", TargetType: "container", Timers: []repository.Timer{}},
		},
	}
}

func TestNewStore(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	if store == nil {
		t.Fatal("expected store to be created")
	}

	if store.GetLastUpdate() != doc.Metadata.LastUpdate {
		t.Errorf("expected lastUpdate %d, got %d", doc.Metadata.LastUpdate, store.GetLastUpdate())
	}
}

func TestStore_DirtyFlag(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	// Initially not dirty
	if store.IsDirty() {
		t.Error("expected store to not be dirty initially")
	}

	// Mark dirty
	store.MarkDirty()
	if !store.IsDirty() {
		t.Error("expected store to be dirty after MarkDirty")
	}

	// Clear dirty
	store.ClearDirty()
	if store.IsDirty() {
		t.Error("expected store to not be dirty after ClearDirty")
	}
}

func TestStore_LastUpdate(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	if store.GetLastUpdate() != 1000 {
		t.Errorf("expected lastUpdate 1000, got %d", store.GetLastUpdate())
	}

	store.SetLastUpdate(2000)
	if store.GetLastUpdate() != 2000 {
		t.Errorf("expected lastUpdate 2000, got %d", store.GetLastUpdate())
	}
}

func TestStore_Snapshot(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshot.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(snapshot.Containers))
	}

	// Modify snapshot should not affect store
	snapshot.Containers = append(snapshot.Containers, repository.Container{Name: "modified"})

	snapshot2, _ := store.Snapshot()
	if len(snapshot2.Containers) != 1 {
		t.Error("modifying snapshot should not affect store")
	}
}

func TestStore_Replace(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)
	store.MarkDirty()

	newDoc := repository.DataDocument{
		Metadata:   repository.Metadata{LastUpdate: 3000},
		Containers: []repository.Container{},
		Order:      []string{},
	}

	err := store.Replace(newDoc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.IsDirty() {
		t.Error("expected store to not be dirty after Replace")
	}

	if store.GetLastUpdate() != 3000 {
		t.Errorf("expected lastUpdate 3000, got %d", store.GetLastUpdate())
	}

	snapshot, _ := store.Snapshot()
	if len(snapshot.Containers) != 0 {
		t.Errorf("expected 0 containers, got %d", len(snapshot.Containers))
	}
}

func TestStore_AddContainer_New(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	newContainer := repository.Container{
		Name:         "container2",
		FriendlyName: "Container 2",
		URL:          "http://c2.local",
		Running:      boolPtr(false),
		Active:       boolPtr(true),
	}

	result, err := store.AddContainer(newContainer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(result.Containers))
	}

	if !store.IsDirty() {
		t.Error("expected store to be dirty after AddContainer")
	}

	// Check order was updated
	if len(result.Order) != 2 || result.Order[1] != "container2" {
		t.Error("expected container2 to be added to order")
	}
}

func TestStore_AddContainer_Update(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	updatedContainer := repository.Container{
		Name:         "container1",
		FriendlyName: "Updated Container 1",
		URL:          "http://c1-updated.local",
		Running:      boolPtr(true),
		Active:       boolPtr(true),
	}

	result, err := store.AddContainer(updatedContainer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container after update, got %d", len(result.Containers))
	}

	if result.Containers[0].FriendlyName != "updated container 1" {
		t.Error("expected container to be updated and normalized to lowercase")
	}

	// Order should not be duplicated
	if len(result.Order) != 1 {
		t.Errorf("expected order to have 1 entry, got %d", len(result.Order))
	}
}

func TestStore_RemoveContainer_Success(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	result, err := store.RemoveContainer("container1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Containers) != 0 {
		t.Errorf("expected 0 containers, got %d", len(result.Containers))
	}

	if len(result.Order) != 0 {
		t.Errorf("expected order to be empty, got %d entries", len(result.Order))
	}

	if !store.IsDirty() {
		t.Error("expected store to be dirty after RemoveContainer")
	}

	// The container should be removed from any groups that referenced it
	if len(result.Groups) != 1 {
		t.Errorf("expected 1 group to remain, got %d", len(result.Groups))
	} else if len(result.Groups[0].Container) != 0 {
		t.Errorf("expected group to have no containers after removal, got %d", len(result.Groups[0].Container))
	}
}

func TestStore_RemoveContainer_NotFound(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	_, err := store.RemoveContainer("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
	if err != ErrContainerNotFound {
		t.Errorf("expected ErrContainerNotFound, got %v", err)
	}
}

func TestStore_AddGroup_New(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	newGroup := repository.Group{
		Name:      "group2",
		Container: []string{"container1"},
		Active:    boolPtr(true),
	}

	result, err := store.AddGroup(newGroup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(result.Groups))
	}

	if len(result.GroupOrder) != 2 || result.GroupOrder[1] != "group2" {
		t.Error("expected group2 to be added to group order")
	}
}

func TestStore_AddGroup_Update(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	updatedGroup := repository.Group{
		Name:      "group1",
		Container: []string{"container1", "container2"},
		Active:    boolPtr(false),
	}

	result, err := store.AddGroup(updatedGroup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Groups) != 1 {
		t.Errorf("expected 1 group after update, got %d", len(result.Groups))
	}

	if len(result.Groups[0].Container) != 2 {
		t.Error("expected group to be updated with 2 containers")
	}
}

func TestStore_RemoveGroup_Success(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	result, err := store.RemoveGroup("group1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(result.Groups))
	}

	if len(result.GroupOrder) != 0 {
		t.Errorf("expected group order to be empty, got %d entries", len(result.GroupOrder))
	}
}

func TestStore_RemoveGroup_NotFound(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	_, err := store.RemoveGroup("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent group")
	}
	if err != ErrGroupNotFound {
		t.Errorf("expected ErrGroupNotFound, got %v", err)
	}
}

func TestStore_AddSchedule_New(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	newSchedule := repository.Schedule{
		ID:         "schedule2",
		Target:     "group1",
		TargetType: "group",
		Timers:     []repository.Timer{},
	}

	result, err := store.AddSchedule(newSchedule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Schedules) != 2 {
		t.Errorf("expected 2 schedules, got %d", len(result.Schedules))
	}
}

func TestStore_AddSchedule_Update(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	updatedSchedule := repository.Schedule{
		ID:         "schedule1",
		Target:     "group1",
		TargetType: "group",
		Timers:     []repository.Timer{{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)}},
	}

	result, err := store.AddSchedule(updatedSchedule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Schedules) != 1 {
		t.Errorf("expected 1 schedule after update, got %d", len(result.Schedules))
	}

	if result.Schedules[0].Target != "group1" {
		t.Error("expected schedule to be updated")
	}
}

func TestStore_RemoveSchedule_Success(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	result, err := store.RemoveSchedule("schedule1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Schedules) != 0 {
		t.Errorf("expected 0 schedules, got %d", len(result.Schedules))
	}
}

func TestStore_RemoveSchedule_NotFound(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	_, err := store.RemoveSchedule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent schedule")
	}
	if err != ErrScheduleNotFound {
		t.Errorf("expected ErrScheduleNotFound, got %v", err)
	}
}

func TestStore_Concurrency(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.Snapshot()
			_ = store.IsDirty()
			_ = store.GetLastUpdate()
		}()
	}

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			container := repository.Container{
				Name:         "concurrent_container",
				FriendlyName: "Concurrent",
				URL:          "http://concurrent.local",
				Running:      boolPtr(false),
				Active:       boolPtr(true),
			}
			_, _ = store.AddContainer(container)
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or panic, concurrency is handled correctly
}

// mockSaver implements repository.Saver for testing
type mockSaver struct {
	saveErr   error
	savedDocs []*repository.DataDocument
	mu        sync.Mutex
}

func (m *mockSaver) Save(ctx context.Context, doc *repository.DataDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.savedDocs = append(m.savedDocs, doc)
	return nil
}

// Count returns the number of saved documents in a thread-safe manner.
func (m *mockSaver) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.savedDocs)
}

func TestStartPersistenceScheduler_PeriodicFlush(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)
	store.MarkDirty() // Mark dirty so it will flush

	saver := &mockSaver{}
	ctx, cancel := context.WithCancel(context.Background())

	StartPersistenceScheduler(ctx, store, saver, 50*time.Millisecond)

	// Wait for at least one flush
	time.Sleep(100 * time.Millisecond)

	cancel()

	// Give time for final flush
	time.Sleep(50 * time.Millisecond)

	// Should have saved at least once
	if saver.Count() < 1 {
		t.Error("expected at least one save operation")
	}

	// After flush, store should not be dirty
	if store.IsDirty() {
		t.Error("expected store to be clean after flush")
	}
}

func TestStartPersistenceScheduler_NotDirtySkipsFlush(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)
	// NOT marking dirty

	saver := &mockSaver{}
	ctx, cancel := context.WithCancel(context.Background())

	StartPersistenceScheduler(ctx, store, saver, 50*time.Millisecond)

	// Wait for potential flush
	time.Sleep(100 * time.Millisecond)

	cancel()
	time.Sleep(50 * time.Millisecond)

	// Should not have saved since store wasn't dirty
	if saver.Count() > 0 {
		t.Error("expected no saves when store is not dirty")
	}
}

func TestStartPersistenceScheduler_SaveError(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)
	store.MarkDirty()

	saver := &mockSaver{saveErr: errors.New("disk full")}
	ctx, cancel := context.WithCancel(context.Background())

	StartPersistenceScheduler(ctx, store, saver, 50*time.Millisecond)

	// Wait for flush attempt
	time.Sleep(100 * time.Millisecond)

	cancel()
	time.Sleep(50 * time.Millisecond)

	// Store should still be dirty since save failed
	if !store.IsDirty() {
		t.Error("expected store to remain dirty after save error")
	}
}

func TestStartPersistenceScheduler_FinalFlushOnShutdown(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	saver := &mockSaver{}
	ctx, cancel := context.WithCancel(context.Background())

	StartPersistenceScheduler(ctx, store, saver, 10*time.Second) // Long interval

	// Mark dirty after scheduler starts
	store.MarkDirty()

	// Cancel immediately - should trigger final flush
	cancel()

	// Give time for final flush
	time.Sleep(100 * time.Millisecond)

	// Should have done final flush
	if saver.Count() < 1 {
		t.Error("expected final flush on shutdown")
	}
}

// ==================== Concurrency Tests ====================

// TestStore_ConcurrentAddContainer verifies that concurrent AddContainer operations
// are thread-safe and don't cause data corruption.
func TestStore_ConcurrentAddContainer(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	const numGoroutines = 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			container := repository.Container{
				Name:         "concurrent-container-" + string(rune('a'+idx%26)),
				FriendlyName: "Concurrent Container",
				URL:          "http://concurrent.local",
				Running:      boolPtr(false),
				Active:       boolPtr(true),
			}
			_, err := store.AddContainer(container)
			if err != nil {
				t.Errorf("AddContainer error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Store should be dirty
	if !store.IsDirty() {
		t.Error("expected store to be dirty after concurrent adds")
	}

	// Snapshot should be valid
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("snapshot error: %v", err)
	}
	if len(snapshot.Containers) < 2 {
		t.Error("expected at least 2 containers after concurrent adds")
	}
}

// TestStore_ConcurrentSnapshotAndModify verifies that taking snapshots while
// modifying the store is thread-safe.
func TestStore_ConcurrentSnapshotAndModify(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	const numGoroutines = 100

	// Concurrent snapshots
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Snapshot()
			if err != nil {
				t.Errorf("Snapshot error: %v", err)
			}
		}()
	}

	// Concurrent modifications
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			container := repository.Container{
				Name:         "snapshot-test-" + string(rune('a'+idx%26)),
				FriendlyName: "Snapshot Test",
				URL:          "http://snapshot.local",
				Running:      boolPtr(false),
				Active:       boolPtr(true),
			}
			_, _ = store.AddContainer(container)
		}(i)
	}

	wg.Wait()
}

// TestStore_ConcurrentDirtyFlag verifies that concurrent MarkDirty/ClearDirty/IsDirty
// operations are thread-safe.
func TestStore_ConcurrentDirtyFlag(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	const numGoroutines = 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(3)

		// Concurrent MarkDirty
		go func() {
			defer wg.Done()
			store.MarkDirty()
		}()

		// Concurrent IsDirty
		go func() {
			defer wg.Done()
			_ = store.IsDirty()
		}()

		// Concurrent ClearDirty
		go func() {
			defer wg.Done()
			store.ClearDirty()
		}()
	}

	wg.Wait()
}

// TestStore_ConcurrentLastUpdate verifies that concurrent GetLastUpdate/SetLastUpdate
// operations are thread-safe.
func TestStore_ConcurrentLastUpdate(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	const numGoroutines = 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		// Concurrent SetLastUpdate
		go func(idx int) {
			defer wg.Done()
			store.SetLastUpdate(int64(1000 + idx))
		}(i)

		// Concurrent GetLastUpdate
		go func() {
			defer wg.Done()
			_ = store.GetLastUpdate()
		}()
	}

	wg.Wait()
}

// TestStartPersistenceScheduler_ConcurrentModifications verifies that the
// persistence scheduler handles concurrent store modifications correctly.
func TestStartPersistenceScheduler_ConcurrentModifications(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)
	saver := &mockSaver{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start persistence scheduler with short interval
	StartPersistenceScheduler(ctx, store, saver, 20*time.Millisecond)

	var wg sync.WaitGroup
	const numGoroutines = 30

	// Concurrent modifications while scheduler is running
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			container := repository.Container{
				Name:         "persist-test-" + string(rune('a'+idx%26)),
				FriendlyName: "Persist Test",
				URL:          "http://persist.local",
				Running:      boolPtr(false),
				Active:       boolPtr(true),
			}
			_, _ = store.AddContainer(container)
			// Small delay to allow scheduler ticks
			time.Sleep(5 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// Give time for scheduler to process
	time.Sleep(100 * time.Millisecond)

	// Should have saved at least once
	if saver.Count() < 1 {
		t.Error("expected at least one save during concurrent modifications")
	}
}

// TestStore_ConcurrentReplaceAndSnapshot verifies that Replace and Snapshot
// operations work correctly when called concurrently.
func TestStore_ConcurrentReplaceAndSnapshot(t *testing.T) {
	doc := createTestDocument()
	store := NewStore(doc)

	var wg sync.WaitGroup
	const numGoroutines = 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		// Concurrent Replace
		go func(idx int) {
			defer wg.Done()
			newDoc := repository.DataDocument{
				Metadata:   repository.Metadata{LastUpdate: int64(2000 + idx)},
				Containers: []repository.Container{},
				Order:      []string{},
			}
			_ = store.Replace(newDoc)
		}(i)

		// Concurrent Snapshot
		go func() {
			defer wg.Done()
			_, _ = store.Snapshot()
		}()
	}

	wg.Wait()

	// Final snapshot should be valid
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("final snapshot error: %v", err)
	}
	if snapshot.Metadata.LastUpdate < 2000 {
		t.Errorf("expected lastUpdate >= 2000, got %d", snapshot.Metadata.LastUpdate)
	}
}
