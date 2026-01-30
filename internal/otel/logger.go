package otel

// Goroutine safety:
// The drain goroutine is the sole reader of l.ch and the sole writer to l.w.
// Logger.mu protects only the l.buf pointer (read by drain, written by SetRingBuffer).
// The ring buffer's own mu handles concurrent Push/Snapshot/Last/Stats calls.
// No nested lock acquisition occurs: drain releases Logger.mu before calling rb.Push().

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// writerChanSize is the capacity of the async write channel.
	// At ~200 bytes/event, 4096 events buffers ~800KB.
	writerChanSize = 4096
)

// logEntry carries both serialized bytes (for disk) and the original Event
// (for ring buffer). This avoids a lossy JSON round-trip through the ring
// buffer — fields like Dur (json:"-") are preserved in the ring copy.
type logEntry struct {
	data []byte
	ev   Event
}

// Logger serializes events as JSONL via an async background writer.
// Goroutine-safe. All emitted events flow through a buffered channel
// to a drain goroutine that writes to disk and pushes to the ring buffer.
type Logger struct {
	mu        sync.Mutex
	buf       *RingBuffer    // nil until SetRingBuffer
	sessionID string         // random hex, set once at creation
	ch        chan logEntry   // buffered channel for async writes
	w         io.Writer      // destination (event log file)
	dropped   atomic.Uint64  // events dropped due to full channel, encode failure, or write error
	closed    atomic.Bool    // true after Close(); prevents send-on-closed-channel panic
	done      chan struct{}   // closed when drain goroutine exits
	closeOnce sync.Once
}

// NewLogger creates a Logger writing JSONL to w asynchronously.
// Starts a background drain goroutine. Call Close() to flush and stop.
func NewLogger(w io.Writer) *Logger {
	var sid [8]byte
	_, _ = rand.Read(sid[:])

	l := &Logger{
		sessionID: fmt.Sprintf("%x", sid[:]),
		ch:        make(chan logEntry, writerChanSize),
		w:         w,
		done:      make(chan struct{}),
	}
	go l.drain()
	return l
}

// NewNullLogger creates a Logger that discards output.
// Callers should still call Close() to stop the drain goroutine.
func NewNullLogger() *Logger {
	return NewLogger(io.Discard)
}

// drain is the background goroutine that reads from ch and writes to disk + ring buffer.
func (l *Logger) drain() {
	defer close(l.done)
	for entry := range l.ch {
		if _, err := l.w.Write(entry.data); err != nil {
			l.dropped.Add(1)
		}

		l.mu.Lock()
		rb := l.buf
		l.mu.Unlock()

		if rb != nil {
			rb.Push(entry.ev)
		}
	}
}

// Emit writes an event to the JSONL log (and ring buffer if attached).
// Sets Time (if zero) and SessionID. Goroutine-safe. Non-blocking: if the
// channel is full or the logger is closed, the event is dropped and the
// drop counter is incremented.
//
// Safe to call concurrently with Close(). If Close() races between the
// closed-flag check and the channel send, the resulting panic is recovered
// and the event is counted as dropped.
func (l *Logger) Emit(e Event) {
	defer func() {
		if recover() != nil {
			l.dropped.Add(1)
		}
	}()

	if l.closed.Load() {
		l.dropped.Add(1)
		return
	}

	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.SessionID = l.sessionID

	data, err := json.Marshal(e)
	if err != nil {
		l.dropped.Add(1)
		return
	}
	data = append(data, '\n')

	select {
	case l.ch <- logEntry{data: data, ev: e}:
	default:
		l.dropped.Add(1)
	}
}

// Info emits an info-level event.
func (l *Logger) Info(kind EventKind, comp string, msg string) {
	l.Emit(Event{Level: LevelInfo, Kind: kind, Comp: comp, Msg: msg})
}

// Error emits an error-level event. Nil err is safe (logged as empty string).
func (l *Logger) Error(kind EventKind, comp string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	l.Emit(Event{Level: LevelError, Kind: kind, Comp: comp, Err: errStr})
}

// Warn emits a warn-level event.
func (l *Logger) Warn(kind EventKind, comp string, msg string) {
	l.Emit(Event{Level: LevelWarn, Kind: kind, Comp: comp, Msg: msg})
}

// SetRingBuffer attaches a ring buffer for live inspection.
func (l *Logger) SetRingBuffer(buf *RingBuffer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = buf
}

// Dropped returns the number of events dropped since creation.
func (l *Logger) Dropped() uint64 {
	return l.dropped.Load()
}

// Close flushes pending events, stops the drain goroutine, and reports
// any dropped events to stderr. Safe to call from goroutines that may
// still be calling Emit() — those calls will be dropped, not panicked.
func (l *Logger) Close() {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		close(l.ch)
		<-l.done

		if d := l.dropped.Load(); d > 0 {
			fmt.Fprintf(os.Stderr, "observer: %d events dropped during session %s\n", d, l.sessionID)
		}
	})
}
