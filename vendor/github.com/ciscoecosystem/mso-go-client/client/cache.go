package client

import (
	"strings"
	"sync"
)

// Cache provides thread-safe caching with statistics tracking
type Cache struct {
	mu            sync.RWMutex
	items         map[string]interface{}
	hits          int64
	misses        int64
	invalidations int64
}

// NewCache creates and returns a new initialized Cache.
func NewCache() *Cache {
	return &Cache{
		items: make(map[string]interface{}),
	}
}

// Set adds or updates an item in the cache.
func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
}

// Get atomically gets and clones an item to prevent race conditions
func (c *Cache) Get(key string, cloneFunc func(interface{}) (interface{}, error)) (interface{}, bool, error) {
	c.mu.RLock()
	item, found := c.items[key]

	var result interface{}
	var cloneErr error

	if found {
		// Clone while holding read lock - prevents race conditions
		result, cloneErr = cloneFunc(item)
	}
	c.mu.RUnlock()

	// Update statistics
	c.mu.Lock()
	if found {
		c.hits++
	} else {
		c.misses++
	}
	c.mu.Unlock()

	return result, found, cloneErr
}

// Delete removes an item from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	c.invalidations++
}

// DeletePattern removes all items from the cache matching the pattern.
func (c *Cache) DeletePattern(pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	deletedCount := 0
	for key := range c.items {
		if strings.Contains(key, pattern) {
			delete(c.items, key)
			deletedCount++
		}
	}
}

// GetStats returns cache performance statistics
func (c *Cache) GetStats() (hits, misses, invalidations int64, hitRatio float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hits = c.hits
	misses = c.misses
	invalidations = c.invalidations
	total := hits + misses
	if total > 0 {
		hitRatio = float64(hits) / float64(total) * 100
	}
	return
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}