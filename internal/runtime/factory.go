package runtime

import (
	"fmt"

	"github.com/asgambat/gospin/internal/repository"
)

const (
	RuntimeTypeDocker = "docker"
	RuntimeTypeMemory = "memory"
)

// NewRuntimeFromConfig creates a ContainerRuntime based on the runtime type.
// If runtimeType is "memory", it creates a MemoryRuntime initialized from the
// document and mirroring registry (which may be nil).
// If runtimeType is "docker" (default), it creates a DockerRuntime.
func NewRuntimeFromConfig(runtimeType string, doc *repository.DataDocument, registry ContainerRegistry) (ContainerRuntime, error) {
	switch runtimeType {
	case RuntimeTypeMemory:
		return NewMemoryRuntimeWithRegistry(doc, registry), nil
	case RuntimeTypeDocker, "":
		return NewDockerRuntime()
	default:
		return nil, fmt.Errorf("unknown runtime type: %s (supported: %s, %s)", runtimeType, RuntimeTypeDocker, RuntimeTypeMemory)
	}
}
