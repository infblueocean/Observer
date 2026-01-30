package otel

import (
	"os"
	"sync/atomic"
)

// traceEnabled is set once at package init. Atomic for safe concurrent access
// (production reads in UI goroutine, test writes via setTraceEnabled).
var traceEnabled atomic.Bool

func init() {
	traceEnabled.Store(os.Getenv("OBSERVER_TRACE") != "")
}

// TraceEnabled reports whether OBSERVER_TRACE is set.
// Inlineable by the Go compiler (simple atomic load).
// Zero cost: single atomic boolean check when false.
func TraceEnabled() bool {
	return traceEnabled.Load()
}

// setTraceEnabled overrides the traceEnabled flag for testing.
// Not exported: test-only helper.
func setTraceEnabled(v bool) {
	traceEnabled.Store(v)
}
