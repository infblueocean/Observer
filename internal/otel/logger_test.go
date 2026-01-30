package otel

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEmitWritesValidJSONL(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Emit(Event{Kind: KindFetchStart, Level: LevelInfo, Comp: "coord"})
	l.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded["kind"] != "fetch.start" {
		t.Errorf("expected kind=fetch.start, got %v", decoded["kind"])
	}
	if decoded["level"] != "info" {
		t.Errorf("expected level=info, got %v", decoded["level"])
	}
	if decoded["comp"] != "coord" {
		t.Errorf("expected comp=coord, got %v", decoded["comp"])
	}
}

func TestEmitSetsTimeAndSessionID(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	before := time.Now()
	l.Emit(Event{Kind: KindStartup})
	l.Close()
	after := time.Now()

	var ev Event
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ev.Time.Before(before) || ev.Time.After(after) {
		t.Errorf("time %v not in [%v, %v]", ev.Time, before, after)
	}
	if ev.SessionID == "" {
		t.Error("session_id should be set")
	}
	if len(ev.SessionID) != 16 {
		t.Errorf("session_id should be 16 hex chars, got %d: %q", len(ev.SessionID), ev.SessionID)
	}
}

func TestDurToMs(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Emit(Event{Kind: KindFetchComplete, Dur: 1500 * time.Millisecond})
	l.Close()

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	durMs, ok := decoded["dur_ms"].(float64)
	if !ok {
		t.Fatal("dur_ms not present or not float64")
	}
	if durMs != 1500 {
		t.Errorf("expected dur_ms=1500, got %v", durMs)
	}
}

func TestOmitempty(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Emit(Event{Kind: KindStartup})
	l.Close()

	line := strings.TrimSpace(buf.String())
	for _, field := range []string{"dur_ms", "count", "source", "query", "dims", "err", "msg", "extra", "qid"} {
		if strings.Contains(line, `"`+field+`"`) {
			t.Errorf("expected field %q to be omitted, but found in: %s", field, line)
		}
	}
}

func TestConcurrentEmit(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Emit(Event{Kind: KindFetchStart, Comp: "test"})
		}()
	}
	wg.Wait()
	l.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 100 {
		t.Errorf("expected 100 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestNullLogger(t *testing.T) {
	l := NewNullLogger()
	l.Emit(Event{Kind: KindStartup})
	l.Close()
	// no panic = pass
}

func TestClose(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Emit(Event{Kind: KindStartup, Msg: "start"})
	l.Emit(Event{Kind: KindShutdown, Msg: "stop"})
	l.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after Close, got %d", len(lines))
	}

	// Idempotent close
	l.Close()
}

func TestDropCounter(t *testing.T) {
	// Use a blocking writer that holds up the drain goroutine while we flood the channel.
	bw := &blockingWriter{
		started: make(chan struct{}),
		block:   make(chan struct{}),
	}
	l := NewLogger(bw)

	// First emit gets picked up by drain, which blocks on write.
	l.Emit(Event{Kind: KindFetchStart})
	<-bw.started // wait for drain to enter Write (deterministic, no sleep)

	// Now flood: channel capacity is writerChanSize, so writerChanSize+10 should cause drops.
	for i := 0; i < writerChanSize+10; i++ {
		l.Emit(Event{Kind: KindFetchStart})
	}

	dropped := l.Dropped()
	if dropped == 0 {
		t.Error("expected some drops when channel is full, got 0")
	}

	close(bw.block) // unblock writer
	l.Close()
}

type blockingWriter struct {
	started chan struct{} // closed when first Write begins
	block   chan struct{} // closed to unblock writer
	once    sync.Once
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.started) // signal that drain has entered Write
		<-w.block        // block until test is done flooding
	})
	return len(p), nil
}

func TestConvenienceHelpers(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Info(KindStartup, "main", "starting")
	l.Warn(KindFetchError, "fetch", "timeout")
	l.Error(KindError, "coord", errForTest("disk full"))
	l.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	tests := []struct {
		level string
		kind  string
		comp  string
	}{
		{"info", "sys.startup", "main"},
		{"warn", "fetch.error", "fetch"},
		{"error", "sys.error", "coord"},
	}
	for i, tt := range tests {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &decoded); err != nil {
			t.Errorf("line %d: %v", i, err)
			continue
		}
		if decoded["level"] != tt.level {
			t.Errorf("line %d: level=%v, want %v", i, decoded["level"], tt.level)
		}
		if decoded["kind"] != tt.kind {
			t.Errorf("line %d: kind=%v, want %v", i, decoded["kind"], tt.kind)
		}
		if decoded["comp"] != tt.comp {
			t.Errorf("line %d: comp=%v, want %v", i, decoded["comp"], tt.comp)
		}
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }

func TestSessionIDConsistent(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Emit(Event{Kind: KindStartup})
	l.Emit(Event{Kind: KindShutdown})
	l.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var ev1, ev2 map[string]any
	json.Unmarshal([]byte(lines[0]), &ev1)
	json.Unmarshal([]byte(lines[1]), &ev2)

	sid1 := ev1["session_id"].(string)
	sid2 := ev2["session_id"].(string)
	if sid1 != sid2 {
		t.Errorf("session IDs differ: %q vs %q", sid1, sid2)
	}
}
