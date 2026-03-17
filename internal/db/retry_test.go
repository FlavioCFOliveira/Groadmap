package db

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIsLockedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "database is locked",
			err:      errors.New("database is locked"),
			expected: true,
		},
		{
			name:     "SQLITE_BUSY error",
			err:      errors.New("SQLITE_BUSY (5)"),
			expected: true,
		},
		{
			name:     "busy timeout with code 5",
			err:      errors.New("database is busy (5)"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "table not found",
			err:      errors.New("table not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLockedError(tt.err)
			if result != tt.expected {
				t.Errorf("isLockedError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	callCount := 0
	err := retryWithBackoff("test operation", func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRetryWithBackoff_NonRetryableError(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("non-retryable error")

	err := retryWithBackoff("test operation", func() error {
		callCount++
		return expectedErr
	})

	if err == nil {
		t.Error("expected error, got nil")
	}

	if callCount != 1 {
		t.Errorf("expected 1 call for non-retryable error, got %d", callCount)
	}

	if !strings.Contains(err.Error(), "non-retryable error") {
		t.Errorf("expected error message to contain 'non-retryable error', got %v", err)
	}
}

func TestRetryWithBackoff_RetryableError(t *testing.T) {
	callCount := 0
	maxCalls := 3

	err := retryWithBackoff("test operation", func() error {
		callCount++
		if callCount < maxCalls {
			return errors.New("database is locked")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error after retries, got %v", err)
	}

	if callCount != maxCalls {
		t.Errorf("expected %d calls, got %d", maxCalls, callCount)
	}
}

func TestRetryWithBackoff_MaxRetriesExceeded(t *testing.T) {
	callCount := 0

	err := retryWithBackoff("test operation", func() error {
		callCount++
		return errors.New("database is locked")
	})

	if err == nil {
		t.Error("expected error after max retries, got nil")
	}

	if callCount != maxRetries {
		t.Errorf("expected %d calls (maxRetries), got %d", maxRetries, callCount)
	}

	if !strings.Contains(err.Error(), "failed after") {
		t.Errorf("expected error message to contain 'failed after', got %v", err)
	}
}

func TestRetryWithBackoff_BackoffTiming(t *testing.T) {
	// This test verifies that backoff delays are applied
	callCount := 0
	start := time.Now()

	// Force 2 retries
	_ = retryWithBackoff("test operation", func() error {
		callCount++
		if callCount < 3 {
			return errors.New("database is locked")
		}
		return nil
	})

	elapsed := time.Since(start)

	// Should have at least initialRetryDelay (50ms) for first retry
	// Second retry should have 100ms delay
	// Total minimum delay: 50ms + 100ms = 150ms
	// We allow some tolerance for execution time
	minExpectedDelay := 120 * time.Millisecond

	if elapsed < minExpectedDelay {
		t.Errorf("expected at least %v delay for retries, got %v", minExpectedDelay, elapsed)
	}
}

func TestRetryWithBackoff_CappedDelay(t *testing.T) {
	// Test that delay is capped at maxRetryDelay
	callCount := 0
	start := time.Now()

	// Force many retries to hit the cap
	_ = retryWithBackoff("test operation", func() error {
		callCount++
		if callCount < maxRetries {
			return errors.New("database is locked")
		}
		return nil
	})

	elapsed := time.Since(start)

	// Calculate expected maximum time with 20 retries:
	// Retry 1: 50ms
	// Retry 2: 100ms
	// Retry 3: 200ms
	// Retry 4: 400ms
	// Retry 5-19: 500ms each (15 * 500ms = 7500ms)
	// Total: ~8750ms + execution time
	maxExpectedDelay := 10 * time.Second

	if elapsed > maxExpectedDelay {
		t.Errorf("expected delay to be capped, but took %v (too long)", elapsed)
	}
}
