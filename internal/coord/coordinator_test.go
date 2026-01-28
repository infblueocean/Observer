package coord

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/store"
)

// mockFetcher implements the fetcher interface for testing.
type mockFetcher struct {
	mu           sync.Mutex
	fetchedSrcs  []fetch.Source
	returnItems  []store.Item
	returnErr    error
	fetchDelay   time.Duration
	fetchCount   atomic.Int32
}

func (m *mockFetcher) Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error) {
	m.fetchCount.Add(1)

	// Simulate delay if configured
	if m.fetchDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.fetchDelay):
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.fetchedSrcs = append(m.fetchedSrcs, src)
	return m.returnItems, m.returnErr
}

func (m *mockFetcher) getFetchedSources() []fetch.Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]fetch.Source, len(m.fetchedSrcs))
	copy(result, m.fetchedSrcs)
	return result
}

func TestCoordinatorFetchesAllSources(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "Source1", URL: "http://example.com/1"},
		{Type: "rss", Name: "Source2", URL: "http://example.com/2"},
		{Type: "rss", Name: "Source3", URL: "http://example.com/3"},
	}

	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Execute - just call fetchAll directly for this test
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify all sources were fetched in order
	fetched := mock.getFetchedSources()
	if len(fetched) != len(sources) {
		t.Errorf("expected %d sources fetched, got %d", len(sources), len(fetched))
	}

	for i, src := range sources {
		if fetched[i].Name != src.Name {
			t.Errorf("source %d: expected %q, got %q", i, src.Name, fetched[i].Name)
		}
	}
}

func TestCoordinatorRespectsContextCancellation(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "Source1", URL: "http://example.com/1"},
		{Type: "rss", Name: "Source2", URL: "http://example.com/2"},
		{Type: "rss", Name: "Source3", URL: "http://example.com/3"},
	}

	// Create a mock that delays to allow cancellation
	mock := &mockFetcher{
		fetchDelay: 100 * time.Millisecond,
	}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Create a context that we'll cancel quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Start fetching in a goroutine
	done := make(chan struct{})
	go func() {
		coord.fetchAll(ctx, nil)
		close(done)
	}()

	// Cancel after the first fetch starts
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for fetchAll to complete
	select {
	case <-done:
		// Success - fetchAll returned
	case <-time.After(2 * time.Second):
		t.Fatal("fetchAll did not respect context cancellation")
	}

	// Verify not all sources were fetched
	fetched := mock.getFetchedSources()
	if len(fetched) >= len(sources) {
		t.Errorf("expected fewer than %d sources fetched due to cancellation, got %d", len(sources), len(fetched))
	}
}

func TestCoordinatorHandlesFetchTimeout(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "SlowSource", URL: "http://example.com/slow"},
	}

	// Create a mock that delays longer than the timeout
	// We'll use a shorter timeout for testing
	mock := &mockFetcher{
		fetchDelay: 5 * time.Second, // Much longer than test timeout context
	}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Create a context with a short timeout to simulate per-fetch timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run fetchAll - it should timeout
	coord.fetchAll(ctx, nil)

	// The fetch should have been attempted but timed out
	count := mock.fetchCount.Load()
	if count != 1 {
		t.Errorf("expected 1 fetch attempt, got %d", count)
	}
}

func TestCoordinatorSavesItems(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "TestSource", URL: "http://example.com/feed"},
	}

	now := time.Now()
	testItems := []store.Item{
		{
			ID:         "item1",
			SourceType: "rss",
			SourceName: "TestSource",
			Title:      "Test Item 1",
			URL:        "http://example.com/1",
			Published:  now,
			Fetched:    now,
		},
		{
			ID:         "item2",
			SourceType: "rss",
			SourceName: "TestSource",
			Title:      "Test Item 2",
			URL:        "http://example.com/2",
			Published:  now,
			Fetched:    now,
		},
	}

	mock := &mockFetcher{
		returnItems: testItems,
	}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify items were saved
	items, err := s.GetItems(100, true)
	if err != nil {
		t.Fatalf("failed to get items: %v", err)
	}

	if len(items) != len(testItems) {
		t.Errorf("expected %d items saved, got %d", len(testItems), len(items))
	}
}

