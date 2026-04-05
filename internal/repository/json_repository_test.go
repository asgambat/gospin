package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func boolPtrJSON(b bool) *bool {
	return &b
}

func createTestDataDocument() DataDocument {
	return DataDocument{
		Metadata: Metadata{LastUpdate: 1000},
		Containers: []Container{
			{Name: "container1", FriendlyName: "Container 1", URL: "http://c1.local", Running: boolPtrJSON(false), Active: boolPtrJSON(true)},
		},
		Order: []string{"container1"},
		Groups: []Group{
			{Name: "group1", Container: []string{"container1"}, Active: boolPtrJSON(true)},
		},
		GroupOrder: []string{"group1"},
		Schedules: []Schedule{
			{ID: "schedule1", Target: "container1", TargetType: "container", Timers: []Timer{}},
		},
	}
}

func TestNewJSONRepository_Success(t *testing.T) {
	repo, err := NewJSONRepository("/tmp/test-config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo == nil {
		t.Error("expected repository to be created")
	}
}

func TestJSONRepository_RemoveSchedulesWithMissingTargets_Load(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := DataDocument{
		Metadata:   Metadata{LastUpdate: 1000},
		Containers: []Container{},
		Groups:     []Group{},
		Schedules: []Schedule{
			{ID: "s1", Target: "container_missing", TargetType: "container", Timers: []Timer{}},
			{ID: "s2", Target: "group_missing", TargetType: "group", Timers: []Timer{}},
		},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if len(loaded.Schedules) != 0 {
		t.Errorf("expected 0 schedules after removing missing targets, got %d", len(loaded.Schedules))
	}
}

func TestJSONRepository_RemoveSchedulesWithMissingTargets_Preserve(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := DataDocument{
		Metadata:   Metadata{LastUpdate: 1000},
		Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1", Active: boolPtrJSON(true)}},
		Groups:     []Group{{Name: "g1", Container: []string{"c1"}, Active: boolPtrJSON(true)}},
		Schedules: []Schedule{
			{ID: "s1", Target: "c1", TargetType: "container", Timers: []Timer{}},
			{ID: "s2", Target: "g1", TargetType: "group", Timers: []Timer{}},
		},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if len(loaded.Schedules) != 2 {
		t.Errorf("expected 2 schedules preserved, got %d", len(loaded.Schedules))
	}
}

func TestNewJSONRepository_EmptyPath(t *testing.T) {
	_, err := NewJSONRepository("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestJSONRepository_LoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create initial file
	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test Load
	loaded, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(loaded.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(loaded.Containers))
	}

	if loaded.Containers[0].Name != "container1" {
		t.Errorf("expected container name 'container1', got '%s'", loaded.Containers[0].Name)
	}
}

func TestJSONRepository_Load_FileNotFound(t *testing.T) {
	repo, _ := NewJSONRepository("/nonexistent/path/config.json")
	_, err := repo.Load(context.Background())
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestJSONRepository_Load_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create invalid JSON file
	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	_, err := repo.Load(context.Background())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestJSONRepository_Load_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create JSON with missing required fields
	invalidDoc := map[string]interface{}{
		"metadata": map[string]interface{}{"lastUpdate": 1000},
		"containers": []map[string]interface{}{
			{"name": "container1"}, // missing required fields
		},
	}
	data, _ := json.MarshalIndent(invalidDoc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	_, err := repo.Load(context.Background())
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestJSONRepository_Save_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create initial empty file
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := createTestDataDocument()
	err = repo.Save(context.Background(), &doc)
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var savedDoc DataDocument
	if err := json.Unmarshal(data, &savedDoc); err != nil {
		t.Fatalf("failed to parse saved file: %v", err)
	}

	if len(savedDoc.Containers) != 1 {
		t.Errorf("expected 1 container in saved file, got %d", len(savedDoc.Containers))
	}
}

func TestJSONRepository_Save_NilDocument(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	repo, _ := NewJSONRepository(configPath)
	err := repo.Save(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil document")
	}
}

func TestJSONRepository_Save_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	repo, _ := NewJSONRepository(configPath)

	// Document with invalid container (missing required URL)
	doc := DataDocument{
		Containers: []Container{
			{Name: "test", FriendlyName: "Test", URL: "", Running: boolPtrJSON(false), Active: boolPtrJSON(true)},
		},
	}

	err := repo.Save(context.Background(), &doc)
	if err == nil {
		t.Error("expected validation error")
	}
}

// MockCacheStore implements CacheStore for testing
type MockCacheStore struct {
	mu         sync.RWMutex
	lastUpdate int64
	dirty      bool
	doc        DataDocument
	replaced   bool
}

func (m *MockCacheStore) GetLastUpdate() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

func (m *MockCacheStore) IsDirty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dirty
}

func (m *MockCacheStore) Snapshot() (DataDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.doc, nil
}

func (m *MockCacheStore) Replace(doc DataDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doc = doc
	m.lastUpdate = doc.Metadata.LastUpdate
	m.replaced = true
	return nil
}

func (m *MockCacheStore) IsReplaced() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.replaced
}

