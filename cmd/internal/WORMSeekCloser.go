package internal

import (
	"io"
	"sync"
)

// WORMSeekCloser models something which can be read in parallel without
// using the file pointer (hence the ReaderAt), but can be written by one
// writer at a time (Writer). It can also be closed and seeked.
type WORMSeekCloser interface {
	io.ReaderAt
	io.Writer
	io.Closer
	io.Seeker
}

// ConcurrentWORMSeekCloser is safe for parallel use. It allows ReadAt
// calls to run in parallel, while other methods are serialized.
type ConcurrentWORMSeekCloser struct {
	worm  WORMSeekCloser
	mutex sync.RWMutex
}

// NewConcurrentWORMSeekCloser wraps a WORMSeekCloser and returns an object safe
// for concurrent use.
func NewConcurrentWORMSeekCloser(inferior WORMSeekCloser) *ConcurrentWORMSeekCloser {
	return &ConcurrentWORMSeekCloser{worm: inferior}
}

// ReadAt which is concurrency-safe
func (worm *ConcurrentWORMSeekCloser) ReadAt(p []byte, off int64) (n int, err error) {
	worm.mutex.RLock()
	defer worm.mutex.RUnlock()
	return worm.worm.ReadAt(p, off)
}

// Seek which is concurrency-safe
func (worm *ConcurrentWORMSeekCloser) Seek(offset int64, whence int) (int64, error) {
	worm.mutex.Lock()
	defer worm.mutex.Unlock()
	return worm.worm.Seek(offset, whence)
}

// Write which is concurrency-safe
func (worm *ConcurrentWORMSeekCloser) Write(p []byte) (n int, err error) {
	worm.mutex.Lock()
	defer worm.mutex.Unlock()
	return worm.worm.Write(p)
}

// Close which is concurrency-safe
func (worm *ConcurrentWORMSeekCloser) Close() error {
	worm.mutex.Lock()
	defer worm.mutex.Unlock()
	return worm.worm.Close()
}
