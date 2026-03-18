package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionCache(t *testing.T) {
	// Create a temporary directory for test databases
	tmpDir, err := os.MkdirTemp("", "groadmap_cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override data directory for testing
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer func() {
		os.Setenv("HOME", originalHome)
	}()

	// Ensure data directory exists
	dataDir := filepath.Join(tmpDir, ".roadmaps")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatalf("Failed to create data dir: %v", err)
	}

	t.Run("OpenCached creates new connection", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		db, err := cache.OpenCached("test1")
		if err != nil {
			t.Fatalf("OpenCached failed: %v", err)
		}

		if db == nil {
			t.Fatal("Expected non-nil database")
		}

		// Verify connection is cached
		if cache.Get("test1") == nil {
			t.Error("Expected connection to be cached")
		}
	})

	t.Run("OpenCached reuses existing connection", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		// First open
		db1, err := cache.OpenCached("test2")
		if err != nil {
			t.Fatalf("First OpenCached failed: %v", err)
		}

		// Second open should return same connection
		db2, err := cache.OpenCached("test2")
		if err != nil {
			t.Fatalf("Second OpenCached failed: %v", err)
		}

		// Should be the same connection
		if db1 != db2 {
			t.Error("Expected same connection to be reused")
		}

		// Check use count
		stats := cache.Stats()
		for _, conn := range stats.Connections {
			if conn.RoadmapName == "test2" && conn.UseCount < 2 {
				t.Errorf("Expected use count >= 2, got %d", conn.UseCount)
			}
		}
	})

	t.Run("Store and Get", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		// Create and store a connection
		db, err := Open("test3")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		cache.Store("test3", db)

		// Retrieve
		retrieved := cache.Get("test3")
		if retrieved != db {
			t.Error("Expected to retrieve same connection")
		}
	})

	t.Run("Remove deletes connection", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		// Create and store
		db, err := Open("test4")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		cache.Store("test4", db)

		// Remove
		cache.Remove("test4")

		// Should be nil
		if cache.Get("test4") != nil {
			t.Error("Expected connection to be removed")
		}
	})

	t.Run("CloseAll closes all connections", func(t *testing.T) {
		cache := NewConnectionCache()

		// Create multiple connections
		for i := 0; i < 3; i++ {
			db, err := Open("test5_" + string(rune('a'+i)))
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			cache.Store("test5_"+string(rune('a'+i)), db)
		}

		// Close all
		err := cache.CloseAll()
		if err != nil {
			t.Errorf("CloseAll failed: %v", err)
		}

		// Should be empty
		stats := cache.Stats()
		if stats.ConnectionCount != 0 {
			t.Errorf("Expected 0 connections, got %d", stats.ConnectionCount)
		}
	})

	t.Run("Stats returns correct information", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		// Create connection
		db, err := Open("test6")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		cache.Store("test6", db)

		// Get stats
		stats := cache.Stats()
		if stats.ConnectionCount != 1 {
			t.Errorf("Expected 1 connection, got %d", stats.ConnectionCount)
		}

		if len(stats.Connections) != 1 {
			t.Errorf("Expected 1 connection info, got %d", len(stats.Connections))
		}

		conn := stats.Connections[0]
		if conn.RoadmapName != "test6" {
			t.Errorf("Expected roadmap name 'test6', got %s", conn.RoadmapName)
		}

		if conn.UseCount != 1 {
			t.Errorf("Expected use count 1, got %d", conn.UseCount)
		}

		if conn.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}

		if conn.LastUsed.IsZero() {
			t.Error("Expected LastUsed to be set")
		}
	})

	t.Run("Thread-safe concurrent access", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		// Create connection first
		db, err := Open("test7")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		cache.Store("test7", db)

		// Concurrent reads
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				_ = cache.Get("test7")
				done <- true
			}()
		}

		// Wait for all
		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("Invalid roadmap name", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		_, err := cache.OpenCached("")
		if err == nil {
			t.Error("Expected error for empty roadmap name")
		}
	})
}

