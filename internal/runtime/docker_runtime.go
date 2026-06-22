package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/bassista/go_spin/internal/logger"
)

// DockerClient defines the interface for Docker client operations used by DockerRuntime.
// This interface allows for mocking in tests.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
}

type DockerRuntime struct {
	cli DockerClient
}

func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("error creating Docker client: %w", err)
	}
	return &DockerRuntime{cli: cli}, nil
}

// NewDockerRuntimeWithClient creates a DockerRuntime with a custom client.
// This is primarily used for testing purposes.
func NewDockerRuntimeWithClient(cli DockerClient) *DockerRuntime {
	return &DockerRuntime{cli: cli}
}

func (d *DockerRuntime) IsRunning(ctx context.Context, containerName string) (bool, error) {
	logger.WithComponent("docker").Debugf("checking if container is running: %s", containerName)
	inspect, err := d.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			logger.WithComponent("docker").Debugf("container not found: %s", containerName)
			return false, fmt.Errorf("container %s not found", containerName)
		}
		logger.WithComponent("docker").Errorf("failed to inspect container %s: %v", containerName, err)
		return false, fmt.Errorf("error checking status of container %s: %w", containerName, err)
	}

	if inspect.Container.State == nil {
		logger.WithComponent("docker").Warnf("container state is null: %s", containerName)
		return false, nil
	}
	logger.WithComponent("docker").Debugf("container isRunning %t for : %s", inspect.Container.State.Running, containerName)
	logger.WithComponent("docker").Tracef("container status %s for : %s", inspect.Container.State.Status, containerName)
	return inspect.Container.State.Running, nil
}

func (d *DockerRuntime) Start(ctx context.Context, containerName string) error {
	logger.WithComponent("docker").Debugf("starting container: %s", containerName)
	_, err := d.cli.ContainerStart(ctx, containerName, client.ContainerStartOptions{})
	if err != nil {
		logger.WithComponent("docker").Errorf("failed to start container %s: %v", containerName, err)
		return fmt.Errorf("error starting container %s: %w", containerName, err)
	}
	logger.WithComponent("docker").Debugf("container started successfully: %s", containerName)
	return nil
}

func (d *DockerRuntime) Stop(ctx context.Context, containerName string) error {
	logger.WithComponent("docker").Debugf("stopping container: %s", containerName)
	_, err := d.cli.ContainerStop(ctx, containerName, client.ContainerStopOptions{})
	if err != nil {
		logger.WithComponent("docker").Errorf("failed to stop container %s: %v", containerName, err)
		return fmt.Errorf("error stopping container %s: %w", containerName, err)
	}
	logger.WithComponent("docker").Debugf("container stopped successfully: %s", containerName)
	return nil
}

// ListContainers returns a list of container names from the Docker daemon.
// Names are returned exactly as stored (case-sensitive), sorted alphabetically (case-insensitive).
// This includes all containers (running and stopped).
func (d *DockerRuntime) ListContainers(ctx context.Context) ([]string, error) {
	logger.WithComponent("docker").Debugf("listing containers")
	result, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		logger.WithComponent("docker").Errorf("failed to list containers: %v", err)
		return nil, fmt.Errorf("error listing containers: %w", err)
	}
	names := make([]string, 0, len(result.Items))
	for _, c := range result.Items {
		if len(c.Names) > 0 {
			// Container names are prefixed with '/', strip it
			name := strings.TrimPrefix(c.Names[0], "/")
			names = append(names, name)
		}
	}
	// Sort names alphabetically, case-insensitive
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	logger.WithComponent("docker").Debugf("listed %d containers: %v", len(names), names)
	return names, nil
}

// Stats returns CPU and memory usage statistics for a container.
func (d *DockerRuntime) Stats(ctx context.Context, containerName string) (ContainerStats, error) {
	logger.WithComponent("docker").Debugf("getting stats for container: %s", containerName)

	result, err := d.cli.ContainerStats(ctx, containerName, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			logger.WithComponent("docker").Debugf("container not found: %s", containerName)
			return ContainerStats{}, fmt.Errorf("container %s not found", containerName)
		}
		logger.WithComponent("docker").Errorf("failed to get stats for container %s: %v", containerName, err)
		return ContainerStats{}, fmt.Errorf("error getting stats for container %s: %w", containerName, err)
	}
	defer func() {
		if cerr := result.Body.Close(); cerr != nil {
			logger.WithComponent("docker").Errorf("failed to close stats response body for container %s: %v", containerName, cerr)
		}
	}()

	var statsResponse container.StatsResponse
	if err := json.NewDecoder(result.Body).Decode(&statsResponse); err != nil {
		logger.WithComponent("docker").Errorf("failed to decode stats response for container %s: %v", containerName, err)
		return ContainerStats{}, fmt.Errorf("error decoding stats for container %s: %w", containerName, err)
	}

	stats := ContainerStats{
		CPUPercent: calculateCPUPercent(&statsResponse),
		MemoryMB:   float64(statsResponse.MemoryStats.Usage) / (1024 * 1024),
	}

	logger.WithComponent("docker").Debugf("container %s stats: CPU=%.2f%%, Memory=%.2f MB", containerName, stats.CPUPercent, stats.MemoryMB)
	return stats, nil
}

// calculateCPUPercent calculates the CPU usage percentage from Docker stats.
func calculateCPUPercent(stats *container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent := (cpuDelta / systemDelta) * float64(stats.CPUStats.OnlineCPUs) * 100.0
		// Cap CPU percentage at 100% to avoid values > 100% on multi-core systems
		return math.Min(cpuPercent, 100.0)
	}
	return 0.0
}
