package main

import (
	"net/http"
	"testing"
	"time"
)

func TestCache_GetMiss(t *testing.T) {
	c := NewCache(10)
	_, ok := c.Get("missing")
	if ok {
		t.Fatal("expected miss, got hit")
	}
}

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache(10)
	e := &cacheEntry{statusCode: 200, header: http.Header{"Foo": {"bar"}}, body: []byte("hello")}
	c.Set("key", e)

	got, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != e {
		t.Error("returned wrong entry")
	}
}

func TestCache_ExpiredEntry(t *testing.T) {
	c := NewCache(10)
	e := &cacheEntry{statusCode: 200, body: []byte("stale"), expiresAt: time.Now().Add(-time.Second)}
	c.Set("key", e)

	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected miss for expired entry, got hit")
	}
}

func TestCache_NeverExpires(t *testing.T) {
	c := NewCache(10)
	e := &cacheEntry{statusCode: 200, body: []byte("static")} // zero expiresAt
	c.Set("key", e)

	_, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit for zero-expiry entry, got miss")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache(2)
	a := &cacheEntry{body: []byte("a")}
	b := &cacheEntry{body: []byte("b")}
	cc := &cacheEntry{body: []byte("c")}

	c.Set("a", a)
	c.Set("b", b)
	c.Set("c", cc) // should evict "a" (LRU)

	if _, ok := c.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Error("expected 'b' to still be present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("expected 'c' to be present")
	}
}

func TestCache_LRUAccessUpdatesOrder(t *testing.T) {
	c := NewCache(2)
	a := &cacheEntry{body: []byte("a")}
	b := &cacheEntry{body: []byte("b")}
	cc := &cacheEntry{body: []byte("c")}

	c.Set("a", a)
	c.Set("b", b)
	c.Get("a") // promote "a" to front, making "b" the LRU
	c.Set("c", cc) // should evict "b"

	if _, ok := c.Get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("expected 'a' to still be present")
	}
}

func TestCache_SetOverwritesExisting(t *testing.T) {
	c := NewCache(10)
	e1 := &cacheEntry{body: []byte("first")}
	e2 := &cacheEntry{body: []byte("second")}
	c.Set("key", e1)
	c.Set("key", e2)

	got, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit")
	}
	if string(got.body) != "second" {
		t.Errorf("expected 'second', got %q", got.body)
	}
}

func TestCache_ZeroCapDisablesCaching(t *testing.T) {
	c := NewCache(0)
	c.Set("key", &cacheEntry{body: []byte("data")})
	if _, ok := c.Get("key"); ok {
		t.Error("expected no-op cache to always miss")
	}
}
