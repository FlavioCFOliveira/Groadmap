package metrics

import (
	"testing"
	"time"
)

func TestStartOperation(t *testing.T) {
	// Test with metrics enabled
	Enable()
	timer := StartOperation("test.operation")
	if timer == nil {
		t.Error("StartOperation should return a timer when enabled")
	}
	if timer.Name != "test.operation" {
		t.Errorf("Expected name 'test.operation', got %q", timer.Name)
	}
	if timer.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}

	// Test with metrics disabled
	Disable()
	timer = StartOperation("test.operation")
	if timer != nil {
		t.Error("StartOperation should return nil when disabled")
	}
	Enable() // Re-enable for other tests
}

func TestEndOperation(t *testing.T) {
	Reset()
	Enable()

	// Record an operation
	timer := StartOperation("test.op")
	time.Sleep(1 * time.Millisecond)
	EndOperation(timer)

	// Verify metric was recorded
	metric, exists := GetOperationMetric("test.op")
	if !exists {
		t.Fatal("Operation metric should exist")
	}
	if metric.Count != 1 {
		t.Errorf("Expected count 1, got %d", metric.Count)
	}
	if metric.TotalTime <= 0 {
		t.Error("TotalTime should be > 0")
	}

	// Test with nil timer
	EndOperation(nil) // Should not panic

	// Test with disabled metrics
	Disable()
	timer = StartOperation("disabled.op")
	if timer != nil {
		t.Error("Should return nil when disabled")
	}
	Enable()
}

func TestRecordOperationMultiple(t *testing.T) {
	Reset()
	Enable()

	// Record multiple operations
	for i := 0; i < 5; i++ {
		timer := StartOperation("multi.op")
		time.Sleep(time.Duration(i+1) * time.Millisecond)
		EndOperation(timer)
	}

	metric, exists := GetOperationMetric("multi.op")
	if !exists {
		t.Fatal("Operation metric should exist")
	}
	if metric.Count != 5 {
		t.Errorf("Expected count 5, got %d", metric.Count)
	}
	if metric.MinTime >= metric.MaxTime {
		t.Error("MinTime should be less than MaxTime")
	}
	if metric.AvgTime <= 0 {
		t.Error("AvgTime should be > 0")
	}
}

func TestIncrementCounter(t *testing.T) {
	Reset()
	Enable()

	IncrementCounter("test.counter")
	IncrementCounter("test.counter")
	IncrementCounter("test.counter")

	metric, exists := GetCounterMetric("test.counter")
	if !exists {
		t.Fatal("Counter metric should exist")
	}
	if metric.Value != 3 {
		t.Errorf("Expected value 3, got %d", metric.Value)
	}
}

func TestAddToCounter(t *testing.T) {
	Reset()
	Enable()

	AddToCounter("batch.counter", 10)
	AddToCounter("batch.counter", 5)

	metric, exists := GetCounterMetric("batch.counter")
	if !exists {
		t.Fatal("Counter metric should exist")
	}
	if metric.Value != 15 {
		t.Errorf("Expected value 15, got %d", metric.Value)
	}
}

func TestGetAllOperations(t *testing.T) {
	Reset()
	Enable()

	// Record different operations
	timer1 := StartOperation("op1")
	EndOperation(timer1)
	timer2 := StartOperation("op2")
	EndOperation(timer2)

	ops := GetAllOperations()
	if len(ops) != 2 {
		t.Errorf("Expected 2 operations, got %d", len(ops))
	}
}

func TestGetAllCounters(t *testing.T) {
	Reset()
	Enable()

	IncrementCounter("counter1")
	IncrementCounter("counter2")

	counters := GetAllCounters()
	if len(counters) != 2 {
		t.Errorf("Expected 2 counters, got %d", len(counters))
	}
}

func TestGetSummary(t *testing.T) {
	Reset()
	Enable()

	timer := StartOperation("summary.op")
	EndOperation(timer)
	IncrementCounter("summary.counter")

	summary := GetSummary()
	if len(summary.Operations) != 1 {
		t.Errorf("Expected 1 operation in summary, got %d", len(summary.Operations))
	}
	if len(summary.Counters) != 1 {
		t.Errorf("Expected 1 counter in summary, got %d", len(summary.Counters))
	}
	if summary.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should not be zero")
	}
}

func TestReset(t *testing.T) {
	Reset()
	Enable()

	IncrementCounter("reset.test")
	timer := StartOperation("reset.op")
	EndOperation(timer)

	Reset()

	_, exists := GetCounterMetric("reset.test")
	if exists {
		t.Error("Counter should be cleared after reset")
	}

	_, exists = GetOperationMetric("reset.op")
	if exists {
		t.Error("Operation should be cleared after reset")
	}
}

func TestEnableDisable(t *testing.T) {
	Reset()

	if !IsEnabled() {
		t.Error("Metrics should be enabled by default")
	}

	Disable()
	if IsEnabled() {
		t.Error("Metrics should be disabled after Disable()")
	}

	Enable()
	if !IsEnabled() {
		t.Error("Metrics should be enabled after Enable()")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{100 * time.Nanosecond, "100 ns"},
		{1500 * time.Nanosecond, "1.50 µs"},
		{1500 * time.Microsecond, "1.50 ms"},
		{1500 * time.Millisecond, "1.50 s"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.input)
		if got != tt.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNonExistentMetric(t *testing.T) {
	Reset()

	_, exists := GetOperationMetric("nonexistent")
	if exists {
		t.Error("Should return false for non-existent operation")
	}

	_, exists = GetCounterMetric("nonexistent")
	if exists {
		t.Error("Should return false for non-existent counter")
	}
}

// Benchmark to verify overhead is < 1%
func BenchmarkMetricsOverhead(b *testing.B) {
	Reset()
	Enable()

	b.Run("WithMetrics", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			timer := StartOperation("benchmark.op")
			// Simulate minimal work
			time.Sleep(1 * time.Microsecond)
			EndOperation(timer)
		}
	})
}

func BenchmarkWithoutMetrics(b *testing.B) {
	Disable()

	b.Run("WithoutMetrics", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			timer := StartOperation("benchmark.op")
			// Simulate minimal work
			time.Sleep(1 * time.Microsecond)
			EndOperation(timer)
		}
	})
	Enable()
}
