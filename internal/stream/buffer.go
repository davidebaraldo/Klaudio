package stream

import "sync"

const DefaultBufferSize = 10 * 1024 * 1024 // 10MB

// RingBuffer is a fixed-size circular buffer that stores the most recent bytes
// written to it. When the buffer is full, new writes overwrite the oldest data.
// It is safe for concurrent use.
type RingBuffer struct {
	mu       sync.Mutex
	data     []byte
	size     int
	writePos int
	written  int // total bytes ever written (for offset tracking)
}

// NewRingBuffer creates a ring buffer with the given capacity in bytes.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	return &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
}

// Write appends p to the buffer, wrapping around when the end is reached.
// Implements io.Writer.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)
	if n == 0 {
		return 0, nil
	}

	// If the incoming data is larger than the buffer, only keep the tail.
	if n >= rb.size {
		copy(rb.data, p[n-rb.size:])
		rb.writePos = 0
		rb.written += n
		return n, nil
	}

	// How much fits before we need to wrap?
	firstChunk := rb.size - rb.writePos
	if firstChunk >= n {
		copy(rb.data[rb.writePos:], p)
	} else {
		copy(rb.data[rb.writePos:], p[:firstChunk])
		copy(rb.data, p[firstChunk:])
	}

	rb.writePos = (rb.writePos + n) % rb.size
	rb.written += n
	return n, nil
}

// ReadAll returns all buffered content ordered from oldest to newest.
// Used for backfill when a client connects late.
func (rb *RingBuffer) ReadAll() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.written == 0 {
		return nil
	}

	// Buffer has not wrapped yet.
	if rb.written <= rb.size {
		used := rb.written
		if used > rb.size {
			used = rb.size
		}
		out := make([]byte, rb.writePos)
		copy(out, rb.data[:rb.writePos])
		return out
	}

	// Buffer has wrapped: data goes from writePos..end then 0..writePos.
	out := make([]byte, rb.size)
	tail := rb.size - rb.writePos
	copy(out, rb.data[rb.writePos:])
	copy(out[tail:], rb.data[:rb.writePos])
	return out
}

// ReadFrom reads data starting from the given logical offset (number of bytes
// written since buffer creation). Returns nil if the offset is out of range.
func (rb *RingBuffer) ReadFrom(offset int) []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.written == 0 || offset >= rb.written {
		return nil
	}

	// Earliest available offset: everything before this has been overwritten.
	earliest := rb.written - rb.size
	if earliest < 0 {
		earliest = 0
	}
	if offset < earliest {
		offset = earliest
	}

	available := rb.written - offset

	// Calculate the physical start position.
	physStart := (rb.writePos - available%rb.size + rb.size) % rb.size
	if available > rb.size {
		available = rb.size
		physStart = rb.writePos
	}

	out := make([]byte, available)
	firstChunk := rb.size - physStart
	if firstChunk >= available {
		copy(out, rb.data[physStart:physStart+available])
	} else {
		copy(out, rb.data[physStart:])
		copy(out[firstChunk:], rb.data[:available-firstChunk])
	}
	return out
}

// Len returns the number of bytes currently stored in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.written < rb.size {
		return rb.written
	}
	return rb.size
}
