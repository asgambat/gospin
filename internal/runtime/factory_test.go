package runtime

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/asgambat/gospin/internal/repository"
)

func TestNewRuntimeFromConfig_Memory(t *testing.T) {
	rt, err := NewRuntimeFromConfig(RuntimeTypeMemory, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt == nil {
		t.Fatal("expected runtime to be created")
	}
	if _, ok := rt.(*MemoryRuntime); !ok {
		t.Error("expected MemoryRuntime type")
	}
}

func TestNewRuntimeFromConfig_MemoryWithDocument(t *testing.T) {
	doc := &repository.DataDocument{
		Containers: []repository.Container{
			{Name: "c1", Running: boolPtr(true)},
		},
	}

	rt, err := NewRuntimeFromConfig(RuntimeTypeMemory, doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mr, ok := rt.(*MemoryRuntime)
	if !ok {
		t.Fatal("expected MemoryRuntime type")
	}

	// Verify container state was loaded
	running, _ := mr.IsRunning(context.Background(), "c1")
	if !running {
		t.Error("expected c1 to be running from document")
	}
}

func TestNewRuntimeFromConfig_Docker(t *testing.T) {
	// This test may fail if Docker is not available
	// We just check that it doesn't return an unknown runtime error
	_, err := NewRuntimeFromConfig(RuntimeTypeDocker, nil, nil)
	// If Docker is not available, we expect an error, but not "unknown runtime type"
	if err != nil {
		if err.Error() == "unknown runtime type: docker (supported: docker, memory)" {
			t.Error("docker should be a recognized runtime type")
		}
		// Other errors (like Docker not available) are acceptable in test environment
		t.Logf("Docker runtime error (may be expected if Docker not running): %v", err)
	}
}

func TestNewRuntimeFromConfig_EmptyString(t *testing.T) {
	// Empty string should default to Docker
	_, err := NewRuntimeFromConfig("", nil, nil)
	if err != nil {
		// If Docker is not available, we expect an error, but not "unknown runtime type"
		if err.Error() == "unknown runtime type:  (supported: docker, memory)" {
			t.Error("empty string should default to docker")
		}
		t.Logf("Docker runtime error (may be expected if Docker not running): %v", err)
	}
}

func TestNewRuntimeFromConfig_UnknownType(t *testing.T) {
	_, err := NewRuntimeFromConfig("unknown-runtime", nil, nil)
	if err == nil {
		t.Error("expected error for unknown runtime type")
	}
}

// ==================== Concurrency Tests ====================

// TestNewRuntimeFromConfig_ConcurrentCreation verifies that creating multiple
// runtimes concurrently doesn't cause race conditions.
func TestNewRuntimeFromConfig_ConcurrentCreation(t *testing.T) {
	var wg sync.WaitGroup
	const numGoroutines = 50

	doc := &repository.DataDocument{
		Containers: []repository.Container{
			{Name: "c1", Running: boolPtr(true)},
			{Name: "c2", Running: boolPtr(false)},
		},
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rt, err := NewRuntimeFromConfig(RuntimeTypeMemory, doc, nil)
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			if rt == nil {
				t.Errorf("goroutine %d: expected runtime to be created", idx)
				return
			}

			// Verify it's the right type and works
			mr, ok := rt.(*MemoryRuntime)
			if !ok {
				t.Errorf("goroutine %d: expected MemoryRuntime type", idx)
				return
			}

			// Verify initial state
			running, _ := mr.IsRunning(context.Background(), "c1")
			if !running {
				t.Errorf("goroutine %d: expected c1 to be running", idx)
			}
		}(i)
	}

	wg.Wait()
}

// TestMemoryRuntime_ConcurrentOperations verifies that concurrent Start/Stop/IsRunning
// operations on MemoryRuntime are thread-safe.
func TestMemoryRuntime_ConcurrentOperations(t *testing.T) {
	doc := &repository.DataDocument{
		Containers: []repository.Container{
			{Name: "c1", Running: boolPtr(false)},
			{Name: "c2", Running: boolPtr(false)},
			{Name: "c3", Running: boolPtr(false)},
		},
	}

	rt, err := NewRuntimeFromConfig(RuntimeTypeMemory, doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mr := rt.(*MemoryRuntime)

	var wg sync.WaitGroup
	ctx := context.Background()
	const numOperations = 100

	// Concurrent starts, stops, and status checks
	for i := 0; i < numOperations; i++ {
		wg.Add(3)
		var containerName string
		switch i % 3 {
		case 1:
			containerName = "c2"
		case 2:
			containerName = "c3"
		default:
			containerName = "c1"
		}

		// Concurrent Start
		go func(name string) {
			defer wg.Done()
			_ = mr.Start(ctx, name)
		}(containerName)

		// Concurrent Stop
		go func(name string) {
			defer wg.Done()
			_ = mr.Stop(ctx, name)
		}(containerName)

		// Concurrent IsRunning
		go func(name string) {
			defer wg.Done()
			_, _ = mr.IsRunning(ctx, name)
		}(containerName)
	}

	wg.Wait()
}

func TestNewRuntimeFromConfig_MemoryWithRegistry(t *testing.T) {
	registry := &registryStub{containers: []repository.Container{{Name: "c1"}}}

	rt, err := NewRuntimeFromConfig(RuntimeTypeMemory, nil, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names, err := rt.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if !slices.Equal(names, []string{"c1"}) {
		t.Errorf("ListContainers() = %v, want [c1]", names)
	}
}
