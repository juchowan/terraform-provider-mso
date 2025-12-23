package client

import (
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
)

// CacheItem represents a cached item with its own statistics
type CacheItem struct {
	Data          interface{}
	Size          int64
	Hits          int64
	Misses        int64
	Invalidations int64
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

	// Update existing item or create new one
	if existingItem, exists := cache.items[key]; exists {
		// Remove old size from total
		cache.totalBytes -= existingItem.Size

		// Update existing item, preserving statistics
		existingItem.Data = value
		existingItem.Size = itemSize
	} else {
		// Create new item
		cache.items[key] = &CacheItem{
			Data:          value,
			Size:          itemSize,
			Hits:          0,
			Misses:        0,
			Invalidations: 0,
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
		cache.mu.RUnlock()

		// Update per-item statistics with write lock to prevent race conditions
		cache.mu.Lock()
		item.Hits++
		cache.mu.Unlock()

		return result, true, cloneErr
	}

	cache.mu.RUnlock()

	// Handle cache miss case (item not found OR item.Data is nil)
	if found && item.Data == nil {
		// Placeholder entry exists but no actual data - still a miss
		cache.mu.Lock()
		item.Misses++
		cache.mu.Unlock()
	} else if !found {
		// No entry exists at all - record miss
		cache.recordMissForKey(key)
	}

	return nil, false, nil
}

// recordMissForKey records a cache miss for a key that doesn't exist yet
func (cache *Cache) recordMissForKey(key string) {
	// Track misses per item, but the item doesn't exist yet
	// Create a placeholder entry to track the miss, which will be updated when Set is called
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if _, exists := cache.items[key]; !exists {
		// Create a temporary entry just to track the miss
		cache.items[key] = &CacheItem{
			Data:          nil, // No data yet
			Size:          0,
			Hits:          0,
			Misses:        1, // Record the miss
			Invalidations: 0,
		}
	} else {
		// Item was created between the RLock and Lock, just increment misses
		cache.items[key].Misses++
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
		hitRatio = calculateHitRatio(hits, misses)
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

	hitRatio = calculateHitRatio(hits, misses)
	return
}

// GetMemoryStats returns cache memory usage statistics
func (cache *Cache) GetMemoryStats() (totalBytes int64, totalMB float64, avgBytesPerItem float64, systemMemoryMB float64) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	totalBytes = cache.totalBytes
	totalMB = bytesToMB(totalBytes)

	itemCount := len(cache.items)
	if itemCount > 0 {
		avgBytesPerItem = float64(totalBytes) / float64(itemCount)
	}

	// Get current system memory stats
	systemMemoryMB = getSystemMemoryMB()

	return
}

// GetDetailedStats returns comprehensive cache statistics including memory usage (aggregated across all items)
func (cache *Cache) GetDetailedStats() (hits, misses, invalidations int64, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB float64) {
	hits, misses, invalidations, hitRatio = cache.GetStats()
	totalBytes, cacheSizeMB, avgItemBytes, systemMemoryMB := cache.GetMemoryStats()
	avgItemKB = bytesToKB(int64(avgItemBytes))
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
	// Get all data with single lock to avoid double locking
	cache.mu.RLock()
	item, exists := cache.items[schemaId]
	if !exists || item == nil {
		cache.mu.RUnlock()
		log.Printf("[DEBUG] %s for %s | ItemStats: New item", event, schemaId)
		return
	}

	// Get all data while holding single lock
	hits := item.Hits
	misses := item.Misses
	invalidations := item.Invalidations
	itemSizeKB := bytesToKB(item.Size)
	cache.mu.RUnlock()

	// Calculate derived values
	hitRatio := calculateHitRatio(hits, misses)
	systemMemoryMB := getSystemMemoryMB()

	log.Printf("[DEBUG] %s for %s | ItemStats: Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Size: %.1fKB | System: %.1fMB",
		event, schemaId, hits, misses, invalidations, hitRatio, itemSizeKB, systemMemoryMB)
}

// LogEventWithSize logs cache events with detailed per-item size and memory information
func (cache *Cache) LogEventWithSize(event, schemaId string) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if item, exists := cache.items[schemaId]; exists {
		itemSizeKB := bytesToKB(item.Size)
		systemMemoryMB := getSystemMemoryMB()

		log.Printf("[DEBUG] %s for %s | ItemSize: %.1fKB | System: %.1fMB",
			event, schemaId, itemSizeKB, systemMemoryMB)
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

// Helper functions to reduce code duplication and improve performance

// calculateHitRatio calculates hit ratio percentage from hits and misses
func calculateHitRatio(hits, misses int64) float64 {
	total := hits + misses
	if total > 0 {
		return float64(hits) / float64(total) * 100
	}
	return 0
}

// getSystemMemoryMB returns current system memory usage in MB
func getSystemMemoryMB() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / (1024 * 1024)
}

// bytesToKB converts bytes to kilobytes
func bytesToKB(bytes int64) float64 {
	return float64(bytes) / 1024
}

// bytesToMB converts bytes to megabytes
func bytesToMB(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024)
}

