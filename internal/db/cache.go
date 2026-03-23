package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ConnectionCache manages database connections for the process lifetime.
// It eliminates connection establishment overhead (10-50ms) for repeated
// operations by reusing connections keyed by roadmap name.
type ConnectionCache struct {
	// connections maps roadmap names to cached database connections
	connections map[string]*CachedConnection

	// mu protects connections for thread-safe access
	mu sync.RWMutex

	// cleanupOnce ensures cleanup runs only once
	cleanupOnce sync.Once
}

// CachedConnection wraps a database connection with metadata
type CachedConnection struct {
	db          *DB
	roadmapName string
	createdAt   time.Time
	lastUsed    time.Time
	useCount    int
}

// globalCache is the process-level connection cache
var globalCache = NewConnectionCache()

// NewConnectionCache creates a new connection cache
func NewConnectionCache() *ConnectionCache {
	return &ConnectionCache{
		connections: make(map[string]*CachedConnection),
	}
}

// OpenCached returns a cached connection for the roadmap, or creates a new one.
// If a cached connection exists and is healthy, it returns the cached connection.
// Otherwise, it creates a new connection and caches it.
func OpenCached(roadmapName string) (*DB, error) {
	return globalCache.OpenCached(roadmapName)
}

// OpenCached returns a cached connection for the roadmap, or creates a new one.
//
// Concurrency design:
//   - The health check (I/O) runs outside any lock to avoid blocking readers
//     for up to 5 seconds.
//   - After the health check, a write lock is acquired for every state mutation
//     (lastUsed, useCount, map insert/delete). This eliminates the data race on
//     CachedConnection fields (Task 73) and the TOCTOU race between releasing
//     RLock and calling Remove (Task 74).
//   - After acquiring the write lock we re-validate state because another
//     goroutine may have raced us between the RLock and the write lock.
func (cc *ConnectionCache) OpenCached(roadmapName string) (*DB, error) {
	// Validate roadmap name first (no lock needed — pure validation).
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	// --- Phase 1: snapshot under read lock, health-check outside lock ---
	cc.mu.RLock()
	snapshot, exists := cc.connections[roadmapName]
	cc.mu.RUnlock()

	if exists {
		healthy := cc.isHealthy(snapshot.db) // I/O outside any lock

		cc.mu.Lock()
		// Re-check: another goroutine may have replaced the entry while we ran
		// the health check.
		current, stillExists := cc.connections[roadmapName]
		if stillExists && current == snapshot {
			// Entry has not changed since our snapshot.
			if healthy {
				// Update metadata under write lock (fixes Task 73 data race).
				current.lastUsed = time.Now()
				current.useCount++
				db := current.db
				cc.mu.Unlock()
				return db, nil
			}
			// Connection is dead. Remove this exact entry only — do not remove
			// an entry that was replaced by another goroutine (fixes Task 74).
			delete(cc.connections, roadmapName)
			cc.mu.Unlock()
			snapshot.db.Close() // #nosec G104 -- best-effort cleanup on cache eviction
			// Fall through to create a fresh connection below.
		} else if stillExists {
			// Entry was replaced by another goroutine while we checked health.
			// Re-validate the new entry under the write lock.
			if cc.isHealthy(current.db) {
				current.lastUsed = time.Now()
				current.useCount++
				db := current.db
				cc.mu.Unlock()
				return db, nil
			}
			// The replacement is also dead; remove it and fall through.
			delete(cc.connections, roadmapName)
			staleDB := current.db
			cc.mu.Unlock()
			staleDB.Close() // #nosec G104 -- best-effort cleanup on cache eviction
		} else {
			// Entry was already removed by another goroutine.
			cc.mu.Unlock()
		}
	}

	// --- Phase 2: create a new connection outside the lock ---
	db, err := Open(roadmapName)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// --- Phase 3: insert under write lock, handle concurrent winner ---
	cc.mu.Lock()
	if existing, ok := cc.connections[roadmapName]; ok {
		// Another goroutine won the race and already stored a valid connection;
		// reuse theirs and discard the one we just opened.
		existing.lastUsed = time.Now()
		existing.useCount++
		winner := existing.db
		cc.mu.Unlock()
		db.Close() // #nosec G104 -- discard redundant connection
		return winner, nil
	}
	cc.connections[roadmapName] = &CachedConnection{
		db:          db,
		roadmapName: roadmapName,
		createdAt:   time.Now(),
		lastUsed:    time.Now(),
		useCount:    1,
	}
	cc.mu.Unlock()

	return db, nil
}

