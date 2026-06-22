package runtime

import (
	"context"
	"testing"
	"time"
)

// TestParseCPUFields is a table-driven test covering CPU field parsing,
// including the iowait field as part of idle time.
func TestParseCPUFields(t *testing.T) {
	tests := []struct {
		name      string
		fields    [][]byte
		wantTotal uint64
		wantIdle  uint64
	}{
		{
			name: "normal usage with iowait",
			fields: [][]byte{
				[]byte("cpu"),
				[]byte("100000"), []byte("20000"), []byte("10000"), []byte("60000"),
				[]byte("5000"), []byte("3000"), []byte("2000"), []byte("1000"),
				[]byte("500"), []byte("200"),
			},
			// total = 100000+20000+10000+60000+5000+3000+2000+1000+500+200 = 201700
			// idle  = idle(60k) + iowait(5k) = 65000
			wantTotal: 201700,
			wantIdle:  65000,
		},
		{
			name: "high user CPU, low iowait",
			fields: [][]byte{
				[]byte("cpu"),
				[]byte("1000000"), []byte("900000"), []byte("50000"), []byte("30000"),
				[]byte("10000"), []byte("20000"), []byte("10000"), []byte("5000"),
				[]byte("2000"), []byte("1000"),
			},
			// total = 1000000+900000+50000+30000+10000+20000+10000+5000+2000+1000 = 2028000
			// idle  = idle(30k) + iowait(10k) = 40000
			wantTotal: 2028000,
			wantIdle:  40000,
		},
		{
			name: "all idle, no other activity",
			fields: [][]byte{
				[]byte("cpu"),
				[]byte("0"), []byte("0"), []byte("0"), []byte("1000000"),
				[]byte("0"), []byte("0"), []byte("0"), []byte("0"),
				[]byte("0"), []byte("0"),
			},
			wantTotal: 1000000,
			wantIdle:  1000000,
		},
		{
			name: "heavy iowait",
			fields: [][]byte{
				[]byte("cpu"),
				[]byte("1000"), []byte("500"), []byte("800"), []byte("20000"),
				[]byte("50000"), []byte("0"), []byte("0"), []byte("0"),
				[]byte("0"), []byte("0"),
			},
			// idle = 20000 + 50000 = 70000
			wantIdle: 70000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, idle := parseCPUFields(tt.fields)
			if total != tt.wantTotal && tt.wantTotal != 0 {
				t.Errorf("total: want %d, got %d", tt.wantTotal, total)
			}
			if idle != tt.wantIdle {
				t.Errorf("idle: want %d, got %d", tt.wantIdle, idle)
			}
		})
	}
}

// TestCPUUsage_DeltaMeasurement tests the delta-based CPU calculation
func TestCPUUsage_DeltaMeasurement(t *testing.T) {
	tempDir := t.TempDir()

	statsCollector := &LinuxSystemStats{
		mountPoint: tempDir,
	}

	ctx := context.Background()

	// First call: implementation performs an internal 100ms sample so the
	// returned value is a real (load-dependent) reading, not 0. We only
	// assert that the value is within the valid percentage range.
	cpuPercent1, err := statsCollector.getCPUUsage(ctx)
	if err != nil {
		t.Fatalf("First CPU measurement failed: %v", err)
	}
	if cpuPercent1 < 0 || cpuPercent1 > 100 {
		t.Errorf("First CPU measurement out of range [0,100], got %d", cpuPercent1)
	}

	// Simulate a previous reading to enable delta calculation
	testFields := [][]byte{
		[]byte("cpu"),
		[]byte("200000"), []byte("60000"), []byte("20000"), []byte("100000"),
		[]byte("10000"), []byte("6000"), []byte("4000"), []byte("2000"),
		[]byte("1000"), []byte("400"),
	}
	total, idle := parseCPUFields(testFields)

	statsCollector.mutex.Lock()
	statsCollector.prevStats = &cpuStats{
		total:     total,
		idle:      idle,
		timestamp: time.Now().Add(-2 * time.Second),
	}
	statsCollector.mutex.Unlock()

	// Second call should calculate delta
	cpuPercent2, err := statsCollector.getCPUUsage(ctx)
	if err != nil {
		t.Fatalf("Second CPU measurement failed: %v", err)
	}

	t.Logf("CPU delta measurement: %d%%", cpuPercent2)

	// The delta is computed against real /proc/stat activity since the
	// prevStats snapshot was taken 2s ago. On a fully idle host the
	// delta can legitimately be 0, so we only assert the valid range
	// and that the cap at 100% holds.
	if cpuPercent2 < 0 || cpuPercent2 > 100 {
		t.Errorf("CPU delta percentage out of range [0,100], got %d%%", cpuPercent2)
	}
}

// TestCPUUsage_FirstCallReturnsValidRange verifies that the very first CPU
// measurement returns a value within the valid percentage range [0, 100].
// The implementation deliberately takes a ~100ms sample on first call
// (via a recursive internal call) so callers see a real reading rather
// than 0. The exact value is system-load dependent, so we only assert
// the bounded range and that no error is returned.
func TestCPUUsage_FirstCallReturnsValidRange(t *testing.T) {
	statsCollector := &LinuxSystemStats{
		mountPoint: "/tmp",
	}

	ctx := context.Background()

	cpuPercent, err := statsCollector.getCPUUsage(ctx)
	if err != nil {
		t.Fatalf("First CPU measurement failed: %v", err)
	}
	if cpuPercent < 0 || cpuPercent > 100 {
		t.Errorf("First CPU measurement out of range [0,100], got %d", cpuPercent)
	}
}

// TestGetStats_Integration tests the full GetStats method
func TestGetStats_Integration(t *testing.T) {
	statsCollector := &LinuxSystemStats{
		mountPoint: "/",
	}

	ctx := context.Background()
	stats, err := statsCollector.GetStats(ctx)

	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// CPU value is a percentage in [0, 100]. The first call may return a
	// real measurement (after the internal 100ms sample) rather than 0,
	// so we only assert the bounded range.
	if stats.CPU < 0 || stats.CPU > 100 {
		t.Errorf("CPU percentage out of range [0,100], got %d", stats.CPU)
	}

	// Check that memory stats are reasonable (using real /proc/meminfo)
	if stats.Memory.Total <= 0 {
		t.Errorf("Expected positive memory total, got %f", stats.Memory.Total)
	}
	if stats.Memory.Used < 0 {
		t.Errorf("Expected non-negative memory used, got %f", stats.Memory.Used)
	}
}

// TestContextCancellation tests that context cancellation is respected
func TestContextCancellation(t *testing.T) {
	statsCollector := &LinuxSystemStats{
		mountPoint: "/",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := statsCollector.GetStats(ctx)
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}