func TestCoordinatorStartAndWait(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "TestSource", URL: "http://example.com/feed"},
	}

	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start coordinator
	coord.Start(ctx, nil)

	// Wait a bit for initial fetch
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait should return quickly
	done := make(chan struct{})
	go func() {
		coord.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}

	// Verify at least one fetch happened (the initial fetch)
	count := mock.fetchCount.Load()
	if count < 1 {
		t.Errorf("expected at least 1 fetch, got %d", count)
	}
}

func TestCoordinatorSourcesImmutable(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "Source1", URL: "http://example.com/1"},
		{Type: "rss", Name: "Source2", URL: "http://example.com/2"},
	}

	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Modify the original slice
	sources[0].Name = "Modified"
	sources = append(sources, fetch.Source{Type: "rss", Name: "Source3", URL: "http://example.com/3"})

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify coordinator used original sources (not modified)
	fetched := mock.getFetchedSources()
	if len(fetched) != 2 {
		t.Errorf("expected 2 sources, got %d", len(fetched))
	}

	if fetched[0].Name != "Source1" {
		t.Errorf("expected first source to be 'Source1', got %q", fetched[0].Name)
	}
}

func TestCoordinatorSendsFetchCompleteMessages(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "GoodSource", URL: "http://example.com/good"},
		{Type: "rss", Name: "BadSource", URL: "http://example.com/bad"},
	}

	testErr := errors.New("fetch error")

	// Create mock that succeeds for first source, fails for second
	callCount := 0
	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			callCount++
			if src.Name == "BadSource" {
				return nil, testErr
			}
			return []store.Item{{
				ID:         "item1",
				SourceType: "rss",
				SourceName: src.Name,
				Title:      "Test",
				URL:        "http://example.com/item",
				Published:  time.Now(),
				Fetched:    time.Now(),
			}}, nil
		},
	}

	coord := NewCoordinatorWithFetcher(s, customFetcher, sources)

	// Execute with nil program (we test that it handles nil gracefully)
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if callCount != 2 {
		t.Errorf("expected 2 fetch calls, got %d", callCount)
	}
}

// customMockFetcher allows custom fetch behavior per call.
type customMockFetcher struct {
	fetchFunc func(ctx context.Context, src fetch.Source) ([]store.Item, error)
}

func (c *customMockFetcher) Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error) {
	return c.fetchFunc(ctx, src)
}

func TestCoordinatorHandlesNilProgram(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "TestSource", URL: "http://example.com/feed"},
	}

	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, sources)

	// Execute with nil program - should not panic
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify fetch was still attempted
	count := mock.fetchCount.Load()
	if count != 1 {
		t.Errorf("expected 1 fetch, got %d", count)
	}
}

func TestCoordinatorHandlesFetchError(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	sources := []fetch.Source{
		{Type: "rss", Name: "ErrorSource", URL: "http://example.com/error"},
		{Type: "rss", Name: "GoodSource", URL: "http://example.com/good"},
	}

	// Create mock that fails for first source
	testErr := errors.New("fetch failed")
	callIdx := 0
	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			callIdx++
			if callIdx == 1 {
				return nil, testErr
			}
			return []store.Item{{
				ID:         "item1",
				SourceType: "rss",
				SourceName: src.Name,
				Title:      "Test",
				URL:        "http://example.com/item",
				Published:  time.Now(),
				Fetched:    time.Now(),
			}}, nil
		},
	}

	coord := NewCoordinatorWithFetcher(s, customFetcher, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify both sources were attempted (error doesn't stop iteration)
	if callIdx != 2 {
		t.Errorf("expected 2 fetch calls despite error, got %d", callIdx)
	}

	// Verify only second source's items were saved
	items, err := s.GetItems(100, true)
	if err != nil {
		t.Fatalf("failed to get items: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item saved (from good source), got %d", len(items))
	}
}
