package main

import (
	"container/list"
	"net/http"
	"sync"
	"time"
)

// cacheEntry holds a buffered HTTP response for replay.
// expiresAt is zero for entries that never expire (static routes).
type cacheEntry struct {
	statusCode int
	header     http.Header
	body       []byte
	expiresAt  time.Time
}

func (e *cacheEntry) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

type lruItem struct {
	key   string
	entry *cacheEntry
}

// Cache is a thread-safe LRU cache with optional per-entry time-based expiry.
type Cache struct {
	mu    sync.Mutex
	cap   int
	items map[string]*list.Element
	list  *list.List
}

// NewCache creates a new Cache with the given capacity.
// cap <= 0 disables caching (Get always misses, Set is a no-op).
func NewCache(cap int) *Cache {
	return &Cache{
		cap:   cap,
		items: make(map[string]*list.Element),
		list:  list.New(),
	}
}

// Get returns the cached entry for key if it exists and has not expired.
func (c *Cache) Get(key string) (*cacheEntry, bool) {
	if c.cap <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	item := el.Value.(*lruItem)
	if item.entry.expired() {
		c.list.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.list.MoveToFront(el)
	return item.entry, true
}

// Set stores entry under key, evicting the least-recently-used entry if at capacity.
func (c *Cache) Set(key string, entry *cacheEntry) {
	if c.cap <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.list.MoveToFront(el)
		el.Value.(*lruItem).entry = entry
		return
	}
	if c.list.Len() >= c.cap {
		oldest := c.list.Back()
		if oldest != nil {
			c.list.Remove(oldest)
			delete(c.items, oldest.Value.(*lruItem).key)
		}
	}
	el := c.list.PushFront(&lruItem{key: key, entry: entry})
	c.items[key] = el
}