func TestJSONRepository_MakeWatcherCallback_ReloadsWhenDiskNewer(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 2000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000, // Older than disk
		dirty:      false,
		doc:        DataDocument{},
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	callback()

	if !cache.IsReplaced() {
		t.Error("expected cache to be replaced when disk is newer")
	}
}

func TestJSONRepository_MakeWatcherCallback_SkipsWhenDiskOlder(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 500
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000, // Newer than disk
		dirty:      false,
		doc:        DataDocument{},
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	callback()

	if cache.IsReplaced() {
		t.Error("expected cache NOT to be replaced when disk is older")
	}
}

func TestJSONRepository_MakeWatcherCallback_SkipsWhenDirty(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 2000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000,
		dirty:      true, // Cache is dirty
		doc:        DataDocument{},
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	callback()

	if cache.IsReplaced() {
		t.Error("expected cache NOT to be replaced when dirty")
	}
}

func TestJSONRepository_MakeWatcherCallback_SkipsWhenSameContent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cacheDoc := createTestDataDocument()
	cacheDoc.Metadata.LastUpdate = 1000
	cacheDoc.ApplyDefaults() // Apply defaults to match what Load() returns

	cache := &MockCacheStore{
		lastUpdate: 1000, // Same as disk
		dirty:      false,
		doc:        cacheDoc, // Same content after ApplyDefaults
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	callback()

	if cache.IsReplaced() {
		t.Error("expected cache NOT to be replaced when content is same")
	}
}

// ==================== Concurrency Tests ====================

// TestJSONRepository_ConcurrentLoadSave verifies that concurrent Load and Save
// operations are thread-safe and don't cause data corruption.
func TestJSONRepository_ConcurrentLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create initial file
	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	ctx := context.Background()
	const numOperations = 50

	// Concurrent loads
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Load(ctx)
			if err != nil {
				// Load errors are acceptable in concurrent scenario
				t.Logf("concurrent load error (may be expected): %v", err)
			}
		}()
	}

	// Concurrent saves
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			saveDoc := createTestDataDocument()
			saveDoc.Metadata.LastUpdate = int64(2000 + idx)
			err := repo.Save(ctx, &saveDoc)
			if err != nil {
				// Save errors are acceptable in concurrent scenario
				t.Logf("concurrent save error (may be expected): %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Final load should succeed and return valid data
	finalDoc, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("final load failed: %v", err)
	}
	if finalDoc == nil {
		t.Fatal("expected non-nil document")
	}
}

// TestJSONRepository_ConcurrentLoads verifies that multiple concurrent Load
// operations don't interfere with each other.
func TestJSONRepository_ConcurrentLoads(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	ctx := context.Background()
	const numReaders = 100

	results := make([]*DataDocument, numReaders)
	errors := make([]error, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errors[idx] = repo.Load(ctx)
		}(i)
	}

	wg.Wait()

	// All loads should succeed
	for i := 0; i < numReaders; i++ {
		if errors[i] != nil {
			t.Errorf("load %d failed: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Errorf("load %d returned nil document", i)
		}
	}
}

// TestJSONRepository_ConcurrentSaves verifies that multiple concurrent Save
// operations don't cause file corruption.
func TestJSONRepository_ConcurrentSaves(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create initial file
	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	ctx := context.Background()
	const numWriters = 30

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			saveDoc := createTestDataDocument()
			saveDoc.Metadata.LastUpdate = int64(3000 + idx)
			_ = repo.Save(ctx, &saveDoc)
		}(i)
	}

	wg.Wait()

	// Final load should return valid JSON
	finalDoc, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("final load after concurrent saves failed: %v", err)
	}
	if finalDoc == nil {
		t.Fatal("expected non-nil document")
	}
	if len(finalDoc.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(finalDoc.Containers))
	}
}

