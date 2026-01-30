// Package otel provides structured observability for Observer.
//
// Events are typed structs serialized as JSONL lines. The Logger writes
// events asynchronously via a buffered channel and background drain goroutine.
// An optional RingBuffer provides live in-memory inspection for the debug overlay.
package otel

import (
	"encoding/json"
	"time"
)

// Level defines event severity for filtering.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// EventKind identifies the category of an observability event.
// Dot-delimited: "<subsystem>.<action>".
type EventKind string

const (
	// Pipeline events
	KindFetchStart    EventKind = "fetch.start"
	KindFetchComplete EventKind = "fetch.complete"
	KindFetchError    EventKind = "fetch.error"
	KindEmbedStart    EventKind = "embed.start"
	KindEmbedComplete EventKind = "embed.complete"
	KindEmbedBatch    EventKind = "embed.batch"
	KindEmbedError    EventKind = "embed.error"

	// Search events
	KindSearchStart    EventKind = "search.start"
	KindSearchPool     EventKind = "search.pool"
	KindQueryEmbed     EventKind = "search.query_embed"
	KindCosineRerank   EventKind = "search.cosine_rerank"
	KindCrossEncoder   EventKind = "search.cross_encoder"
	KindSearchComplete EventKind = "search.complete"
	KindSearchCancel   EventKind = "search.cancel"

	// Store events
	KindStoreError EventKind = "store.error"

	// UI events
	KindKeyPress   EventKind = "ui.key"
	KindViewRender EventKind = "ui.render"

	// System events
	KindStartup  EventKind = "sys.startup"
	KindShutdown EventKind = "sys.shutdown"
	KindError    EventKind = "sys.error"

	// Trace events (Priority 5)
	KindMsgReceived EventKind = "trace.msg_received"
	KindMsgHandled  EventKind = "trace.msg_handled"
)

// Event is the universal observability record. Every field except Kind and
// Time is optional. Serialized as a single JSONL line.
type Event struct {
	Time      time.Time      `json:"t"`
	Level     Level          `json:"level,omitempty"`
	Kind      EventKind      `json:"kind"`
	Comp      string         `json:"comp,omitempty"`       // component: "coord", "ui", "fetch", "main"
	SessionID string         `json:"session_id,omitempty"` // random hex, same for entire app run
	QueryID   string         `json:"qid,omitempty"`        // search correlation ID
	Dur       time.Duration  `json:"-"`                    // not serialized directly
	DurMs     float64        `json:"dur_ms,omitempty"`     // computed from Dur at marshal time
	Count     int            `json:"count,omitempty"`
	Source    string         `json:"source,omitempty"`
	Query     string         `json:"query,omitempty"`
	Dims      int            `json:"dims,omitempty"`
	Err       string         `json:"err,omitempty"`
	Msg       string         `json:"msg,omitempty"`       // free text
	Extra     map[string]any `json:"extra,omitempty"`     // escape hatch for unusual fields
}

// MarshalJSON implements json.Marshaler, converting Dur to DurMs.
func (e Event) MarshalJSON() ([]byte, error) {
	type Alias Event
	a := struct {
		Alias
	}{Alias: Alias(e)}
	if e.Dur > 0 {
		a.DurMs = float64(e.Dur) / float64(time.Millisecond)
	}
	return json.Marshal(a)
}
