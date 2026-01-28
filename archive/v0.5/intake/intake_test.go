//go:build ignore

package intake

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/model"
)

func TestNewPipeline(t *testing.T) {
	// Test pipeline creation with nil dependencies (should not panic)
	p := NewPipeline(nil, nil, nil)
	if p == nil {
		t.Fatal("NewPipeline returned nil")
	}
}

func TestPipelineOptions(t *testing.T) {
	p := NewPipeline(nil, nil, nil,
		WithEmbedTimeout(1*time.Minute),
		WithDedupThreshold(0.9),
		WithEmbedBatchSize(100),
		WithSkipDuplicates(true),
	)

	if p.embedTimeout != 1*time.Minute {
		t.Errorf("embedTimeout = %v, want 1m", p.embedTimeout)
	}
	if p.dedupThreshold != 0.9 {
		t.Errorf("dedupThreshold = %v, want 0.9", p.dedupThreshold)
	}
	if p.embedBatchSize != 100 {
		t.Errorf("embedBatchSize = %d, want 100", p.embedBatchSize)
	}
	if !p.skipDuplicates {
		t.Error("skipDuplicates = false, want true")
	}
}

func TestProcessEmptyItems(t *testing.T) {
	p := NewPipeline(nil, nil, nil)
	result := p.Process(context.Background(), nil)

	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if result.TotalTime == 0 {
		t.Error("TotalTime should be non-zero")
	}
}

func TestProcessWithStoreOnly(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	p := NewPipeline(nil, store, nil)

	items := []model.Item{
		{
			ID:         "test1",
			Source:     model.SourceRSS,
			SourceName: "Test",
			Title:      "Test Article 1",
			Published:  time.Now(),
			Fetched:    time.Now(),
		},
		{
			ID:         "test2",
			Source:     model.SourceRSS,
			SourceName: "Test",
			Title:      "Test Article 2",
			Published:  time.Now(),
			Fetched:    time.Now(),
		},
	}

	result := p.Process(context.Background(), items)

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if result.Stored != 2 {
		t.Errorf("Stored = %d, want 2", result.Stored)
	}
	if result.Unique != 2 {
		t.Errorf("Unique = %d, want 2 (no dedup without embedder)", result.Unique)
	}
	if len(result.Errors) > 0 {
		t.Errorf("Errors = %v", result.Errors)
	}

	// Verify items are in the store
	retrieved, err := store.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("Retrieved %d items, want 2", len(retrieved))
	}
}

func TestProcessContextCancellation(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	p := NewPipeline(nil, store, nil)

	items := []model.Item{
		{
			ID:         "test1",
			Source:     model.SourceRSS,
			SourceName: "Test",
			Title:      "Test Article",
			Published:  time.Now(),
			Fetched:    time.Now(),
		},
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := p.Process(ctx, items)

	// Should still return a result even with cancelled context
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
}

func TestResultTiming(t *testing.T) {
	p := NewPipeline(nil, nil, nil)

	items := []model.Item{
		{ID: "1", Source: model.SourceRSS, SourceName: "Test", Title: "Test", Published: time.Now(), Fetched: time.Now()},
	}

	result := p.Process(context.Background(), items)

	// TotalTime should always be set
	if result.TotalTime == 0 {
		t.Error("TotalTime should be non-zero")
	}
}

func TestPipelineStats(t *testing.T) {
	p := NewPipeline(nil, nil, nil)
	stats := p.Stats()

	if stats.EmbedderAvailable {
		t.Error("EmbedderAvailable should be false with nil embedder")
	}
	if stats.DedupIndexed != 0 {
		t.Errorf("DedupIndexed = %d, want 0", stats.DedupIndexed)
	}
}

// newTestStore creates a temporary store for testing.
func newTestStore(t *testing.T) *model.Store {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "intake-test-*")
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
