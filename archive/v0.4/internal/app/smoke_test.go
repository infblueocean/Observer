package app

import (
	"runtime"
	"testing"
)

// TestAppPackageCompiles is a basic smoke test that verifies the app package
// compiles and the basic types are defined correctly.
// A full lifecycle test would require mocking many dependencies.
func TestAppPackageCompiles(t *testing.T) {
	// Verify Model type has expected fields
	var m Model

	// Check critical fields exist (compile-time check)
	_ = m.width
	_ = m.height
	_ = m.mode
	_ = m.showSources
	_ = m.showBrainTrust
	_ = m.recentErrors
	_ = m.itemCategories

	// Check viewMode constants exist
	if modeStream != 0 {
		t.Error("modeStream should be 0")
	}
	if modeFilters != 1 {
		t.Error("modeFilters should be 1")
	}
}

// TestBaselineGoroutineCount records baseline goroutine count for reference.
// This is not a pass/fail test, just documentation.
func TestBaselineGoroutineCount(t *testing.T) {
	count := runtime.NumGoroutine()
	t.Logf("Baseline goroutine count (test environment): %d", count)

	// In a test environment, expect relatively few goroutines
	// The actual app will have more due to feeds, work pool, etc.
	if count > 50 {
		t.Logf("Warning: high baseline goroutine count may indicate leak in test setup")
	}
}

// TestRecentErrorType verifies RecentError struct
func TestRecentErrorType(t *testing.T) {
	err := RecentError{
		Err: nil,
	}
	if err.Err != nil {
		t.Error("expected nil error")
	}
}