// TestJSONRepository_LoadWithContextCancellation verifies that Load respects
// context cancellation.
func TestJSONRepository_LoadWithContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = repo.Load(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestJSONRepository_SaveWithContextCancellation verifies that Save respects
// context cancellation.
func TestJSONRepository_SaveWithContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	doc := createTestDataDocument()
	err = repo.Save(ctx, &doc)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestJSONRepository_StartWatcher_Success verifies that the watcher starts correctly
// and shuts down cleanly when context is cancelled.
func TestJSONRepository_StartWatcher_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000,
		dirty:      false,
		doc:        doc,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = jsonRepo.StartWatcher(ctx, cache)
	if err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop watcher
	cancel()

	// Give the watcher time to shut down
	time.Sleep(50 * time.Millisecond)
}

// TestJSONRepository_StartWatcher_FileChange verifies that file changes trigger reload.
func TestJSONRepository_StartWatcher_FileChange(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 500, // Older than disk
		dirty:      false,
		doc:        DataDocument{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = jsonRepo.StartWatcher(ctx, cache)
	if err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Modify the file
	doc.Metadata.LastUpdate = 2000
	doc.Containers[0].FriendlyName = "Updated Container"
	data, _ = json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update test file: %v", err)
	}

	// Wait for debounce + processing
	time.Sleep(400 * time.Millisecond)

	if !cache.IsReplaced() {
		t.Error("expected cache to be replaced after file change")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestJSONRepository_MakeWatcherCallback_LoadError verifies behavior when load fails.
func TestJSONRepository_MakeWatcherCallback_LoadError(t *testing.T) {
	// Create repo pointing to non-existent file
	repo, _ := NewJSONRepository("/nonexistent/path/config.json")
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000,
		dirty:      false,
		doc:        DataDocument{},
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	// Should not panic, just log error
	callback()

	if cache.IsReplaced() {
		t.Error("expected cache NOT to be replaced when load fails")
	}
}

// TestJSONRepository_MakeWatcherCallback_DifferentContentSameTimestamp verifies
// replacement when content differs but timestamp is the same.
func TestJSONRepository_MakeWatcherCallback_DifferentContentSameTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	// Cache has same timestamp but different content
	cacheDoc := createTestDataDocument()
	cacheDoc.Metadata.LastUpdate = 1000
	cacheDoc.Containers[0].FriendlyName = "Different Name"

	cache := &MockCacheStore{
		lastUpdate: 1000, // Same as disk
		dirty:      false,
		doc:        cacheDoc, // Different content
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	callback()

	if !cache.IsReplaced() {
		t.Error("expected cache to be replaced when content differs")
	}
}

// MockCacheStoreWithSnapshotError is a mock that returns error on Snapshot
type MockCacheStoreWithSnapshotError struct {
	lastUpdate int64
	dirty      bool
}

func (m *MockCacheStoreWithSnapshotError) GetLastUpdate() int64 {
	return m.lastUpdate
}

func (m *MockCacheStoreWithSnapshotError) IsDirty() bool {
	return m.dirty
}

func (m *MockCacheStoreWithSnapshotError) Snapshot() (DataDocument, error) {
	return DataDocument{}, errors.New("snapshot error")
}

func (m *MockCacheStoreWithSnapshotError) Replace(doc DataDocument) error {
	return nil
}

// TestJSONRepository_MakeWatcherCallback_SnapshotError verifies behavior when snapshot fails.
func TestJSONRepository_MakeWatcherCallback_SnapshotError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStoreWithSnapshotError{
		lastUpdate: 1000, // Same as disk, will trigger snapshot
		dirty:      false,
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	// Should not panic, just log error
	callback()
}

// MockCacheStoreWithReplaceError is a mock that returns error on Replace
type MockCacheStoreWithReplaceError struct {
	lastUpdate int64
	dirty      bool
	doc        DataDocument
}

func (m *MockCacheStoreWithReplaceError) GetLastUpdate() int64 {
	return m.lastUpdate
}

func (m *MockCacheStoreWithReplaceError) IsDirty() bool {
	return m.dirty
}

func (m *MockCacheStoreWithReplaceError) Snapshot() (DataDocument, error) {
	return m.doc, nil
}

func (m *MockCacheStoreWithReplaceError) Replace(doc DataDocument) error {
	return errors.New("replace error")
}

// TestJSONRepository_MakeWatcherCallback_ReplaceError verifies behavior when replace fails.
func TestJSONRepository_MakeWatcherCallback_ReplaceError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 2000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, _ := NewJSONRepository(configPath)
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStoreWithReplaceError{
		lastUpdate: 1000, // Older than disk
		dirty:      false,
		doc:        DataDocument{},
	}

	callback := jsonRepo.MakeWatcherCallback(cache)
	// Should not panic, just log error
	callback()
}

// TestJSONRepository_Save_ToNonExistentDirectory verifies error handling
// when saving to a directory that doesn't exist.
func TestJSONRepository_Save_ToNonExistentDirectory(t *testing.T) {
	repo, _ := NewJSONRepository("/nonexistent/dir/config.json")

	doc := createTestDataDocument()
	err := repo.Save(context.Background(), &doc)
	if err == nil {
		t.Error("expected error when saving to non-existent directory")
	}
}

// TestJSONRepository_StartWatcher_InvalidDirectory verifies error when watching
// a non-existent directory.
func TestJSONRepository_StartWatcher_InvalidDirectory(t *testing.T) {
	repo, _ := NewJSONRepository("/nonexistent/dir/config.json")
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 1000,
		dirty:      false,
		doc:        DataDocument{},
	}

	ctx := context.Background()
	err := jsonRepo.StartWatcher(ctx, cache)
	if err == nil {
		t.Error("expected error when watching non-existent directory")
	}
}

