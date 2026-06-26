package sidecar

import (
	"sync"
	"time"
)

// LogLine is a single captured log entry.
type LogLine struct {
	Timestamp time.Time
	Level     string // "info", "warn", "error"
	Message   string
	Source    string // "sidecar", "executor", "exec"
}

// LogBuffer is a thread-safe ring buffer that also supports follow-mode subscribers.
type LogBuffer struct {
	mu   sync.RWMutex
	buf  []LogLine
	cap  int
	head int // next write position
	full bool
	subs map[chan LogLine]struct{}
}

// NewLogBuffer creates a ring buffer that holds up to cap entries.
func NewLogBuffer(cap int) *LogBuffer {
	return &LogBuffer{
		buf:  make([]LogLine, cap),
		cap:  cap,
		subs: make(map[chan LogLine]struct{}),
	}
}

// Append adds a log line and notifies all followers.
func (b *LogBuffer) Append(line LogLine) {
	b.mu.Lock()
	b.buf[b.head] = line
	b.head = (b.head + 1) % b.cap
	if b.head == 0 && !b.full {
		b.full = true
	}
	// Copy subscribers under lock, send outside
	subs := make([]chan LogLine, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- line:
		default:
			// slow subscriber, drop
		}
	}
}

// Tail returns the most recent n lines (or all if n <= 0).
func (b *LogBuffer) Tail(n int) []LogLine {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := b.count()
	if n <= 0 || n > total {
		n = total
	}

	result := make([]LogLine, 0, n)
	start := (b.head - n + b.cap) % b.cap
	if !b.full && start > b.head {
		start = 0
		n = b.head
	}
	for i := 0; i < n; i++ {
		idx := (start + i) % b.cap
		result = append(result, b.buf[idx])
	}
	return result
}

func (b *LogBuffer) count() int {
	if b.full {
		return b.cap
	}
	return b.head
}

// Subscribe returns a channel that receives new log lines. Call Unsubscribe when done.
func (b *LogBuffer) Subscribe() chan LogLine {
	ch := make(chan LogLine, 128)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a follow channel.
func (b *LogBuffer) Unsubscribe(ch chan LogLine) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	// drain
	for range ch {
	}
}
