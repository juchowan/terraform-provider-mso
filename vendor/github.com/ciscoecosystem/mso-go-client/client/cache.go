package client

import (
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
func (cache *Cache) Set(key string, value interface{}) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.items[key] = value
}

// Get atomically gets and clones an item to prevent race conditions
func (cache *Cache) Get(key string, cloneFunc func(interface{}) (interface{}, error)) (interface{}, bool, error) {
	cache.mu.RLock()
	item, found := cache.items[key]

	var result interface{}
	var cloneErr error

	if found {
		// Clone while holding read lock - prevents race conditions
		result, cloneErr = cloneFunc(item)
	}
	cache.mu.RUnlock()

	// Update statistics
	cache.mu.Lock()
	if found {
		cache.hits++
	} else {
		cache.misses++
	}
	cache.mu.Unlock()

	return result, found, cloneErr
}

// Delete removes an item from the cache.
func (cache *Cache) Delete(key string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	delete(cache.items, key)
	cache.invalidations++
}

// GetStats returns cache performance statistics
func (cache *Cache) GetStats() (hits, misses, invalidations int64, hitRatio float64) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	hits = cache.hits
	misses = cache.misses
	invalidations = cache.invalidations
	total := hits + misses
	if total > 0 {
		hitRatio = float64(hits) / float64(total) * 100
	}
	return
}

// Size returns the number of items in the cache
func (cache *Cache) Size() int {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return len(cache.items)
}

// Clear removes all items from the cache
func (cache *Cache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	itemCount := len(cache.items)
	cache.items = make(map[string]interface{})
	cache.invalidations += int64(itemCount)
}
