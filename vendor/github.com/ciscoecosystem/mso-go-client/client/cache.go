package client

import (
	"log"
	"runtime"
	"sync"
	"time"
)

// CacheItem represents a cached item with its own statistics
type CacheItem struct {
	Data          interface{}
	Size          int64
	Hits          int64
	Misses        int64
	Invalidations int64
	CreatedAt     time.Time
	LastAccessAt  time.Time
}

// Cache provides thread-safe caching with per-item statistics tracking and memory monitoring
type Cache struct {
	mu         sync.RWMutex
	items      map[string]*CacheItem
	totalBytes int64
}

// NewCache creates and returns a new initialized Cache.
func NewCache() *Cache {
	return &Cache{
		items: make(map[string]*CacheItem),
	}
}

// Set adds or updates an item in the cache with per-item size tracking.
func (cache *Cache) Set(key string, value interface{}) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Calculate size of new item (expecting JSON bytes)
	var itemSize int64
	if jsonBytes, ok := value.([]byte); ok {
		itemSize = int64(len(jsonBytes))
	} else {
		// Fallback for non-byte values (shouldn't happen with current implementation)
		itemSize = 1024 // Estimate 1KB for unknown types
	}

	now := time.Now()

	// Update existing item or create new one
	if existingItem, exists := cache.items[key]; exists {
		// Remove old size from total
		cache.totalBytes -= existingItem.Size

		// Update existing item, preserving statistics
		existingItem.Data = value
		existingItem.Size = itemSize
		existingItem.LastAccessAt = now
	} else {
		// Create new item
		cache.items[key] = &CacheItem{
			Data:         value,
			Size:         itemSize,
			Hits:         0,
			Misses:       0,
			Invalidations: 0,
			CreatedAt:    now,
			LastAccessAt: now,
		}
	}

	// Add new size to total
	cache.totalBytes += itemSize
}

// Get atomically gets and clones an item with per-item statistics tracking
func (cache *Cache) Get(key string, cloneFunc func(interface{}) (interface{}, error)) (interface{}, bool, error) {
	cache.mu.RLock()
	item, found := cache.items[key]

	var result interface{}
	var cloneErr error

	if found && item.Data != nil {
		// Clone while holding read lock - prevents race conditions
		result, cloneErr = cloneFunc(item.Data)

		// Update per-item statistics - hits
		item.Hits++
		item.LastAccessAt = time.Now()
		cache.mu.RUnlock()
		return result, true, cloneErr
	}

	cache.mu.RUnlock()

	// Handle cache miss case (item not found OR item.Data is nil)
	if found && item.Data == nil {
		// Placeholder entry exists but no actual data - still a miss
		cache.mu.Lock()
		item.Misses++
		item.LastAccessAt = time.Now()
		cache.mu.Unlock()
	} else if !found {
		// No entry exists at all - record miss
		cache.recordMissForKey(key)
	}

	return nil, false, nil
}

// recordMissForKey records a cache miss for a key that doesn't exist yet
func (cache *Cache) recordMissForKey(key string) {
	// This is a bit tricky - we want to track misses per item, but the item doesn't exist yet
	// We'll create a placeholder entry to track the miss, which will be updated when Set is called
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if _, exists := cache.items[key]; !exists {
		// Create a temporary entry just to track the miss
		now := time.Now()
		cache.items[key] = &CacheItem{
			Data:         nil, // No data yet
			Size:         0,
			Hits:         0,
			Misses:       1, // Record the miss
			Invalidations: 0,
			CreatedAt:    now,
			LastAccessAt: now,
		}
	} else {
		// Item was created between the RLock and Lock, just increment misses
		cache.items[key].Misses++
		cache.items[key].LastAccessAt = time.Now()
	}
}

// Delete removes an item from the cache with per-item invalidation tracking.
func (cache *Cache) Delete(key string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if item, exists := cache.items[key]; exists {
		// Update total bytes
		cache.totalBytes -= item.Size

		// Record invalidation in the item before deletion
		item.Invalidations++

		// Remove the item
		delete(cache.items, key)
	}
}

// GetItemStats returns statistics for a specific cache item
func (cache *Cache) GetItemStats(key string) (hits, misses, invalidations int64, hitRatio float64, found bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if item, exists := cache.items[key]; exists {
		hits = item.Hits
		misses = item.Misses
		invalidations = item.Invalidations
		total := hits + misses
		if total > 0 {
			hitRatio = float64(hits) / float64(total) * 100
		}
		found = true
	}
	return
}

// GetStats returns aggregated cache performance statistics across all items
func (cache *Cache) GetStats() (hits, misses, invalidations int64, hitRatio float64) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	for _, item := range cache.items {
		hits += item.Hits
		misses += item.Misses
		invalidations += item.Invalidations
	}

	total := hits + misses
	if total > 0 {
		hitRatio = float64(hits) / float64(total) * 100
	}
	return
}

