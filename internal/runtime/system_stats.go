package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// SystemStats represents system-wide resource usage
type SystemStats struct {
	Updated time.Time   `json:"updated"`
	Memory  MemoryStats `json:"memory"`
	Disk    DiskStats   `json:"disk"`
	CPU     int         `json:"cpu"`
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

// cpuStats holds CPU statistics for delta calculation
type cpuStats struct {
	timestamp time.Time
	total     uint64
	idle      uint64
}

// Byte size constants for human-readable conversions
const (
	KB = 1024
	MB = KB * 1024
	GB = MB * 1024
)

// maxCPUDeltaAge is the maximum age of a previous CPU reading for delta calculation
const maxCPUDeltaAge = 5 * time.Second

// LinuxSystemStats implements SystemStatsCollector for Linux systems
type LinuxSystemStats struct {
	prevStats  *cpuStats
	mountPoint string
	mutex      sync.RWMutex
}

// NewSystemStatsCollector returns a new system stats collector based on the OS
func NewSystemStatsCollector(mountPoint string) SystemStatsCollector {
	return &LinuxSystemStats{
		mountPoint: mountPoint,
	}
}

// GetStats collects CPU, memory, and disk statistics
func (l *LinuxSystemStats) GetStats(ctx context.Context) (SystemStats, error) {
	if err := ctx.Err(); err != nil {
		return SystemStats{}, err
	}

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

// getCPUUsage calculates CPU usage percentage using delta measurement with /proc/stat
func (l *LinuxSystemStats) getCPUUsage(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

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

	// Parse CPU fields
	total, idle := parseCPUFields(fields)

	// Initialize prevStats on the very first measurement so the next call can compute a delta.
	l.mutex.Lock()
	if l.prevStats == nil {
		l.prevStats = &cpuStats{total: total, idle: idle, timestamp: time.Now()}
		l.mutex.Unlock()

		// Sleep briefly to ensure the next /proc/stat sample reflects a non-trivial time slice.
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		return l.getCPUUsage(ctx)
	}

	// Snapshot prev under lock, then release the lock so the math below does not hold it across
	// further recursive or sleep calls.
	prev := *l.prevStats
	l.mutex.Unlock()

	// Calculate delta from the previous reading.
	if time.Since(prev.timestamp) < maxCPUDeltaAge {
		totalDelta := total - prev.total
		idleDelta := idle - prev.idle

		if totalDelta > 0 {
			// CPU% = (total_delta - idle_delta) / total_delta * 100
			idlePercent := float64(idleDelta) / float64(totalDelta) * 100.0
			cpuPercent := 100.0 - idlePercent

			// Cap at 100% and round up
			result := int(math.Ceil(math.Min(cpuPercent, 100.0)))

			// Update prevStats for next calculation
			l.mutex.Lock()
			l.prevStats = &cpuStats{total: total, idle: idle, timestamp: time.Now()}
			l.mutex.Unlock()

			return result, nil
		}
	}

	// Save current stats for next calculation
	l.mutex.Lock()
	l.prevStats = &cpuStats{total: total, idle: idle, timestamp: time.Now()}
	l.mutex.Unlock()

	// Return 0 on first measurement (need two readings for delta)
	return 0, nil
}

// parseCPUFields parses the CPU fields from /proc/stat and returns total and idle time.
// Idle includes the idle and iowait fields (fields 4 and 5 in /proc/stat after "cpu").
func parseCPUFields(fields [][]byte) (uint64, uint64) {
	var total uint64
	var idle uint64

	// Skip the "cpu" prefix and parse all numeric fields:
	// index 1=user, 2=nice, 3=system, 4=idle, 5=iowait, 6=irq, 7=softirq, 8=steal, 9=guest, 10=guest_nice
	for i := 1; i < len(fields); i++ {
		val, err := strconv.ParseUint(string(fields[i]), 10, 64)
		if err != nil {
			continue
		}
		total += val

		// Idle = idle (field 4) + iowait (field 5)
		if i == 4 || i == 5 {
			idle += val
		}
	}

	return total, idle
}

// getMemoryStats reads memory information from /proc/meminfo
func (l *LinuxSystemStats) getMemoryStats(ctx context.Context) (MemoryStats, error) {
	if err := ctx.Err(); err != nil {
		return MemoryStats{}, err
	}

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
		val, err := strconv.ParseUint(string(fields[1]), 10, 64)
		if err != nil {
			continue
		}
		// values in /proc/meminfo are in KB

		switch key {
		case "MemTotal:":
			memTotal = val
		case "MemFree:":
			memFree = val
		case "MemAvailable:":
			memAvailable = val
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

// getDiskStats reads disk usage using syscall.Statfs
func (l *LinuxSystemStats) getDiskStats(ctx context.Context) (DiskStats, error) {
	if err := ctx.Err(); err != nil {
		return DiskStats{}, err
	}

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
