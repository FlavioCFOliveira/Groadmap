package db

import (
	"context"
	"fmt"
	"sync"
	"time"
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
func (cc *ConnectionCache) OpenCached(roadmapName string) (*DB, error) {
	// Validate roadmap name first
	if err := validateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	// Try to get from cache
	cc.mu.RLock()
	cached, exists := cc.connections[roadmapName]
	cc.mu.RUnlock()

	if exists {
		// Check if connection is still healthy
		if cc.isHealthy(cached.db) {
			cached.lastUsed = time.Now()
			cached.useCount++
			return cached.db, nil
		}

		// Connection is dead, remove it
		cc.Remove(roadmapName)
		cached.db.Close()
	}

	// Create new connection
	db, err := Open(roadmapName)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Cache the new connection
	cc.Store(roadmapName, db)
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
		globalCache.CloseAll()
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

// validateRoadmapName validates a roadmap name
func validateRoadmapName(name string) error {
	if name == "" {
		return fmt.Errorf("roadmap name cannot be empty")
	}
	// Check for invalid characters
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return fmt.Errorf("roadmap name contains invalid character: %c", r)
		}
	}
	return nil
}
