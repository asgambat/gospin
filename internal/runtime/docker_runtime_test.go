package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDockerClient is a mock implementation of DockerClient interface
type MockDockerClient struct {
	mock.Mock
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerInspectResult), args.Error(1)
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerStartResult), args.Error(1)
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerStopResult), args.Error(1)
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	args := m.Called(ctx, options)
	return args.Get(0).(client.ContainerListResult), args.Error(1)
}

func (m *MockDockerClient) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerStatsResult), args.Error(1)
}

func TestNewDockerRuntimeWithClient(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)
	assert.NotNil(t, dr)
	assert.Equal(t, mockClient, dr.cli)
}

func TestDockerRuntime_IsRunning_Running(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	inspectResult := client.ContainerInspectResult{
		Container: container.InspectResponse{
			State: &container.State{
				Running: true,
			},
		},
	}

	mockClient.On("ContainerInspect", ctx, containerName, client.ContainerInspectOptions{}).Return(inspectResult, nil)

	running, err := dr.IsRunning(ctx, containerName)
	assert.NoError(t, err)
	assert.True(t, running)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_IsRunning_NotRunning(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	inspectResult := client.ContainerInspectResult{
		Container: container.InspectResponse{
			State: &container.State{
				Running: false,
			},
		},
	}

	mockClient.On("ContainerInspect", ctx, containerName, client.ContainerInspectOptions{}).Return(inspectResult, nil)

	running, err := dr.IsRunning(ctx, containerName)
	assert.NoError(t, err)
	assert.False(t, running)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_IsRunning_NilState(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	inspectResult := client.ContainerInspectResult{
		Container: container.InspectResponse{
			State: nil,
		},
	}

	mockClient.On("ContainerInspect", ctx, containerName, client.ContainerInspectOptions{}).Return(inspectResult, nil)

	running, err := dr.IsRunning(ctx, containerName)
	assert.NoError(t, err)
	assert.False(t, running)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_IsRunning_ContainerNotFound(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "non-existent-container"

	// Create a NotFound error using the errdefs package
	notFoundErr := errdefs.ErrNotFound

	mockClient.On("ContainerInspect", ctx, containerName, client.ContainerInspectOptions{}).
		Return(client.ContainerInspectResult{}, notFoundErr)

	running, err := dr.IsRunning(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.False(t, running)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_IsRunning_InspectError(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerInspect", ctx, containerName, client.ContainerInspectOptions{}).
		Return(client.ContainerInspectResult{}, errors.New("inspect error"))

	running, err := dr.IsRunning(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error checking status of container")
	assert.False(t, running)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Start_Success(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerStart", ctx, containerName, client.ContainerStartOptions{}).
		Return(client.ContainerStartResult{}, nil)

	err := dr.Start(ctx, containerName)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Start_Error(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerStart", ctx, containerName, client.ContainerStartOptions{}).
		Return(client.ContainerStartResult{}, errors.New("start failed"))

	err := dr.Start(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error starting container")
	assert.Contains(t, err.Error(), "start failed")
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Stop_Success(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerStop", ctx, containerName, client.ContainerStopOptions{}).
		Return(client.ContainerStopResult{}, nil)

	err := dr.Stop(ctx, containerName)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Stop_Error(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerStop", ctx, containerName, client.ContainerStopOptions{}).
		Return(client.ContainerStopResult{}, errors.New("stop failed"))

	err := dr.Stop(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error stopping container")
	assert.Contains(t, err.Error(), "stop failed")
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_ListContainers_Success(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()

	listResult := client.ContainerListResult{
		Items: []container.Summary{
			{Names: []string{"/MyApp"}},
			{Names: []string{"/another-container"}},
		},
	}

	mockClient.On("ContainerList", ctx, client.ContainerListOptions{All: true}).Return(listResult, nil)

	names, err := dr.ListContainers(ctx)
	assert.NoError(t, err)
	// Names are sorted alphabetically, case-insensitive
	assert.Equal(t, []string{"another-container", "MyApp"}, names)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_ListContainers_Empty(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()

	listResult := client.ContainerListResult{Items: []container.Summary{}}

	mockClient.On("ContainerList", ctx, client.ContainerListOptions{All: true}).Return(listResult, nil)

	names, err := dr.ListContainers(ctx)
	assert.NoError(t, err)
	assert.Empty(t, names)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_ListContainers_Error(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()

	mockClient.On("ContainerList", ctx, client.ContainerListOptions{All: true}).Return(client.ContainerListResult{}, errors.New("list failed"))

	names, err := dr.ListContainers(ctx)
	assert.Error(t, err)
	assert.Nil(t, names)
	assert.Contains(t, err.Error(), "error listing containers")
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Stats_Success(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	// Create mock stats response JSON
	statsResponse := container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 1000000000, // 1 second in nanoseconds
			},
			SystemUsage: 10000000000, // 10 seconds
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // 0.5 seconds in nanoseconds
			},
			SystemUsage: 9000000000, // 9 seconds
		},
		MemoryStats: container.MemoryStats{
			Usage: 104857600, // 100 MB in bytes
		},
	}

	statsJSON, _ := json.Marshal(statsResponse)
	mockBody := io.NopCloser(bytes.NewReader(statsJSON))

	mockClient.On("ContainerStats", ctx, containerName, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	}).Return(client.ContainerStatsResult{Body: mockBody}, nil)

	stats, err := dr.Stats(ctx, containerName)
	assert.NoError(t, err)
	assert.InDelta(t, 100.0, stats.MemoryMB, 0.01)
	assert.Greater(t, stats.CPUPercent, 0.0)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Stats_NotFound(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "nonexistent"

	mockClient.On("ContainerStats", ctx, containerName, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	}).Return(client.ContainerStatsResult{}, errdefs.ErrNotFound)

	stats, err := dr.Stats(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, ContainerStats{}, stats)
	mockClient.AssertExpectations(t)
}

func TestDockerRuntime_Stats_Error(t *testing.T) {
	mockClient := &MockDockerClient{}
	dr := NewDockerRuntimeWithClient(mockClient)

	ctx := context.Background()
	containerName := "test-container"

	mockClient.On("ContainerStats", ctx, containerName, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	}).Return(client.ContainerStatsResult{}, errors.New("stats failed"))

	stats, err := dr.Stats(ctx, containerName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error getting stats")
	assert.Equal(t, ContainerStats{}, stats)
	mockClient.AssertExpectations(t)
}

func TestCalculateCPUPercent_CapAt100Percent(t *testing.T) {
	// Test case: 4 cores, 50% CPU usage -> should return 50%, not 200%
	stats := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 1000000000, // 1 second in nanoseconds
			},
			SystemUsage: 10000000000, // 10 seconds
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // 0.5 seconds in nanoseconds
			},
			SystemUsage: 9000000000, // 9 seconds
		},
	}

	cpuPercent := calculateCPUPercent(stats)
	// Expected: ((1s - 0.5s) / (10s - 9s)) * 4 * 100 = 200%, but capped at 100%
	assert.Equal(t, 100.0, cpuPercent, "CPU percentage should be capped at 100%")
}

func TestCalculateCPUPercent_NormalCase(t *testing.T) {
	// Test case: 4 cores, 25% CPU usage -> should return 25%
	stats := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 750000000, // 0.75 seconds in nanoseconds
			},
			SystemUsage: 10000000000, // 10 seconds
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // 0.5 seconds in nanoseconds
			},
			SystemUsage: 9000000000, // 9 seconds
		},
	}

	cpuPercent := calculateCPUPercent(stats)
	// Expected: ((0.75s - 0.5s) / (10s - 9s)) * 4 * 100 = 100%
	assert.Equal(t, 100.0, cpuPercent, "CPU percentage should be 100%")
}

func TestCalculateCPUPercent_ZeroCase(t *testing.T) {
	// Test case: no CPU usage -> should return 0%
	stats := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // No change from previous
			},
			SystemUsage: 10000000000, // 10 seconds
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // Same as current
			},
			SystemUsage: 9000000000, // 9 seconds
		},
	}

	cpuPercent := calculateCPUPercent(stats)
	assert.Equal(t, 0.0, cpuPercent, "CPU percentage should be 0% when no usage")
}
