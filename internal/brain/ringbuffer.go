package brain

import (
	"sync"
)

// SensorySignalRingBuffer is a thread-safe circular buffer for SensorySignal.
type SensorySignalRingBuffer struct {
	mu       sync.Mutex
	data     []SensorySignal
	capacity int
	head     int
	tail     int
	size     int
}

// NewSensorySignalRingBuffer constructs a buffer with the given capacity.
func NewSensorySignalRingBuffer(capacity int) *SensorySignalRingBuffer {
	return &SensorySignalRingBuffer{
		data:     make([]SensorySignal, capacity),
		capacity: capacity,
	}
}

// Push adds a signal. If the buffer is full, the oldest signal is evicted.
func (r *SensorySignalRingBuffer) Push(sig SensorySignal) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.capacity <= 0 {
		return
	}

	if r.size == r.capacity {
		// Evict oldest (head)
		r.head = (r.head + 1) % r.capacity
		r.size--
	}

	r.data[r.tail] = sig
	r.tail = (r.tail + 1) % r.capacity
	r.size++
}

// Pop retrieves the oldest signal. Returns false if empty.
func (r *SensorySignalRingBuffer) Pop() (SensorySignal, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		return SensorySignal{}, false
	}

	sig := r.data[r.head]
	r.data[r.head] = SensorySignal{} // GC friendly
	r.head = (r.head + 1) % r.capacity
	r.size--

	return sig, true
}

// Size returns the current number of elements.
func (r *SensorySignalRingBuffer) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// EvictAndPushChannel acts as a channel-based ring buffer.
// If the channel is full, it evicts the oldest element in the channel
// and pushes the new signal. This avoids blocking and maintains FIFO order.
func EvictAndPushChannel(ch chan SensorySignal, sig SensorySignal) {
	select {
	case ch <- sig:
		// Sent successfully
	default:
		// Channel is full. Evict the oldest element from the channel (FIFO head)
		select {
		case <-ch:
		default:
		}
		// Write the new signal (non-blocking)
		select {
		case ch <- sig:
		default:
		}
	}
}
