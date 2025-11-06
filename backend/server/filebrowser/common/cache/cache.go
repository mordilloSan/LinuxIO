package cache

import (
	"sync"
	"time"
)

// NewCache creates a new TTL cache with optional expiration and cleanup intervals.
// First parameter is expiration time (default 24h), second is cleanup interval (default 1h).
func NewCache[T any](settings ...time.Duration) *Cache[T] {
	expires := 24 * time.Hour // default expiration
	cleanup := 1 * time.Hour  // default cleanup interval

	if len(settings) > 0 {
		expires = settings[0]
	}
	if len(settings) > 1 {
		cleanup = settings[1]
	}

	c := &Cache[T]{
		data:         make(map[string]cachedValue[T]),
		expiresAfter: expires,
	}

	// Start background cleanup goroutine
	go c.cleanupExpiredJob(cleanup)

	return c
}

// Cache is a thread-safe generic cache with TTL support.
type Cache[T any] struct {
	data         map[string]cachedValue[T]
	mu           sync.RWMutex
	expiresAfter time.Duration
}

type cachedValue[T any] struct {
	value     T
	expiresAt time.Time
}

// Set stores a value with the default expiration time.
func (c *Cache[T]) Set(key string, value T) {
	c.SetWithExp(key, value, c.expiresAfter)
}

// SetWithExp stores a value with a custom expiration time.
func (c *Cache[T]) SetWithExp(key string, value T, exp time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = cachedValue[T]{
		value:     value,
		expiresAt: time.Now().Add(exp),
	}
}

// Get retrieves a value from the cache. Returns (value, false) if not found or expired.
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.data[key]
	if !ok {
		var zero T
		return zero, false
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		var zero T
		return zero, false
	}

	return cached.value, true
}

// Delete removes a value from the cache.
func (c *Cache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// cleanupExpired removes all expired entries from the cache.
func (c *Cache[T]) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, cached := range c.data {
		if now.After(cached.expiresAt) {
			delete(c.data, key)
		}
	}
}

// cleanupExpiredJob runs periodic cleanup of expired entries.
func (c *Cache[T]) cleanupExpiredJob(frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanupExpired()
	}
}
