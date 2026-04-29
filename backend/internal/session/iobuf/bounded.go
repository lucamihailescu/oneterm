// Package iobuf hosts the bounded session output buffer in a leaf package
// with no DB dependency, so its behavior can be unit-tested without spinning
// up MySQL (the parent session package opens DB connections at init time).
package iobuf

import (
	"bytes"
	"sync"
)

// DefaultOutBufMax bounds the in-memory output buffer for a single session.
// 4 MiB matches the common upper bound of terminal scrollback that a client
// can reasonably display, and is small enough that even thousands of idle
// sessions cannot exhaust process memory. Verbose commands still record to
// the on-disk replay file, so nothing is lost from the audit trail.
const DefaultOutBufMax = 4 * 1024 * 1024

// BoundedBuffer is a drop-in replacement for *bytes.Buffer that caps the
// retained byte count. When a write would exceed the cap, the oldest bytes
// are discarded to make room; the most-recent terminal output is what the
// client cares about, and recording happens through a separate path
// (SshRecoder.Write in protocols/utils.go), so eviction here only affects
// the live websocket frame about to be flushed.
type BoundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func NewBoundedBuffer(max int) *BoundedBuffer {
	if max <= 0 {
		max = DefaultOutBufMax
	}
	return &BoundedBuffer{max: max}
}

// Write appends p, evicting oldest bytes if the result would exceed max.
// If p alone exceeds max, only the trailing max bytes of p are retained.
func (b *BoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)

	if len(p) >= b.max {
		b.buf.Reset()
		b.buf.Write(p[len(p)-b.max:])
		return written, nil
	}

	if overflow := b.buf.Len() + len(p) - b.max; overflow > 0 {
		b.buf.Next(overflow)
	}
	b.buf.Write(p)
	return written, nil
}

func (b *BoundedBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

// Bytes returns a copy of the current contents. Returning a copy avoids
// data races between the caller iterating over the slice and a concurrent
// Write that triggers eviction (which calls bytes.Buffer.Next, mutating the
// underlying storage).
func (b *BoundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	src := b.buf.Bytes()
	if len(src) == 0 {
		return nil
	}
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

func (b *BoundedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *BoundedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}
