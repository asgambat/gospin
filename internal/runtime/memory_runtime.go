package runtime

import (
	"context"
	"sync"

	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
)

// MemoryRuntime is a temporary ContainerRuntime implementation that keeps state in memory.
// It is useful while the Docker-socket implementation is not available to execute tests or other development tasks.
type MemoryRuntime struct {
	running map[string]bool
	mu      sync.RWMutex
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

// ListContainers returns the names of containers known to the memory runtime.
// Names are returned exactly as they are stored (case-sensitive).
func (m *MemoryRuntime) ListContainers(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.running))
	for n := range m.running {
		names = append(names, n)
	}
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