// GetMemoryStats returns cache memory usage statistics
func (cache *Cache) GetMemoryStats() (totalBytes int64, totalMB float64, avgBytesPerItem float64, systemMemoryMB float64) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	totalBytes = cache.totalBytes
	totalMB = float64(totalBytes) / (1024 * 1024)

	itemCount := len(cache.items)
	if itemCount > 0 {
		avgBytesPerItem = float64(totalBytes) / float64(itemCount)
	}

	// Get current system memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	systemMemoryMB = float64(m.Alloc) / (1024 * 1024)

	return
}

// GetDetailedStats returns comprehensive cache statistics including memory usage (aggregated across all items)
func (cache *Cache) GetDetailedStats() (hits, misses, invalidations int64, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB float64) {
	hits, misses, invalidations, hitRatio = cache.GetStats()
	totalBytes, cacheSizeMB, avgItemBytes, systemMemoryMB := cache.GetMemoryStats()
	avgItemKB = avgItemBytes / 1024
	_ = totalBytes // Avoid unused variable
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

	// Record invalidations for all items before clearing
	for _, item := range cache.items {
		item.Invalidations++
	}

	cache.items = make(map[string]*CacheItem)
	cache.totalBytes = 0
}

// LogEvent logs cache events with per-item statistics
func (cache *Cache) LogEvent(event, schemaId string) {
	// Get per-item statistics
	hits, misses, invalidations, hitRatio, found := cache.GetItemStats(schemaId)

	if found {
		// Get memory info for the specific item
		cache.mu.RLock()
		var itemSizeKB float64
		if item, exists := cache.items[schemaId]; exists {
			itemSizeKB = float64(item.Size) / 1024
		}
		cache.mu.RUnlock()

		// Get system memory for context
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		systemMemoryMB := float64(m.Alloc) / (1024 * 1024)

		log.Printf("[DEBUG] %s for %s | ItemStats: Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Size: %.1fKB | System: %.1fMB",
			event, schemaId, hits, misses, invalidations, hitRatio, itemSizeKB, systemMemoryMB)
	} else {
		// Fallback for items that don't exist yet
		log.Printf("[DEBUG] %s for %s | ItemStats: New item", event, schemaId)
	}
}

// LogEventWithSize logs cache events with detailed per-item size and memory information
func (cache *Cache) LogEventWithSize(event, schemaId string) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if item, exists := cache.items[schemaId]; exists {
		itemSizeKB := float64(item.Size) / 1024

		// Get system memory for context
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		systemMemoryMB := float64(m.Alloc) / (1024 * 1024)

		log.Printf("[DEBUG] %s for %s | ItemSize: %.1fKB | Created: %s | LastAccess: %s | System: %.1fMB",
			event, schemaId, itemSizeKB,
			item.CreatedAt.Format("15:04:05"),
			item.LastAccessAt.Format("15:04:05"),
			systemMemoryMB)
	}
}

// LogOperation logs cache operations with aggregated statistics across all items
func (cache *Cache) LogOperation(event string) {
	hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	itemCount := cache.Size()
	log.Printf("[DEBUG] %s | AggregateStats: Items=%d, Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Memory: Cache=%.2fMB, AvgItem=%.1fKB, System=%.1fMB",
		event, itemCount, hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB)
}

// LogMemoryReport logs a comprehensive memory usage report with per-item insights
func (cache *Cache) LogMemoryReport() {
	hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	itemCount := cache.Size()
	log.Printf("[INFO] CACHE_MEMORY_REPORT | Items: %d | Cache: %.2fMB (%.1fKB avg/item) | System: %.1fMB | Performance: %d hits, %d misses, %.1f%% hit ratio, %d invalidations",
		itemCount, cacheSizeMB, avgItemKB, systemMemoryMB, hits, misses, hitRatio, invalidations)
}

// GetAllItemStats returns statistics for all cached items (useful for debugging)
func (cache *Cache) GetAllItemStats() map[string]map[string]interface{} {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	stats := make(map[string]map[string]interface{})

	for key, item := range cache.items {
		total := item.Hits + item.Misses
		var hitRatio float64
		if total > 0 {
			hitRatio = float64(item.Hits) / float64(total) * 100
		}

		stats[key] = map[string]interface{}{
			"hits":         item.Hits,
			"misses":       item.Misses,
			"invalidations": item.Invalidations,
			"hitRatio":     hitRatio,
			"sizeKB":       float64(item.Size) / 1024,
			"createdAt":    item.CreatedAt,
			"lastAccessAt": item.LastAccessAt,
		}
	}

	return stats
}