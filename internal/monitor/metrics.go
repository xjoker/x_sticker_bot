package monitor

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects runtime statistics in a thread-safe manner.
type Metrics struct {
	startTime      time.Time
	totalRequests  atomic.Int64
	errorCount     atomic.Int64
	rateDenied     atomic.Int64
	commandCounts  sync.Map // map[string]*atomic.Int64
	recentRequests *RingBuffer
}

// Snapshot is a point-in-time view of all metrics, safe for JSON serialization.
type Snapshot struct {
	Uptime         string           `json:"uptime"`
	TotalRequests  int64            `json:"total_requests"`
	RequestsPerMin float64          `json:"requests_per_min"`
	ErrorCount     int64            `json:"error_count"`
	ErrorRate      float64          `json:"error_rate"`
	CommandCounts  map[string]int64 `json:"command_counts"`
	RateDenied     int64            `json:"rate_denied"`
	MemoryMB       float64          `json:"memory_mb"`
	Goroutines     int              `json:"goroutines"`
	ActiveTasks    int              `json:"active_tasks"`
	TaskCapacity   int              `json:"task_capacity"`
}

// NewMetrics creates a new Metrics instance with default ring buffer size.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime:      time.Now(),
		recentRequests: NewRingBuffer(3600),
	}
}

// RecordRequest increments the total request counter and the per-command counter.
func (m *Metrics) RecordRequest(command string) {
	m.totalRequests.Add(1)
	m.recentRequests.Add(time.Now())

	actual, _ := m.commandCounts.LoadOrStore(command, &atomic.Int64{})
	actual.(*atomic.Int64).Add(1)
}

// RecordError increments the error counter.
func (m *Metrics) RecordError() {
	m.errorCount.Add(1)
}

// RecordRateDenied increments the rate-denied counter.
func (m *Metrics) RecordRateDenied() {
	m.rateDenied.Add(1)
}

// Snapshot builds a point-in-time snapshot of all metrics.
func (m *Metrics) Snapshot() Snapshot {
	now := time.Now()
	total := m.totalRequests.Load()
	errors := m.errorCount.Load()

	var errorRate float64
	if total > 0 {
		errorRate = float64(errors) / float64(total) * 100
	}

	reqPerMin := float64(m.recentRequests.CountSince(now.Add(-60 * time.Second)))

	commandCounts := make(map[string]int64)
	m.commandCounts.Range(func(key, value any) bool {
		commandCounts[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return Snapshot{
		Uptime:         formatUptime(now.Sub(m.startTime)),
		TotalRequests:  total,
		RequestsPerMin: reqPerMin,
		ErrorCount:     errors,
		ErrorRate:      errorRate,
		CommandCounts:  commandCounts,
		RateDenied:     m.rateDenied.Load(),
		MemoryMB:       float64(mem.Alloc) / 1024 / 1024,
		Goroutines:     runtime.NumGoroutine(),
	}
}

// formatUptime formats a duration as "3d 12h 45m".
func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		times: make([]time.Time, size),
		size:  size,
	}
}

// RingBuffer is a fixed-size circular buffer for sliding window calculations.
type RingBuffer struct {
	mu    sync.Mutex
	times []time.Time
	size  int
	pos   int
}

// Add inserts a timestamp into the ring buffer.
func (rb *RingBuffer) Add(t time.Time) {
	rb.mu.Lock()
	rb.times[rb.pos] = t
	rb.pos = (rb.pos + 1) % rb.size
	rb.mu.Unlock()
}

// CountSince returns the number of entries recorded since the given time.
func (rb *RingBuffer) CountSince(since time.Time) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	count := 0
	for _, t := range rb.times {
		if !t.IsZero() && !t.Before(since) {
			count++
		}
	}
	return count
}
