// Package cache holds the read-through caching policy a resource can declare
// and the in-process cache that enforces it. Caching is off by default. A
// resource opts in from its definition:
//
//	var Todos = resource.Of[Todo]("todos").
//		Store(store.Memory[Todo]()).
//		Caching(
//			cache.Enabled(),
//			cache.TTL(15*time.Minute),
//		).
//		Build()
//
// Cache entries are always keyed by tenant as well as by record, so a cached
// read can never cross an org boundary.
package cache

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Policy is the caching configuration for one resource. The zero value means
// caching is disabled.
type Policy struct {
	Enabled bool
	TTL     time.Duration // how long an entry may be served before it is refetched
}

// Option mutates a Policy. Options are applied in the order they are given,
// so a later option overrides an earlier one.
type Option func(*Policy)

// Enabled turns caching on for the resource. An enabled policy must also set
// a positive TTL; entries are never cached indefinitely.
func Enabled() Option { return func(p *Policy) { p.Enabled = true } }

// Disabled turns caching off. It exists so a shared base definition that
// enables caching can be overridden by a resource that must always read
// through to the store.
func Disabled() Option { return func(p *Policy) { p.Enabled = false } }

// TTL sets how long an entry may be served before it is refetched.
func TTL(d time.Duration) Option { return func(p *Policy) { p.TTL = d } }

// Apply returns a copy of p with the options applied in order.
func (p Policy) Apply(opts ...Option) Policy {
	for _, o := range opts {
		o(&p)
	}
	return p
}

// Validate reports whether the policy is usable. It is called by
// resource.Builder.Build, so an invalid policy fails at program start.
func (p Policy) Validate() error {
	if p.TTL < 0 {
		return fmt.Errorf("cache: ttl %s is negative", p.TTL)
	}
	if p.Enabled && p.TTL == 0 {
		return errors.New("cache: enabled without a ttl; entries are never cached indefinitely")
	}
	return nil
}

// Cache is an in-process TTL cache of T keyed by string. A nil *Cache is
// valid and never hits, which is how a disabled policy is represented, so
// callers need no branch on whether caching is on.
type Cache[T any] struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	items map[string]entry[T]
}

type entry[T any] struct {
	value   T
	expires time.Time
}

// New returns a Cache for the policy, or nil when the policy is disabled.
func New[T any](p Policy) *Cache[T] {
	if !p.Enabled {
		return nil
	}
	return &Cache[T]{ttl: p.TTL, now: time.Now, items: map[string]entry[T]{}}
}

// Get returns the cached value for key if it is present and unexpired.
func (c *Cache[T]) Get(key string) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return zero, false
	}
	if !c.now().Before(e.expires) {
		delete(c.items, key)
		return zero, false
	}
	return e.value, true
}

// Set stores v under key for the policy's TTL.
func (c *Cache[T]) Set(key string, v T) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry[T]{value: v, expires: c.now().Add(c.ttl)}
}

// Delete removes key, if present.
func (c *Cache[T]) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}