// TestJSONRepository_StartWatcher_RemoveEvent verifies handling of remove events.
func TestJSONRepository_StartWatcher_RemoveEvent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 500,
		dirty:      false,
		doc:        DataDocument{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = jsonRepo.StartWatcher(ctx, cache)
	if err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Remove the file
	_ = os.Remove(configPath)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Recreate the file with new content
	doc.Metadata.LastUpdate = 2000
	data, _ = json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to recreate test file: %v", err)
	}

	// Wait for debounce + processing
	time.Sleep(400 * time.Millisecond)

	if !cache.IsReplaced() {
		t.Error("expected cache to be replaced after file recreated")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestJSONRepository_StartWatcher_IgnoresOtherFiles verifies that changes to
// other files in the same directory are ignored.
func TestJSONRepository_StartWatcher_IgnoresOtherFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	otherPath := filepath.Join(tmpDir, "other.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jsonRepo := repo.(*JSONRepository)

	cache := &MockCacheStore{
		lastUpdate: 500,
		dirty:      false,
		doc:        DataDocument{},
		replaced:   false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = jsonRepo.StartWatcher(ctx, cache)
	if err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Write to a different file
	if err := os.WriteFile(otherPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create other file: %v", err)
	}

	// Wait to ensure no spurious reload
	time.Sleep(400 * time.Millisecond)

	if cache.IsReplaced() {
		t.Error("expected cache NOT to be replaced when other file changes")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestJSONRepository_StartWatcher_DebounceMultipleEvents verifies that multiple
// rapid events are debounced into a single reload.
func TestJSONRepository_StartWatcher_DebounceMultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	doc := createTestDataDocument()
	doc.Metadata.LastUpdate = 1000
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	repo, err := NewJSONRepository(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jsonRepo := repo.(*JSONRepository)

	replaceCount := 0
	cache := &MockCacheStoreCountingReplaces{
		lastUpdate:   500,
		dirty:        false,
		doc:          DataDocument{},
		replaceCount: &replaceCount,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = jsonRepo.StartWatcher(ctx, cache)
	if err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Rapid successive writes
	for i := 0; i < 5; i++ {
		doc.Metadata.LastUpdate = int64(2000 + i)
		data, _ = json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			t.Fatalf("failed to update test file: %v", err)
		}
		time.Sleep(50 * time.Millisecond) // Less than debounce time
	}

	// Wait for debounce + processing
	time.Sleep(400 * time.Millisecond)

	// Should have been called only once due to debouncing
	if cache.GetReplaceCount() > 2 { // Allow some tolerance
		t.Errorf("expected debouncing to reduce reload count, got %d replaces", cache.GetReplaceCount())
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

// MockCacheStoreCountingReplaces counts how many times Replace is called
type MockCacheStoreCountingReplaces struct {
	mu           sync.RWMutex
	lastUpdate   int64
	dirty        bool
	doc          DataDocument
	replaceCount *int
}

func (m *MockCacheStoreCountingReplaces) GetLastUpdate() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

func (m *MockCacheStoreCountingReplaces) IsDirty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dirty
}

func (m *MockCacheStoreCountingReplaces) Snapshot() (DataDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.doc, nil
}

func (m *MockCacheStoreCountingReplaces) Replace(doc DataDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.replaceCount++
	m.doc = doc
	m.lastUpdate = doc.Metadata.LastUpdate
	return nil
}

func (m *MockCacheStoreCountingReplaces) GetReplaceCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.replaceCount
}
