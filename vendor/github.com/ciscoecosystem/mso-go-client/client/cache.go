package client

import (
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
)

const (
	KB = 1024
	MB = 1024 * 1024
)

// Cache provides thread-safe caching with per-item statistics tracking and memory monitoring
type Cache struct {
	mu         sync.RWMutex
	items      map[string]*CacheItem
	totalBytes int64
}

// CacheItem represents a cached item with its own statistics
type CacheItem struct {
	Data          interface{}
	Size          int64
	Hits          int64
	Misses        int64
	Invalidations int64
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
		// Fallback for non-byte values
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

// Get gets and clones an item with per-item statistics tracking
func (cache *Cache) Get(key string, cloneFunc func(interface{}) (interface{}, error)) (interface{}, bool, error) {
	debugEnabled := isCacheDebugEnabled() // Check once to avoid repeated calls

	if debugEnabled {
		// Use write lock when we need to update statistics
		cache.mu.Lock()
		defer cache.mu.Unlock()

		item, found := cache.items[key]

		if found && item.Data != nil {
			// Clone while holding lock
			result, cloneErr := cloneFunc(item.Data)
			// Update statistics safely under same lock
			item.Hits++
			return result, true, cloneErr
		}

		// Handle miss cases with statistics
		if found && item.Data == nil {
			// Placeholder entry exists but no actual data - still a miss
			item.Misses++
		} else if !found {
			// No entry exists - create placeholder and record miss
			cache.items[key] = &CacheItem{
				Data:          nil, // No data yet
				Size:          0,
				Hits:          0,
				Misses:        1, // Record the miss
				Invalidations: 0,
			}
		}

		return nil, false, nil
	} else {
		// Use read lock when no statistics needed
		cache.mu.RLock()
		defer cache.mu.RUnlock()

		item, found := cache.items[key]

		if found && item.Data != nil {
			result, cloneErr := cloneFunc(item.Data)
			return result, true, cloneErr
		}

		return nil, false, nil
	}
}

// Delete removes an item from the cache with per-item invalidation tracking.
func (cache *Cache) Delete(key string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if item, exists := cache.items[key]; exists {
		// Update total bytes
		cache.totalBytes -= item.Size

		// Record invalidation in the item before deletion (only when debug enabled)
		if isCacheDebugEnabled() {
			item.Invalidations++
		}

		// Remove the item
		delete(cache.items, key)
	}
}

// Clear removes all items from the cache
func (cache *Cache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Record invalidations for all items before clearing (only when debug enabled)
	if isCacheDebugEnabled() {
		for _, item := range cache.items {
			item.Invalidations++
		}
	}

	cache.items = make(map[string]*CacheItem)
	cache.totalBytes = 0
}

// LogEvent logs cache events with comprehensive per-item statistics and optional size-only mode
func (cache *Cache) LogEvent(event, schemaId string, sizeOnly ...bool) {
	// Early return if debug logging is disabled
	if !isCacheDebugEnabled() {
		return
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	item, exists := cache.items[schemaId]
	if !exists || item == nil {
		log.Printf("[DEBUG] %s for %s | ItemStats: New item", event, schemaId)
		return
	}

	itemSizeKB := float64(item.Size) / KB
	systemMemoryMB := getSystemMemoryMB()

	// Check if size-only mode is requested
	if len(sizeOnly) > 0 && sizeOnly[0] {
		log.Printf("[DEBUG] %s for %s | ItemSize: %.1fKB | System: %.1fMB",
			event, schemaId, itemSizeKB, systemMemoryMB)
	} else {
		// Full statistics logging
		hitRatio := calculateHitRatio(item.Hits, item.Misses)
		log.Printf("[DEBUG] %s for %s | ItemStats: Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Size: %.1fKB | System: %.1fMB",
			event, schemaId, item.Hits, item.Misses, item.Invalidations, hitRatio, itemSizeKB, systemMemoryMB)
	}
}

// LogOperation logs cache operations with aggregated statistics across all items
func (cache *Cache) LogOperation(event string) {
	// Early return if debug logging is disabled
	if !isCacheDebugEnabled() {
		return
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// Calculate aggregate statistics directly
	var totalHits, totalMisses, totalInvalidations int64
	itemCount := len(cache.items)

	for _, item := range cache.items {
		totalHits += item.Hits
		totalMisses += item.Misses
		totalInvalidations += item.Invalidations
	}

	hitRatio := calculateHitRatio(totalHits, totalMisses)
	cacheSizeMB := float64(cache.totalBytes) / MB
	systemMemoryMB := getSystemMemoryMB()

	var avgItemKB float64
	if itemCount > 0 {
		avgItemKB = float64(cache.totalBytes) / float64(itemCount) / KB
	}

	log.Printf("[DEBUG] %s | AggregateStats: Items=%d, Hits=%d, Misses=%d, Invalidations=%d, HitRatio=%.1f%% | Memory: Cache=%.2fMB, AvgItem=%.1fKB, System=%.1fMB",
		event, itemCount, totalHits, totalMisses, totalInvalidations, hitRatio, cacheSizeMB, avgItemKB, systemMemoryMB)
}

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
	return float64(m.Alloc) / MB
}

// isCacheDebugEnabled checks if cache debug logging is enabled
// Checks TF_LOG environment variable for DEBUG or TRACE level
func isCacheDebugEnabled() bool {
	if tfLog := os.Getenv("TF_LOG"); tfLog != "" {
		logLevel := strings.ToUpper(tfLog)
		return logLevel == "DEBUG" || logLevel == "TRACE"
	}
	return false
}
