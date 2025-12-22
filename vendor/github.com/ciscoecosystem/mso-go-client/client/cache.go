package client

import (
	"log"
	"runtime"
	"sync"
)

// Cache provides thread-safe caching with statistics tracking and memory monitoring
type Cache struct {
	mu            sync.RWMutex
	items         map[string]interface{}
	itemSizes     map[string]int64  // Track size of each cached item in bytes
	hits          int64
	misses        int64
	invalidations int64
	totalBytes    int64             // Total memory used by cache items
}

// NewCache creates and returns a new initialized Cache.
func NewCache() *Cache {
	return &Cache{
		items:     make(map[string]interface{}),
		itemSizes: make(map[string]int64),
	}
}

// Set adds or updates an item in the cache with size tracking.
func (cache *Cache) Set(key string, value interface{}) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Remove previous item size if it exists
	if prevSize, exists := cache.itemSizes[key]; exists {
		cache.totalBytes -= prevSize
	}

	// Calculate size of new item (expecting JSON bytes)
	var itemSize int64
	if jsonBytes, ok := value.([]byte); ok {
		itemSize = int64(len(jsonBytes))
	} else {
		// Fallback for non-byte values (shouldn't happen with current implementation)
		itemSize = 1024 // Estimate 1KB for unknown types
	}

	// Store item and track its size
	cache.items[key] = value
	cache.itemSizes[key] = itemSize
	cache.totalBytes += itemSize
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

// Delete removes an item from the cache with size tracking.
func (cache *Cache) Delete(key string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Remove size tracking for the item
	if itemSize, exists := cache.itemSizes[key]; exists {
		cache.totalBytes -= itemSize
		delete(cache.itemSizes, key)
	}

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

// GetDetailedStats returns comprehensive cache statistics including memory usage
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

// Clear removes all items from the cache with size tracking
func (cache *Cache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	itemCount := len(cache.items)
	cache.items = make(map[string]interface{})
	cache.itemSizes = make(map[string]int64)
	cache.totalBytes = 0
	cache.invalidations += int64(itemCount)
}

// LogEvent logs cache events with consistent statistics formatting including memory usage
func (cache *Cache) LogEvent(event, schemaId string) {
	hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	log.Printf("[DEBUG] %s for %s | Stats: Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Memory: Cache=%.2fMB, AvgItem=%.1fKB, System=%.1fMB",
		event, schemaId, hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB)
}

// LogEventWithSize logs cache events with detailed size and memory information
func (cache *Cache) LogEventWithSize(event, schemaId string) {
	_, _, _, _, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	itemCount := cache.Size()
	log.Printf("[DEBUG] %s for %s | Items: %d | Memory: Cache=%.2fMB, AvgItem=%.1fKB, System=%.1fMB",
		event, schemaId, itemCount, cacheSizeMB, avgItemKB, systemMemoryMB)
}

// LogOperation logs cache operations without schema ID including memory stats
func (cache *Cache) LogOperation(event string) {
	hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	log.Printf("[DEBUG] %s | Stats: Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Memory: Cache=%.2fMB, AvgItem=%.1fKB, System=%.1fMB",
		event, hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB)
}

// LogMemoryReport logs a comprehensive memory usage report
func (cache *Cache) LogMemoryReport() {
	hits, misses, invalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB := cache.GetDetailedStats()
	itemCount := cache.Size()
	log.Printf("[INFO] CACHE_MEMORY_REPORT | Items: %d | Cache: %.2fMB (%.1fKB avg/item) | System: %.1fMB | Performance: %d hits, %d misses, %.1f%% hit ratio, %d invalidations",
		itemCount, cacheSizeMB, avgItemKB, systemMemoryMB, hits, misses, hitRatio, invalidations)
}
