package models

import (
	"testing"
	"unsafe"
)

// BenchmarkStructSize measures the size of key structs.
// This verifies TASK-P009: Optimize Struct Field Alignment.
func BenchmarkStructSize(b *testing.B) {
	b.Run("Task", func(b *testing.B) {
		size := unsafe.Sizeof(Task{})
		b.Logf("Task struct size: %d bytes", size)
	})

	b.Run("Sprint", func(b *testing.B) {
		size := unsafe.Sizeof(Sprint{})
		b.Logf("Sprint struct size: %d bytes", size)
	})

	b.Run("AuditEntry", func(b *testing.B) {
		size := unsafe.Sizeof(AuditEntry{})
		b.Logf("AuditEntry struct size: %d bytes", size)
	})

	b.Run("TaskUpdate", func(b *testing.B) {
		size := unsafe.Sizeof(TaskUpdate{})
		b.Logf("TaskUpdate struct size: %d bytes", size)
	})
}