// Get retrieves a cached connection without creating a new one.
// Returns nil if no cached connection exists.
func (cc *ConnectionCache) Get(roadmapName string) *DB {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if cached, exists := cc.connections[roadmapName]; exists {
		return cached.db
	}
	return nil
}

// Store adds a connection to the cache.
func (cc *ConnectionCache) Store(roadmapName string, db *DB) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.connections[roadmapName] = &CachedConnection{
		db:          db,
		roadmapName: roadmapName,
		createdAt:   time.Now(),
		lastUsed:    time.Now(),
		useCount:    1,
	}
}

// Remove deletes a connection from the cache.
func (cc *ConnectionCache) Remove(roadmapName string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	delete(cc.connections, roadmapName)
}

// isHealthy checks if a database connection is still alive
func (cc *ConnectionCache) isHealthy(db *DB) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := db.PingContext(ctx)
	return err == nil
}

// CloseAll closes all cached connections.
// Should be called on process exit.
func CloseAllCached() error {
	return globalCache.CloseAll()
}

// CloseAll closes all cached connections.
func (cc *ConnectionCache) CloseAll() error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	var firstErr error
	for name, cached := range cc.connections {
		if err := cached.db.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("closing connection for %s: %w", name, err)
			}
		}
	}

	// Clear the map
	cc.connections = make(map[string]*CachedConnection)

	return firstErr
}

// Stats returns statistics about the cache
func (cc *ConnectionCache) Stats() CacheStats {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	stats := CacheStats{
		ConnectionCount: len(cc.connections),
		Connections:     make([]ConnectionInfo, 0, len(cc.connections)),
	}

	for _, cached := range cc.connections {
		stats.Connections = append(stats.Connections, ConnectionInfo{
			RoadmapName: cached.roadmapName,
			CreatedAt:   cached.createdAt,
			LastUsed:    cached.lastUsed,
			UseCount:    cached.useCount,
		})
	}

	return stats
}

// CacheStats contains statistics about cached connections
type CacheStats struct {
	ConnectionCount int
	Connections     []ConnectionInfo
}

// ConnectionInfo contains metadata about a cached connection
type ConnectionInfo struct {
	RoadmapName string
	CreatedAt   time.Time
	LastUsed    time.Time
	UseCount    int
}

// init registers cleanup on process exit
func init() {
	// Register cleanup using atexit pattern
	defaultAtexit.Register(func() {
		globalCache.CloseAll() // #nosec G104 -- best-effort cleanup on process exit
	})
}

// atexit provides process exit handlers
type atexit struct {
	handlers []func()
	mu       sync.Mutex
	once     sync.Once
}

// defaultAtexit is the global atexit registry
var defaultAtexit = &atexit{}

// Register adds a function to be called on process exit
func (a *atexit) Register(f func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers = append(a.handlers, f)
}

// Run executes all registered handlers
func (a *atexit) Run() {
	a.once.Do(func() {
		a.mu.Lock()
		handlers := make([]func(), len(a.handlers))
		copy(handlers, a.handlers)
		a.mu.Unlock()

		// Run handlers in reverse order (LIFO)
		for i := len(handlers) - 1; i >= 0; i-- {
			handlers[i]()
		}
	})
}

// Register adds a function to be called on process exit
func RegisterExitHandler(f func()) {
	defaultAtexit.Register(f)
}

// RunExitHandlers executes all registered exit handlers
func RunExitHandlers() {
	defaultAtexit.Run()
}
