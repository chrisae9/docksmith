package compose

import "sync"

// KeyedMutex serializes critical sections that share a key — used to make
// compose read-modify-write cycles on the same file atomic.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// NewKeyedMutex returns an empty KeyedMutex ready for use.
func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: make(map[string]*keyedLock)}
}

// Lock blocks until the key is free and returns the matching unlock.
func (k *KeyedMutex) Lock(key string) func() {
	k.mu.Lock()
	entry, ok := k.locks[key]
	if !ok {
		entry = &keyedLock{}
		k.locks[key] = entry
	}
	entry.refs++
	k.mu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()

		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
