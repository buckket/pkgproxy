package internal

import "sync"

// Cache is a map protected by a Mutex, which can only be accessed
// via the method LockedDo.
type Cache[K comparable, V any] struct {
	cache map[K]V
	mutex sync.Mutex
}

// LockedDo executes a function with the cache mutex held, so that
// f is the only user at the moment. The mutex is released as soon as
// f returns.
func (c *Cache[K, V]) LockedDo(f func(cache map[K]V) error) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.cache == nil {
		c.cache = make(map[K]V)
	}
	return f(c.cache)
}
