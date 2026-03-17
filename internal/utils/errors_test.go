package utils

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	// Test that sentinel errors are defined
	tests := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrInvalidInput", ErrInvalidInput},
		{"ErrRequired", ErrRequired},
		{"ErrNoRoadmap", ErrNoRoadmap},
		{"ErrDatabase", ErrDatabase},
		{"ErrValidation", ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Errorf("%s should not be nil", tt.name)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	// Test direct error
	if !IsNotFound(ErrNotFound) {
		t.Error("IsNotFound(ErrNotFound) should return true")
	}

	// Test wrapped error
	wrapped := fmt.Errorf("wrapped: %w", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound(wrapped ErrNotFound) should return true")
	}

	// Test different error
	if IsNotFound(ErrAlreadyExists) {
		t.Error("IsNotFound(ErrAlreadyExists) should return false")
	}
}

func TestIsAlreadyExists(t *testing.T) {
	if !IsAlreadyExists(ErrAlreadyExists) {
		t.Error("IsAlreadyExists(ErrAlreadyExists) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrAlreadyExists)
	if !IsAlreadyExists(wrapped) {
		t.Error("IsAlreadyExists(wrapped ErrAlreadyExists) should return true")
	}

	if IsAlreadyExists(ErrNotFound) {
		t.Error("IsAlreadyExists(ErrNotFound) should return false")
	}
}

func TestIsInvalidInput(t *testing.T) {
	if !IsInvalidInput(ErrInvalidInput) {
		t.Error("IsInvalidInput(ErrInvalidInput) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrInvalidInput)
	if !IsInvalidInput(wrapped) {
		t.Error("IsInvalidInput(wrapped ErrInvalidInput) should return true")
	}

	if IsInvalidInput(ErrNotFound) {
		t.Error("IsInvalidInput(ErrNotFound) should return false")
	}
}

func TestIsRequired(t *testing.T) {
	if !IsRequired(ErrRequired) {
		t.Error("IsRequired(ErrRequired) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrRequired)
	if !IsRequired(wrapped) {
		t.Error("IsRequired(wrapped ErrRequired) should return true")
	}

	if IsRequired(ErrNotFound) {
		t.Error("IsRequired(ErrNotFound) should return false")
	}
}

func TestIsNoRoadmap(t *testing.T) {
	if !IsNoRoadmap(ErrNoRoadmap) {
		t.Error("IsNoRoadmap(ErrNoRoadmap) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrNoRoadmap)
	if !IsNoRoadmap(wrapped) {
		t.Error("IsNoRoadmap(wrapped ErrNoRoadmap) should return true")
	}

	if IsNoRoadmap(ErrNotFound) {
		t.Error("IsNoRoadmap(ErrNotFound) should return false")
	}
}

func TestIsDatabase(t *testing.T) {
	if !IsDatabase(ErrDatabase) {
		t.Error("IsDatabase(ErrDatabase) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrDatabase)
	if !IsDatabase(wrapped) {
		t.Error("IsDatabase(wrapped ErrDatabase) should return true")
	}

	if IsDatabase(ErrNotFound) {
		t.Error("IsDatabase(ErrNotFound) should return false")
	}
}

func TestIsValidation(t *testing.T) {
	if !IsValidation(ErrValidation) {
		t.Error("IsValidation(ErrValidation) should return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", ErrValidation)
	if !IsValidation(wrapped) {
		t.Error("IsValidation(wrapped ErrValidation) should return true")
	}

	if IsValidation(ErrNotFound) {
		t.Error("IsValidation(ErrNotFound) should return false")
	}
}

func TestErrorsIs(t *testing.T) {
	// Test that errors.Is works correctly with sentinel errors
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{"ErrNotFound direct", ErrNotFound, ErrNotFound, true},
		{"ErrNotFound wrapped", fmt.Errorf("wrapped: %w", ErrNotFound), ErrNotFound, true},
		{"ErrNotFound different", ErrAlreadyExists, ErrNotFound, false},
		{"ErrAlreadyExists direct", ErrAlreadyExists, ErrAlreadyExists, true},
		{"ErrAlreadyExists wrapped", fmt.Errorf("wrapped: %w", ErrAlreadyExists), ErrAlreadyExists, true},
		{"ErrInvalidInput direct", ErrInvalidInput, ErrInvalidInput, true},
		{"ErrRequired direct", ErrRequired, ErrRequired, true},
		{"ErrNoRoadmap direct", ErrNoRoadmap, ErrNoRoadmap, true},
		{"ErrDatabase direct", ErrDatabase, ErrDatabase, true},
		{"ErrValidation direct", ErrValidation, ErrValidation, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errors.Is(tt.err, tt.target)
			if result != tt.expected {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, result, tt.expected)
			}
		})
	}
}