func TestConnectionCacheHealthCheck(t *testing.T) {
	// Create a temporary directory for test databases
	tmpDir, err := os.MkdirTemp("", "groadmap_health_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override data directory for testing
	os.Setenv("HOME", tmpDir)

	// Ensure data directory exists
	dataDir := filepath.Join(tmpDir, ".roadmaps")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatalf("Failed to create data dir: %v", err)
	}

	t.Run("Healthy connection passes check", func(t *testing.T) {
		cache := NewConnectionCache()
		defer cache.CloseAll()

		db, err := cache.OpenCached("health_test1")
		if err != nil {
			t.Fatalf("OpenCached failed: %v", err)
		}

		if !cache.isHealthy(db) {
			t.Error("Expected healthy connection to pass check")
		}
	})

	t.Run("Closed connection fails check", func(t *testing.T) {
		cache := NewConnectionCache()

		db, err := Open("health_test2")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		// Close the underlying connection
		db.Close()

		// Should fail health check
		if cache.isHealthy(db) {
			t.Error("Expected closed connection to fail check")
		}
	})
}

func BenchmarkConnectionCache(b *testing.B) {
	// Create a temporary directory for test databases
	tmpDir, err := os.MkdirTemp("", "groadmap_cache_bench")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override data directory for testing
	os.Setenv("HOME", tmpDir)

	// Ensure data directory exists
	dataDir := filepath.Join(tmpDir, ".roadmaps")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		b.Fatalf("Failed to create data dir: %v", err)
	}

	// Create initial connection
	cache := NewConnectionCache()
	defer cache.CloseAll()

	_, err = cache.OpenCached("bench_test")
	if err != nil {
		b.Fatalf("Setup failed: %v", err)
	}

	b.Run("Cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := cache.OpenCached("bench_test")
			if err != nil {
				b.Fatalf("OpenCached failed: %v", err)
			}
		}
	})

	b.Run("Uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			db, err := Open("bench_test_uncached")
			if err != nil {
				b.Fatalf("Open failed: %v", err)
			}
			db.Close()
		}
	})
}

func BenchmarkConnectionCacheConcurrent(b *testing.B) {
	// Create a temporary directory for test databases
	tmpDir, err := os.MkdirTemp("", "groadmap_cache_concurrent")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override data directory for testing
	os.Setenv("HOME", tmpDir)

	// Ensure data directory exists
	dataDir := filepath.Join(tmpDir, ".roadmaps")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		b.Fatalf("Failed to create data dir: %v", err)
	}

	// Create initial connection
	cache := NewConnectionCache()
	defer cache.CloseAll()

	_, err = cache.OpenCached("concurrent_test")
	if err != nil {
		b.Fatalf("Setup failed: %v", err)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := cache.OpenCached("concurrent_test")
			if err != nil {
				b.Fatalf("OpenCached failed: %v", err)
			}
		}
	})
}

func TestAtexit(t *testing.T) {
	t.Run("Register and run handlers", func(t *testing.T) {
		atexit := &atexit{}

		var called bool
		atexit.Register(func() {
			called = true
		})

		atexit.Run()

		if !called {
			t.Error("Expected handler to be called")
		}
	})

	t.Run("Handlers run in LIFO order", func(t *testing.T) {
		atexit := &atexit{}

		var order []int
		atexit.Register(func() {
			order = append(order, 1)
		})
		atexit.Register(func() {
			order = append(order, 2)
		})
		atexit.Register(func() {
			order = append(order, 3)
		})

		atexit.Run()

		// Should be 3, 2, 1 (LIFO)
		if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
			t.Errorf("Expected order [3, 2, 1], got %v", order)
		}
	})

	t.Run("Run only executes once", func(t *testing.T) {
		atexit := &atexit{}

		var count int
		atexit.Register(func() {
			count++
		})

		atexit.Run()
		atexit.Run()
		atexit.Run()

		if count != 1 {
			t.Errorf("Expected handler to run once, got %d", count)
		}
	})
}
