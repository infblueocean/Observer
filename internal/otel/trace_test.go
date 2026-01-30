package otel

import "testing"

func TestTraceEnabledToggle(t *testing.T) {
	orig := TraceEnabled()
	defer setTraceEnabled(orig)

	setTraceEnabled(true)
	if !TraceEnabled() {
		t.Error("TraceEnabled() should be true after setTraceEnabled(true)")
	}

	setTraceEnabled(false)
	if TraceEnabled() {
		t.Error("TraceEnabled() should be false after setTraceEnabled(false)")
	}
}
