package runtime

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/repository"
)

// ContainerRegistry exposes the containers registered in the data document.
// The memory runtime has no container daemon to ask, so it mirrors the registry:
// a registered container exists in the runtime even if it was added after start-up.
// It is satisfied by the cache store.
type ContainerRegistry interface {
	Snapshot() (repository.DataDocument, error)
}

// MemoryRuntime is a temporary ContainerRuntime implementation that keeps state in memory.
// It is useful while the Docker-socket implementation is not available to execute tests or other development tasks.
type MemoryRuntime struct {
	running  map[string]bool
	registry ContainerRegistry
	mu       sync.RWMutex
}

func NewMemoryRuntime() *MemoryRuntime {
	return &MemoryRuntime{running: map[string]bool{}}
}

func NewMemoryRuntimeFromDocument(doc repository.DataDocument) *MemoryRuntime {
	mr := NewMemoryRuntime()
	for _, c := range doc.Containers {
		if c.Name == "" {
			continue
		}
		if c.Running != nil {
			mr.running[c.Name] = *c.Running
		}
	}
	return mr
}

// NewMemoryRuntimeWithRegistry creates a memory runtime that seeds its running
// state from doc (which may be nil) and answers ListContainers from the live
// registry. Without the registry, containers created after start-up would be
// reported as missing and the orphan cleanup would delete them.
func NewMemoryRuntimeWithRegistry(doc *repository.DataDocument, registry ContainerRegistry) *MemoryRuntime {
	mr := NewMemoryRuntime()
	if doc != nil {
		mr = NewMemoryRuntimeFromDocument(*doc)
	}
	mr.registry = registry
	return mr
}

func (m *MemoryRuntime) IsRunning(_ context.Context, containerName string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	isRunning := m.running[containerName]
	logger.WithComponent("memory-runtime").Debugf("checking if container is running: %s, result: %v", containerName, isRunning)
	return isRunning, nil
}

func (m *MemoryRuntime) Start(_ context.Context, containerName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	logger.WithComponent("memory-runtime").Debugf("starting container: %s", containerName)
	m.running[containerName] = true
	return nil
}

func (m *MemoryRuntime) Stop(_ context.Context, containerName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	logger.WithComponent("memory-runtime").Debugf("stopping container: %s", containerName)
	m.running[containerName] = false
	return nil
}

// ListContainers returns the names of containers known to the memory runtime:
// the ones it tracks plus every container registered in the data document. Names
// are returned exactly as they are stored (case-sensitive), sorted and without
// duplicates.
func (m *MemoryRuntime) ListContainers(_ context.Context) ([]string, error) {
	m.mu.RLock()
	names := make([]string, 0, len(m.running))
	for n := range m.running {
		names = append(names, n)
	}
	m.mu.RUnlock()

	if m.registry != nil {
		doc, err := m.registry.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("snapshot registry: %w", err)
		}
		for _, c := range doc.Containers {
			if c.Name != "" {
				names = append(names, c.Name)
			}
		}
	}

	slices.Sort(names)
	names = slices.Compact(names)
	logger.WithComponent("memory-runtime").Debugf("listing containers: %v", names)
	return names, nil
}

// Stats returns simulated CPU and memory usage statistics for a container.
// In the memory runtime, this returns zero values as no actual container exists.
func (m *MemoryRuntime) Stats(_ context.Context, containerName string) (ContainerStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	logger.WithComponent("memory-runtime").Debugf("getting stats for container: %s", containerName)
	// Memory runtime returns zero stats since there is no real container
	return ContainerStats{
		CPUPercent: 0.0,
		MemoryMB:   0.0,
	}, nil
}
