//go:build ignore

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

func TestPipelineEmpty(t *testing.T) {
	pipeline := NewPipeline()

	items := []model.Item{{ID: "1"}, {ID: "2"}}
	result, err := pipeline.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("empty pipeline should not error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("empty pipeline should pass through all items, got %d", len(result))
	}
}

func TestPipelineChaining(t *testing.T) {
	// Filter that keeps items with even-length IDs
	evenFilter := NewSyncFilter("even", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		var result []model.Item
		for _, item := range items {
			if len(item.ID)%2 == 0 {
				result = append(result, item)
			}
		}
		return result, nil
	})

	// Filter that keeps items with title
	hasTitle := NewSyncFilter("has-title", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		var result []model.Item
		for _, item := range items {
			if item.Title != "" {
				result = append(result, item)
			}
		}
		return result, nil
	})

	pipeline := NewPipeline().Add(evenFilter).Add(hasTitle)

	items := []model.Item{
		{ID: "a", Title: ""},         // odd ID, no title -> filtered by even
		{ID: "ab", Title: ""},        // even ID, no title -> filtered by hasTitle
		{ID: "abc", Title: "Title"},  // odd ID, has title -> filtered by even
		{ID: "abcd", Title: "Title"}, // even ID, has title -> passes both
	}

	result, err := pipeline.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("pipeline.Run failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item to pass both filters, got %d", len(result))
	}
	if len(result) > 0 && result[0].ID != "abcd" {
		t.Errorf("expected item 'abcd', got %s", result[0].ID)
	}
}

func TestPipelineFilterError(t *testing.T) {
	expectedErr := errors.New("filter failed")

	failFilter := NewSyncFilter("fail", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		return nil, expectedErr
	})

	pipeline := NewPipeline().Add(failFilter)

	_, err := pipeline.Run(context.Background(), []model.Item{{ID: "1"}}, nil)
	if err == nil {
		t.Error("expected error from failing filter")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

func TestPipelineContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Slow filter that would block
	slowFilter := NewSyncFilter("slow", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		time.Sleep(1 * time.Second)
		return items, nil
	})

	pipeline := NewPipeline().Add(slowFilter)

	_, err := pipeline.Run(ctx, []model.Item{{ID: "1"}}, nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPipelineContextCancelledBetweenFilters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// First filter cancels the context
	cancellingFilter := NewSyncFilter("canceller", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		cancel()
		return items, nil
	})

	// Second filter should not run
	ran := false
	secondFilter := NewSyncFilter("second", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		ran = true
		return items, nil
	})

	pipeline := NewPipeline().Add(cancellingFilter).Add(secondFilter)

	_, err := pipeline.Run(ctx, []model.Item{{ID: "1"}}, nil)
	if err == nil {
		t.Error("expected error after context cancellation")
	}
	if ran {
		t.Error("second filter should not have run after context was cancelled")
	}
}

func TestPipelineLen(t *testing.T) {
	pipeline := NewPipeline()
	if pipeline.Len() != 0 {
		t.Errorf("expected empty pipeline to have len 0, got %d", pipeline.Len())
	}

	filter := NewSyncFilter("test", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		return items, nil
	})
	pipeline.Add(filter).Add(filter).Add(filter)

	if pipeline.Len() != 3 {
		t.Errorf("expected pipeline with 3 filters to have len 3, got %d", pipeline.Len())
	}
}

func TestSyncFilterName(t *testing.T) {
	filter := NewSyncFilter("my-filter", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		return items, nil
	})

	if filter.Name() != "my-filter" {
		t.Errorf("expected name 'my-filter', got %s", filter.Name())
	}
}

func TestSyncFilterContextCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	filter := NewSyncFilter("test", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		return items, nil
	})

	_, err := filter.Run(ctx, []model.Item{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSyncFilterWithWorkPool(t *testing.T) {
	// SyncFilter should work even when pool is provided (but not use it)
	pool := work.NewPool(2)
	pool.Start(context.Background())
	defer pool.Stop()

	filter := NewSyncFilter("test", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
		return items, nil
	})

	result, err := filter.Run(context.Background(), []model.Item{{ID: "1"}}, pool)
	if err != nil {
		t.Fatalf("filter with pool failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}
