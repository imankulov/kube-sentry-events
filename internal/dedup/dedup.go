package dedup

import (
	"sync"
	"time"
)

const (
	// MaxEntries is the maximum number of entries in the cache.
	MaxEntries = 10000
)

// entry represents a cached event.
type entry struct {
	key       string
	expiresAt time.Time
	count     int
	firstSeen time.Time
	lastSeen  time.Time
}

// Deduplicator prevents sending duplicate events within a time window.
type Deduplicator struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]*entry
	order   []string // LRU order tracking
}

// New creates a new deduplicator with the given time window.
func New(window time.Duration) *Deduplicator {
	d := &Deduplicator{
		window:  window,
		entries: make(map[string]*entry),
		order:   make([]string, 0),
	}
	go d.cleanupLoop()
	return d
}

// Check returns true if this is a new event (should be sent),
// false if it's a duplicate (should be skipped).
// Also returns the count of occurrences and first/last seen times.
func (d *Deduplicator) Check(namespace, pod, reason string) (isNew bool, count int, firstSeen, lastSeen time.Time) {
	key := namespace + "/" + pod + "/" + reason
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if e, exists := d.entries[key]; exists {
		if now.Before(e.expiresAt) {
			// Still within window, increment count and update lastSeen
			e.count++
			e.lastSeen = now
			e.expiresAt = now.Add(d.window) // Extend window
			return false, e.count, e.firstSeen, e.lastSeen
		}
		// Expired, treat as new
		delete(d.entries, key)
	}

	// New entry
	d.addEntry(key, now)
	return true, 1, now, now
}

// GetStats returns the count and timestamps for an event without marking it.
func (d *Deduplicator) GetStats(namespace, pod, reason string) (count int, firstSeen, lastSeen time.Time, exists bool) {
	key := namespace + "/" + pod + "/" + reason

	d.mu.Lock()
	defer d.mu.Unlock()

	if e, ok := d.entries[key]; ok && time.Now().Before(e.expiresAt) {
		return e.count, e.firstSeen, e.lastSeen, true
	}
	return 0, time.Time{}, time.Time{}, false
}

func (d *Deduplicator) addEntry(key string, now time.Time) {
	// Evict oldest if at capacity
	for len(d.entries) >= MaxEntries && len(d.order) > 0 {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.entries, oldest)
	}

	e := &entry{
		key:       key,
		expiresAt: now.Add(d.window),
		count:     1,
		firstSeen: now,
		lastSeen:  now,
	}
	d.entries[key] = e
	d.order = append(d.order, key)
}

func (d *Deduplicator) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		d.cleanup()
	}
}

func (d *Deduplicator) cleanup() {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove expired entries
	newOrder := make([]string, 0, len(d.order))
	for _, key := range d.order {
		if e, exists := d.entries[key]; exists {
			if now.Before(e.expiresAt) {
				newOrder = append(newOrder, key)
			} else {
				delete(d.entries, key)
			}
		}
	}
	d.order = newOrder
}

// Size returns the current number of entries in the cache.
func (d *Deduplicator) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}
