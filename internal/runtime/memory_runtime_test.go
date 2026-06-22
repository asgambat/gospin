package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/bassista/go_spin/internal/repository"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestNewMemoryRuntime(t *testing.T) {
	mr := NewMemoryRuntime()
	if mr == nil {
		t.Fatal("expected MemoryRuntime to be created")
	}
	if mr.running == nil {
		t.Error("expected running map to be initialized")
	}
}

func TestNewMemoryRuntimeFromDocument(t *testing.T) {
	doc := repository.DataDocument{
		Containers: []repository.Container{
			{Name: "running-container", Running: boolPtr(true)},
			{Name: "stopped-container", Running: boolPtr(false)},
			{Name: ""},           // Empty name should be skipped
			{Name: "no-running"}, // No Running field
		},
	}

	mr := NewMemoryRuntimeFromDocument(doc)

	running1, _ := mr.IsRunning(context.Background(), "running-container")
	if !running1 {
		t.Error("expected running-container to be running")
	}

	running2, _ := mr.IsRunning(context.Background(), "stopped-container")
	if running2 {
		t.Error("expected stopped-container to not be running")
	}
}

func TestMemoryRuntime_IsRunning(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	// Unknown container should return false
	running, err := mr.IsRunning(ctx, "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running {
		t.Error("expected unknown container to not be running")
	}
}

func TestMemoryRuntime_ListContainers(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	// empty initially
	list, err := mr.ListContainers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %v", list)
	}

	// add some containers
	if startErr := mr.Start(ctx, "Alpha"); startErr != nil {
		t.Fatalf("start failed: %v", startErr)
	}
	if startErr := mr.Start(ctx, "beta"); startErr != nil {
		t.Fatalf("start failed: %v", startErr)
	}

	list, err = mr.ListContainers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// verify case-sensitive names present
	want := map[string]bool{"Alpha": true, "beta": true}
	if len(list) != len(want) {
		t.Fatalf("expected %d containers, got %d: %v", len(want), len(list), list)
	}
	for _, n := range list {
		if !want[n] {
			t.Fatalf("unexpected container name: %s", n)
		}
	}
}

func TestMemoryRuntime_Start(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	err := mr.Start(ctx, "container1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	running, _ := mr.IsRunning(ctx, "container1")
	if !running {
		t.Error("expected container1 to be running after Start")
	}
}

func TestMemoryRuntime_Stop(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	// Start then stop
	_ = mr.Start(ctx, "container1")
	err := mr.Stop(ctx, "container1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	running, _ := mr.IsRunning(ctx, "container1")
	if running {
		t.Error("expected container1 to not be running after Stop")
	}
}

func TestMemoryRuntime_StopUnknown(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	// Stop unknown container should work (sets to false)
	err := mr.Stop(ctx, "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	running, _ := mr.IsRunning(ctx, "unknown")
	if running {
		t.Error("expected unknown container to not be running")
	}
}

func TestMemoryRuntime_Concurrency(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mr.IsRunning(ctx, "container")
		}()
	}

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = mr.Start(ctx, "container")
			} else {
				_ = mr.Stop(ctx, "container")
			}
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or panic, concurrency is handled correctly
}

func TestMemoryRuntime_Stats(t *testing.T) {
	mr := NewMemoryRuntime()
	ctx := context.Background()

	// Memory runtime should return zero stats
	stats, err := mr.Stats(ctx, "any-container")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.CPUPercent != 0.0 {
		t.Errorf("expected CPUPercent 0, got %v", stats.CPUPercent)
	}
	if stats.MemoryMB != 0.0 {
		t.Errorf("expected MemoryMB 0, got %v", stats.MemoryMB)
	}
}
