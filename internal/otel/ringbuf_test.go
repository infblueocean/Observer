package otel

import (
	"sync"
	"testing"
)

func TestPushAndSnapshot(t *testing.T) {
	r := NewRingBuffer(8)
	for i := 0; i < 5; i++ {
		r.Push(Event{Kind: KindFetchStart, Count: i})
	}

	snap := r.Snapshot()
	if len(snap) != 5 {
		t.Fatalf("expected 5 events, got %d", len(snap))
	}
	for i, e := range snap {
		if e.Count != i {
			t.Errorf("snap[%d].Count=%d, want %d", i, e.Count, i)
		}
	}
}

func TestWrapAround(t *testing.T) {
	r := NewRingBuffer(4)
	for i := 0; i < 8; i++ {
		r.Push(Event{Kind: KindFetchStart, Count: i})
	}

	snap := r.Snapshot()
	if len(snap) != 4 {
		t.Fatalf("expected 4 events, got %d", len(snap))
	}
	// Should contain events 4, 5, 6, 7 (oldest evicted)
	for i, e := range snap {
		want := i + 4
		if e.Count != want {
			t.Errorf("snap[%d].Count=%d, want %d", i, e.Count, want)
		}
	}
}

func TestLast(t *testing.T) {
	r := NewRingBuffer(8)
	for i := 0; i < 8; i++ {
		r.Push(Event{Kind: KindFetchStart, Count: i})
	}

	last3 := r.Last(3)
	if len(last3) != 3 {
		t.Fatalf("expected 3, got %d", len(last3))
	}
	// Should be 5, 6, 7
	for i, e := range last3 {
		want := i + 5
		if e.Count != want {
			t.Errorf("last3[%d].Count=%d, want %d", i, e.Count, want)
		}
	}
}

func TestLastMoreThanCount(t *testing.T) {
	r := NewRingBuffer(8)
	r.Push(Event{Kind: KindStartup, Count: 1})
	r.Push(Event{Kind: KindShutdown, Count: 2})

	last := r.Last(100)
	if len(last) != 2 {
		t.Fatalf("expected 2, got %d", len(last))
	}
}

func TestLastWrapped(t *testing.T) {
	r := NewRingBuffer(4)
	for i := 0; i < 6; i++ {
		r.Push(Event{Kind: KindFetchStart, Count: i})
	}
	// Buffer has [4,5, 2,3] with head=2, count=4
	// Events in order: 2, 3, 4, 5
	last2 := r.Last(2)
	if len(last2) != 2 {
		t.Fatalf("expected 2, got %d", len(last2))
	}
	if last2[0].Count != 4 || last2[1].Count != 5 {
		t.Errorf("expected [4,5], got [%d,%d]", last2[0].Count, last2[1].Count)
	}
}

func TestStats(t *testing.T) {
	r := NewRingBuffer(16)
	r.Push(Event{Kind: KindFetchStart})
	r.Push(Event{Kind: KindFetchStart})
	r.Push(Event{Kind: KindFetchComplete})
	r.Push(Event{Kind: KindFetchError})
	r.Push(Event{Kind: KindFetchError})
	r.Push(Event{Kind: KindFetchError})

	stats := r.Stats()
	if stats[KindFetchStart] != 2 {
		t.Errorf("fetch.start=%d, want 2", stats[KindFetchStart])
	}
	if stats[KindFetchComplete] != 1 {
		t.Errorf("fetch.complete=%d, want 1", stats[KindFetchComplete])
	}
	if stats[KindFetchError] != 3 {
		t.Errorf("fetch.error=%d, want 3", stats[KindFetchError])
	}
}

func TestConcurrentPushSnapshot(t *testing.T) {
	r := NewRingBuffer(256)
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Push(Event{Kind: KindFetchStart})
			}
		}()
	}

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = r.Snapshot()
				_ = r.Last(10)
				_ = r.Stats()
			}
		}()
	}

	wg.Wait()
}

func TestEmptySnapshot(t *testing.T) {
	r := NewRingBuffer(8)
	snap := r.Snapshot()
	if snap != nil {
		t.Errorf("expected nil, got %v", snap)
	}
}

func TestLen(t *testing.T) {
	r := NewRingBuffer(4)
	if r.Len() != 0 {
		t.Errorf("expected 0, got %d", r.Len())
	}

	r.Push(Event{Kind: KindStartup})
	if r.Len() != 1 {
		t.Errorf("expected 1, got %d", r.Len())
	}

	for i := 0; i < 10; i++ {
		r.Push(Event{Kind: KindFetchStart})
	}
	if r.Len() != 4 {
		t.Errorf("expected 4 (capped at size), got %d", r.Len())
	}
}

func TestDeepCopyExtra(t *testing.T) {
	r := NewRingBuffer(4)
	extra := map[string]any{"key": "original"}
	r.Push(Event{Kind: KindStartup, Extra: extra})

	// Mutate original
	extra["key"] = "mutated"

	snap := r.Snapshot()
	if snap[0].Extra["key"] != "original" {
		t.Errorf("extra was aliased: got %v, want 'original'", snap[0].Extra["key"])
	}
}

func TestDefaultRingSize(t *testing.T) {
	r := NewRingBuffer(0)
	if r.size != DefaultRingSize {
		t.Errorf("expected default size %d, got %d", DefaultRingSize, r.size)
	}
}

func TestRingBufferWithLogger(t *testing.T) {
	r := NewRingBuffer(16)
	l := NewNullLogger()
	l.SetRingBuffer(r)

	l.Emit(Event{Kind: KindStartup, Msg: "hello"})
	l.Emit(Event{Kind: KindShutdown, Msg: "bye"})
	l.Close() // Close waits for drain to finish; no sleep needed

	if r.Len() != 2 {
		t.Errorf("expected 2 events in ring buffer, got %d", r.Len())
	}
	last := r.Last(2)
	if last[0].Kind != KindStartup {
		t.Errorf("expected KindStartup, got %v", last[0].Kind)
	}
	if last[1].Kind != KindShutdown {
		t.Errorf("expected KindShutdown, got %v", last[1].Kind)
	}
}

func TestCap(t *testing.T) {
	r := NewRingBuffer(64)
	if r.Cap() != 64 {
		t.Errorf("Cap() = %d, want 64", r.Cap())
	}

	r2 := NewRingBuffer(0) // defaults to DefaultRingSize
	if r2.Cap() != DefaultRingSize {
		t.Errorf("Cap() = %d, want %d", r2.Cap(), DefaultRingSize)
	}
}

func TestLastNegative(t *testing.T) {
	r := NewRingBuffer(8)
	r.Push(Event{Kind: KindStartup})

	if got := r.Last(-1); got != nil {
		t.Errorf("Last(-1) = %v, want nil", got)
	}
	if got := r.Last(0); got != nil {
		t.Errorf("Last(0) = %v, want nil", got)
	}
}
