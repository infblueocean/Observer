package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/controller"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

func TestMainFeedControllerDefaults(t *testing.T) {
	cfg := DefaultMainFeedConfig()

	if cfg.MaxAge != 6*time.Hour {
		t.Errorf("expected default MaxAge of 6h, got %v", cfg.MaxAge)
	}
	if cfg.MaxPerSource != 10 {
		t.Errorf("expected default MaxPerSource of 10, got %d", cfg.MaxPerSource)
	}
}

func TestMainFeedControllerValidation(t *testing.T) {
	// Negative values should be corrected
	cfg := MainFeedConfig{
		MaxAge:       -1 * time.Hour,
		MaxPerSource: -5,
	}

	ctrl := NewMainFeedController(cfg)

	// Can't directly check private fields, but we can verify it doesn't panic
	// and behaves correctly
	if ctrl.ID() != "main-feed" {
		t.Errorf("expected ID 'main-feed', got %s", ctrl.ID())
	}
}

func TestMainFeedControllerRefresh(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	pool := work.NewPool(2)
	pool.Start(context.Background())
	defer pool.Stop()

	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	// Add some items to the store
	items := []model.Item{
		{ID: "1", Source: model.SourceRSS, SourceName: "Test", Title: "Article 1", Published: time.Now().Add(-1 * time.Hour), Fetched: time.Now()},
		{ID: "2", Source: model.SourceRSS, SourceName: "Test", Title: "Article 2", Published: time.Now().Add(-2 * time.Hour), Fetched: time.Now()},
	}
	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	// Subscribe and trigger refresh
	events := ctrl.Subscribe()
	ctrl.Refresh(context.Background(), store, pool)

	// Should receive started event
	select {
	case event := <-events:
		if event.Type != controller.EventStarted {
			t.Errorf("expected EventStarted, got %v", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for started event")
	}

	// Should receive completed event with items
	select {
	case event := <-events:
		if event.Type != controller.EventCompleted {
			t.Errorf("expected EventCompleted, got %v", event.Type)
		}
		if len(event.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(event.Items))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for completed event")
	}
}

func TestMainFeedControllerRefreshEmpty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	pool := work.NewPool(2)
	pool.Start(context.Background())
	defer pool.Stop()

	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	events := ctrl.Subscribe()
	ctrl.Refresh(context.Background(), store, pool)

	// Should receive started
	<-events

	// Should receive completed with empty items
	select {
	case event := <-events:
		if event.Type != controller.EventCompleted {
			t.Errorf("expected EventCompleted, got %v", event.Type)
		}
		if len(event.Items) != 0 {
			t.Errorf("expected 0 items, got %d", len(event.Items))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for completed event")
	}
}

func TestMainFeedControllerContextCancellation(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	pool := work.NewPool(2)
	pool.Start(context.Background())
	defer pool.Stop()

	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	events := ctrl.Subscribe()
	ctrl.Refresh(ctx, store, pool)

	// Should receive error event
	select {
	case event := <-events:
		if event.Type != controller.EventError {
			t.Errorf("expected EventError for cancelled context, got %v", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for error event")
	}
}

func TestMainFeedControllerSetMaxAge(t *testing.T) {
	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	// Should accept valid value
	ctrl.SetMaxAge(12 * time.Hour)
	// No direct way to verify, but it shouldn't panic

	// Should ignore invalid value
	ctrl.SetMaxAge(-1 * time.Hour)
	ctrl.SetMaxAge(0)
	// Should not panic
}

func TestMainFeedControllerSetMaxPerSource(t *testing.T) {
	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	// Should accept valid value
	ctrl.SetMaxPerSource(20)

	// Should ignore invalid values
	ctrl.SetMaxPerSource(0)
	ctrl.SetMaxPerSource(-5)
	// Should not panic
}

func TestMainFeedControllerConcurrentRefresh(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	pool := work.NewPool(2)
	pool.Start(context.Background())
	defer pool.Stop()

	ctrl := NewMainFeedController(DefaultMainFeedConfig())

	// Add items
	items := []model.Item{
		{ID: "1", Source: model.SourceRSS, SourceName: "Test", Title: "A", Published: time.Now(), Fetched: time.Now()},
	}
	store.SaveItems(items)

	// Concurrent refreshes should not race
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			ctrl.Refresh(context.Background(), store, pool)
			done <- struct{}{}
		}()
	}

	// Wait for all to complete
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent refreshes")
		}
	}
}

// newTestStore creates a temporary store for testing.
func newTestStore(t *testing.T) *model.Store {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "observer-ctrl-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := model.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	return store
}
