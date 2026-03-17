// Package utils provides utility functions for the Groadmap application.
package utils

import (
	"log/slog"
	"os"
)

// Logger is the application-wide structured logger.
// It outputs JSON-formatted logs to stderr with level INFO by default.
var Logger *slog.Logger

func init() {
	// Initialize the logger with JSON handler outputting to stderr
	// This ensures logs don't interfere with JSON output to stdout
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	Logger = slog.New(handler)
}

// SetLogLevel sets the minimum log level for the global logger.
// Valid levels: DEBUG, INFO, WARN, ERROR
func SetLogLevel(level string) {
	var slogLevel slog.Level
	switch level {
	case "DEBUG":
		slogLevel = slog.LevelDebug
	case "INFO":
		slogLevel = slog.LevelInfo
	case "WARN":
		slogLevel = slog.LevelWarn
	case "ERROR":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	Logger = slog.New(handler)
}

// LogRetry logs a retry attempt with structured fields.
// This is used by the database retry mechanism.
func LogRetry(operation string, attempt, maxRetries int, delay string) {
	Logger.Warn("database operation retry",
		slog.String("operation", operation),
		slog.Int("attempt", attempt),
		slog.Int("max_retries", maxRetries),
		slog.String("delay", delay),
		slog.String("reason", "database locked"),
	)
}

// LogError logs an error with structured fields.
func LogError(msg string, err error, attrs ...slog.Attr) {
	args := []any{slog.String("error", err.Error())}
	for _, attr := range attrs {
		args = append(args, attr)
	}
	Logger.Error(msg, args...)
}

// LogInfo logs an info message with structured fields.
func LogInfo(msg string, attrs ...slog.Attr) {
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}
	Logger.Info(msg, args...)
}

// LogDebug logs a debug message with structured fields.
func LogDebug(msg string, attrs ...slog.Attr) {
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}
	Logger.Debug(msg, args...)
}
