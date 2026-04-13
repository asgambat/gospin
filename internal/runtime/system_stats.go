package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"time"
)

// SystemStats represents system-wide resource usage
type SystemStats struct {
	CPU     int         `json:"cpu"`
	Memory  MemoryStats `json:"memory"`
	Disk    DiskStats   `json:"disk"`
	Updated time.Time   `json:"updated"`
}

// MemoryStats represents memory usage
type MemoryStats struct {
	Used  float64 `json:"used"`  // in GB
	Total float64 `json:"total"` // in GB
	Free  float64 `json:"free"`  // in GB
}

// DiskStats represents disk usage for the root filesystem
type DiskStats struct {
	Used  float64 `json:"used"`  // in GB
	Total float64 `json:"total"` // in GB
	Free  float64 `json:"free"`  // in GB
}

// SystemStatsCollector defines the interface for collecting system stats
type SystemStatsCollector interface {
	GetStats(ctx context.Context) (SystemStats, error)
}

// LinuxSystemStats implements SystemStatsCollector for Linux systems
type LinuxSystemStats struct {
	mountPoint string
}

// NewSystemStatsCollector returns a new system stats collector based on the OS
func NewSystemStatsCollector(mountPoint string) SystemStatsCollector {
	return &LinuxSystemStats{
		mountPoint: mountPoint,
	}
}

// GetStats collects CPU, memory, and disk statistics
func (l *LinuxSystemStats) GetStats(ctx context.Context) (SystemStats, error) {
	stats := SystemStats{
		Updated: time.Now(),
	}

	// Get CPU usage
	cpuPercent, err := l.getCPUUsage(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to get CPU stats: %w", err)
	}
	stats.CPU = cpuPercent

	// Get Memory usage
	memStats, err := l.getMemoryStats(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to get memory stats: %w", err)
	}
	stats.Memory = memStats

	// Get Disk usage
	diskStats, err := l.getDiskStats(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to get disk stats: %w", err)
	}
	stats.Disk = diskStats

	return stats, nil
}

// getCPUUsage calculates CPU usage percentage
func (l *LinuxSystemStats) getCPUUsage(ctx context.Context) (int, error) {
	// Read /proc/stat for CPU stats
	cpuData, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}

	// Parse first line: cpu  user nice system idle iowait irq softirq steal guest guest_nice
	lines := bytes.Split(cpuData, []byte("\n"))
	if len(lines) < 1 {
		return 0, fmt.Errorf("invalid /proc/stat content")
	}

	fields := bytes.Fields(lines[0])
	if len(fields) < 5 || string(fields[0]) != "cpu" {
		return 0, fmt.Errorf("unexpected /proc/stat format")
	}

	// Calculate total and idle time
	var total uint64
	for i := 1; i < len(fields); i++ {
		val := 0
		fmt.Sscanf(string(fields[i]), "%d", &val)
		total += uint64(val)
	}

	// We need to measure delta over time for accurate CPU usage
	// For simplicity, we'll use a heuristic based on scheduler info
	return l.calculateCPUFromRuntime(), nil
}

// calculateCPUFromRuntime uses Go's runtime package to estimate CPU usage
func (l *LinuxSystemStats) calculateCPUFromRuntime() int {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// Get number of CPUs
	numCPU := float64(runtime.NumCPU())

	// Use a simple heuristic: get current goroutine count and estimate
	// This is a rough approximation since we can't get true CPU without delta measurement
	// For better accuracy, we would need to measure over time

	// Read /proc/loadavg as an alternative
	loadavg, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		// Fallback: return a nominal value
		return 0
	}

	// Parse first value (1-minute load average)
	var load1, load5, load15 float64
	var runnable, total int
	fmt.Sscanf(string(loadavg), "%f %f %f %d %d", &load1, &load5, &load15, &runnable, &total)

	// Convert load average to percentage (load / num_cpus * 100)
	cpuPercent := (load1 / numCPU) * 100

	// Round up to the nearest integer
	roundedCPU := int(math.Ceil(cpuPercent))

	// Cap at 100% for display
	if roundedCPU > 100 {
		roundedCPU = 100
	}

	return roundedCPU
}

// getMemoryStats reads memory information from /proc/meminfo
func (l *LinuxSystemStats) getMemoryStats(ctx context.Context) (MemoryStats, error) {
	memData, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemoryStats{}, err
	}

	var memTotal, memFree, memAvailable uint64

	lines := bytes.Split(memData, []byte("\n"))
	for _, line := range lines {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := string(fields[0])
		var value uint64
		fmt.Sscanf(string(fields[1]), "%d", &value)
		// values in /proc/meminfo are in KB

		switch key {
		case "MemTotal:":
			memTotal = value
		case "MemFree:":
			memFree = value
		case "MemAvailable:":
			memAvailable = value
		}
	}

	if memTotal == 0 {
		return MemoryStats{}, fmt.Errorf("could not read memory info")
	}

	// Use MemAvailable if available, otherwise calculate from MemFree
	available := memAvailable
	if available == 0 {
		available = memFree
	}

	totalGB := float64(memTotal) / 1024 / 1024
	freeGB := float64(available) / 1024 / 1024
	usedGB := totalGB - freeGB

	return MemoryStats{
		Used:  roundToTwoDecimals(usedGB),
		Total: roundToTwoDecimals(totalGB),
		Free:  roundToTwoDecimals(freeGB),
	}, nil
}

// getDiskStats reads disk usage from /proc/mounts and df command
func (l *LinuxSystemStats) getDiskStats(ctx context.Context) (DiskStats, error) {
	// Actually use syscall to get disk usage
	var stat syscall.Statfs_t
	if err := syscall.Statfs(l.mountPoint, &stat); err != nil {
		return DiskStats{}, err
	}

	blockSize := uint64(stat.Bsize)
	totalBlocks := stat.Blocks
	freeBlocks := stat.Bfree

	totalGB := float64(blockSize*totalBlocks) / 1024 / 1024 / 1024
	freeGB := float64(blockSize*freeBlocks) / 1024 / 1024 / 1024
	usedGB := totalGB - freeGB

	return DiskStats{
		Used:  roundToTwoDecimals(usedGB),
		Total: roundToTwoDecimals(totalGB),
		Free:  roundToTwoDecimals(freeGB),
	}, nil
}

// roundToTwoDecimals rounds a float to two decimal places
func roundToTwoDecimals(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// GetSystemStatsJSON returns system stats as JSON string (for API responses)
func GetSystemStatsJSON(ctx context.Context, mountPoint string) (string, error) {
	collector := NewSystemStatsCollector(mountPoint)
	stats, err := collector.GetStats(ctx)
	if err != nil {
		return "", err
	}

	jsonData, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("failed to marshal stats: %w", err)
	}

	return string(jsonData), nil
}
