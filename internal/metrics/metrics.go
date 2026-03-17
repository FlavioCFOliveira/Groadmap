// Package metrics provides performance metrics collection for Groadmap operations.
//
// This package implements lightweight metrics collection with minimal overhead (<1%).
// Metrics are stored in memory and can be persisted to the database for long-term analysis.
//
// Usage:
//
//	// Record operation duration
//	start := metrics.StartOperation("task.create")
//	// ... do work ...
//	metrics.EndOperation(start)
//
//	// Get metrics summary
//	summary := metrics.GetSummary()
package metrics

import (
	"fmt"
	"sync"
	"time"
)

// MetricType represents the type of metric being collected.
type MetricType string

const (
	// MetricOperation represents a timed operation
	MetricOperation MetricType = "operation"
	// MetricCounter represents a counter metric
	MetricCounter MetricType = "counter"
)

// OperationMetric stores metrics for a specific operation type.
type OperationMetric struct {
	Name      string        `json:"name"`
	Count     int64         `json:"count"`
	TotalTime time.Duration `json:"total_time"`
	MinTime   time.Duration `json:"min_time"`
	MaxTime   time.Duration `json:"max_time"`
	AvgTime   time.Duration `json:"avg_time"`
}

// CounterMetric stores a simple counter value.
type CounterMetric struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// Metrics holds all collected metrics.
type Metrics struct {
	mu         sync.RWMutex
	operations map[string]*OperationMetric
	counters   map[string]*CounterMetric
	enabled    bool
}

// Global metrics instance
var globalMetrics = &Metrics{
	operations: make(map[string]*OperationMetric),
	counters:   make(map[string]*CounterMetric),
	enabled:    true,
}

// OperationTimer represents a running operation timer.
type OperationTimer struct {
	Name      string
	StartTime time.Time
}

// StartOperation begins timing an operation.
// Returns a timer that should be passed to EndOperation.
func StartOperation(name string) *OperationTimer {
	if !globalMetrics.enabled {
		return nil
	}
	return &OperationTimer{
		Name:      name,
		StartTime: time.Now(),
	}
}

// EndOperation records the completion of a timed operation.
func EndOperation(timer *OperationTimer) {
	if timer == nil || !globalMetrics.enabled {
		return
	}

	duration := time.Since(timer.StartTime)
	globalMetrics.recordOperation(timer.Name, duration)
}

// recordOperation records an operation metric (thread-safe).
func (m *Metrics) recordOperation(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric, exists := m.operations[name]
	if !exists {
		metric = &OperationMetric{
			Name:    name,
			MinTime: duration,
			MaxTime: duration,
		}
		m.operations[name] = metric
	}

	metric.Count++
	metric.TotalTime += duration

	if duration < metric.MinTime {
		metric.MinTime = duration
	}
	if duration > metric.MaxTime {
		metric.MaxTime = duration
	}

	// Calculate average
	metric.AvgTime = metric.TotalTime / time.Duration(metric.Count)
}

// IncrementCounter increments a counter metric by 1.
func IncrementCounter(name string) {
	AddToCounter(name, 1)
}

// AddToCounter adds a value to a counter metric.
func AddToCounter(name string, value int64) {
	if !globalMetrics.enabled {
		return
	}

	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	metric, exists := globalMetrics.counters[name]
	if !exists {
		metric = &CounterMetric{Name: name}
		globalMetrics.counters[name] = metric
	}

	metric.Value += value
}

// GetOperationMetric retrieves metrics for a specific operation.
func GetOperationMetric(name string) (*OperationMetric, bool) {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	metric, exists := globalMetrics.operations[name]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	return &OperationMetric{
		Name:      metric.Name,
		Count:     metric.Count,
		TotalTime: metric.TotalTime,
		MinTime:   metric.MinTime,
		MaxTime:   metric.MaxTime,
		AvgTime:   metric.AvgTime,
	}, true
}

// GetCounterMetric retrieves a counter metric.
func GetCounterMetric(name string) (*CounterMetric, bool) {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	metric, exists := globalMetrics.counters[name]
	if !exists {
		return nil, false
	}

	// Return a copy
	return &CounterMetric{
		Name:  metric.Name,
		Value: metric.Value,
	}, true
}

// GetAllOperations returns all operation metrics.
func GetAllOperations() []*OperationMetric {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	result := make([]*OperationMetric, 0, len(globalMetrics.operations))
	for _, metric := range globalMetrics.operations {
		result = append(result, &OperationMetric{
			Name:      metric.Name,
			Count:     metric.Count,
			TotalTime: metric.TotalTime,
			MinTime:   metric.MinTime,
			MaxTime:   metric.MaxTime,
			AvgTime:   metric.AvgTime,
		})
	}

	return result
}

// GetAllCounters returns all counter metrics.
func GetAllCounters() []*CounterMetric {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	result := make([]*CounterMetric, 0, len(globalMetrics.counters))
	for _, metric := range globalMetrics.counters {
		result = append(result, &CounterMetric{
			Name:  metric.Name,
			Value: metric.Value,
		})
	}

	return result
}

// Summary represents a summary of all metrics.
type Summary struct {
	Operations  []*OperationMetric `json:"operations"`
	Counters    []*CounterMetric   `json:"counters"`
	GeneratedAt time.Time          `json:"generated_at"`
}

// GetSummary returns a complete summary of all metrics.
func GetSummary() *Summary {
	return &Summary{
		Operations:  GetAllOperations(),
		Counters:    GetAllCounters(),
		GeneratedAt: time.Now(),
	}
}

// Reset clears all metrics.
func Reset() {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	globalMetrics.operations = make(map[string]*OperationMetric)
	globalMetrics.counters = make(map[string]*CounterMetric)
}

// Enable enables metrics collection.
func Enable() {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()
	globalMetrics.enabled = true
}

// Disable disables metrics collection.
func Disable() {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()
	globalMetrics.enabled = false
}

// IsEnabled returns whether metrics collection is enabled.
func IsEnabled() bool {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()
	return globalMetrics.enabled
}

// FormatDuration formats a duration for human-readable output.
func FormatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%d ns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2f µs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Nanoseconds())/1e6)
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}
