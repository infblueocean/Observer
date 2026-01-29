package coord

import (
	"context"
	"errors"
	"fmt"
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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

	// Execute - just call fetchAll directly for this test
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify all sources were fetched (order not guaranteed with parallel fetch)
	fetched := mock.getFetchedSources()
	if len(fetched) != len(sources) {
		t.Errorf("expected %d sources fetched, got %d", len(sources), len(fetched))
	}

	// Build set of expected source names
	expected := make(map[string]bool)
	for _, src := range sources {
		expected[src.Name] = true
	}

	// Verify all expected sources were fetched
	for _, src := range fetched {
		if !expected[src.Name] {
			t.Errorf("unexpected source fetched: %q", src.Name)
		}
		delete(expected, src.Name)
	}
	for name := range expected {
		t.Errorf("source not fetched: %q", name)
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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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

	// Check that Source1 was fetched (not "Modified")
	foundSource1 := false
	for _, src := range fetched {
		if src.Name == "Source1" {
			foundSource1 = true
		}
		if src.Name == "Modified" {
			t.Error("coordinator used modified source name")
		}
		if src.Name == "Source3" {
			t.Error("coordinator used appended source")
		}
	}
	if !foundSource1 {
		t.Error("Source1 was not fetched")
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

	// Create mock that succeeds for GoodSource, fails for BadSource
	var callCount atomic.Int32
	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			callCount.Add(1)
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

	coord := NewCoordinatorWithFetcher(s, customFetcher, nil, sources)

	// Execute with nil program (we test that it handles nil gracefully)
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if callCount.Load() != 2 {
		t.Errorf("expected 2 fetch calls, got %d", callCount.Load())
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
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

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

	// Create mock that fails for ErrorSource, succeeds for GoodSource
	testErr := errors.New("fetch failed")
	var callCount atomic.Int32
	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			callCount.Add(1)
			if src.Name == "ErrorSource" {
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

	coord := NewCoordinatorWithFetcher(s, customFetcher, nil, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify both sources were attempted (error doesn't stop other fetches)
	if callCount.Load() != 2 {
		t.Errorf("expected 2 fetch calls despite error, got %d", callCount.Load())
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

// mockEmbedder implements embed.Embedder for testing.
type mockEmbedder struct {
	available bool
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockEmbedder) Available() bool {
	return m.available
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func TestCoordinatorEmbedsAfterFetch(t *testing.T) {
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
	}

	mock := &mockFetcher{returnItems: testItems}
	embedCount := 0
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify embedding was called
	if embedCount != 1 {
		t.Errorf("expected 1 embed call, got %d", embedCount)
	}

	// Verify embedding was saved with correct content
	emb, err := s.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("failed to get embedding: %v", err)
	}
	if emb == nil {
		t.Fatal("expected embedding to be saved, got nil")
	}

	// Verify embedding values match what the mock returned
	expectedEmb := []float32{0.1, 0.2, 0.3}
	if len(emb) != len(expectedEmb) {
		t.Fatalf("embedding length mismatch: got %d, want %d", len(emb), len(expectedEmb))
	}
	for i, v := range expectedEmb {
		if emb[i] != v {
			t.Errorf("embedding[%d] = %f, want %f", i, emb[i], v)
		}
	}
}

func TestCoordinatorSkipsEmbeddingWhenUnavailable(t *testing.T) {
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
	}

	mock := &mockFetcher{returnItems: testItems}
	embedCount := 0
	embedder := &mockEmbedder{
		available: false, // Embedder not available
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify embedding was NOT called
	if embedCount != 0 {
		t.Errorf("expected 0 embed calls when unavailable, got %d", embedCount)
	}
}

func TestCoordinatorRespectsCancellationDuringEmbedding(t *testing.T) {
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
	// Create multiple items to embed
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	embedCount := 0
	ctx, cancel := context.WithCancel(context.Background())
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(embCtx context.Context, text string) ([]float32, error) {
			embedCount++
			if embedCount == 1 {
				cancel() // Cancel after first embed
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Execute
	coord.fetchAll(ctx, nil)

	// Verify embedding stopped early (should be less than 3)
	if embedCount >= 3 {
		t.Errorf("expected fewer than 3 embed calls due to cancellation, got %d", embedCount)
	}
}

func TestCoordinatorHandlesEmbedErrors(t *testing.T) {
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
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	embedCount := 0
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			if embedCount == 1 {
				return nil, errors.New("embed failed")
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify both items were attempted despite error
	if embedCount != 2 {
		t.Errorf("expected 2 embed attempts despite error, got %d", embedCount)
	}

	// Verify first item was NOT embedded (error occurred)
	emb1, _ := s.GetEmbedding("item1")
	if emb1 != nil {
		t.Error("expected item1 embedding NOT to be saved due to error")
	}

	// Verify second item was embedded (succeeded)
	emb2, _ := s.GetEmbedding("item2")
	if emb2 == nil {
		t.Error("expected item2 embedding to be saved")
	}
}

func TestCoordinatorSkipsEmbeddingWhenNilEmbedder(t *testing.T) {
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
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	// Explicitly pass nil embedder
	coord := NewCoordinatorWithFetcher(s, mock, nil, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify items were fetched
	items, _ := s.GetItems(100, true)
	if len(items) != 1 {
		t.Errorf("expected 1 item saved, got %d", len(items))
	}

	// Verify no embedding was saved (since embedder is nil)
	emb, _ := s.GetEmbedding("item1")
	if emb != nil {
		t.Error("expected no embedding when embedder is nil")
	}
}

func TestCoordinatorStopsWhenOllamaDisappears(t *testing.T) {
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
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	embedCount := 0
	available := true

	// Use dynamic availability mock - Available() returns false after first embed
	coord := NewCoordinatorWithFetcher(s, mock, &mockEmbedderWithDynamicAvailable{
		available: &available,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			if embedCount == 1 {
				available = false // Ollama disappears after first embed
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}, sources)

	// Execute
	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify embedding stopped early (should be 1, since Available() returns false after first)
	if embedCount >= 3 {
		t.Errorf("expected fewer than 3 embed calls when Ollama disappears, got %d", embedCount)
	}
}

// mockEmbedderWithDynamicAvailable allows changing availability during test.
type mockEmbedderWithDynamicAvailable struct {
	available *bool
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockEmbedderWithDynamicAvailable) Available() bool {
	return *m.available
}

func (m *mockEmbedderWithDynamicAvailable) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func TestCoordinatorFetchesInParallel(t *testing.T) {
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

	// Use channels to prove parallelism:
	// Each fetch signals it started, then waits for permission to continue.
	// We'll wait until all 3 have started before releasing them.
	started := make(chan struct{}, 3)
	proceed := make(chan struct{})

	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			started <- struct{}{} // Signal that this fetch started
			<-proceed             // Wait for permission to continue
			return []store.Item{}, nil
		},
	}

	coord := NewCoordinatorWithFetcher(s, customFetcher, nil, sources)

	// Run fetchAll in a goroutine
	done := make(chan struct{})
	go func() {
		coord.fetchAll(context.Background(), nil)
		close(done)
	}()

	// Wait for all 3 fetches to start (proves they're running in parallel)
	for i := 0; i < 3; i++ {
		select {
		case <-started:
			// Good, another fetch started
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for fetch %d to start - not running in parallel", i+1)
		}
	}

	// All 3 started concurrently - release them
	close(proceed)

	// Wait for fetchAll to complete
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fetchAll to complete")
	}
}

func TestCoordinatorParallelRespectsLimit(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Create more sources than the concurrency limit (5)
	sources := make([]fetch.Source, 10)
	for i := 0; i < 10; i++ {
		sources[i] = fetch.Source{Type: "rss", Name: fmt.Sprintf("Source%d", i), URL: fmt.Sprintf("http://example.com/%d", i)}
	}

	// Track max concurrent fetches
	var current atomic.Int32
	var maxConcurrent atomic.Int32
	proceed := make(chan struct{})

	customFetcher := &customMockFetcher{
		fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
			n := current.Add(1)
			// Update max if this is a new high
			for {
				old := maxConcurrent.Load()
				if n <= old || maxConcurrent.CompareAndSwap(old, n) {
					break
				}
			}
			<-proceed // Wait for signal
			current.Add(-1)
			return []store.Item{}, nil
		},
	}

	coord := NewCoordinatorWithFetcher(s, customFetcher, nil, sources)

	// Run fetchAll in a goroutine
	done := make(chan struct{})
	go func() {
		coord.fetchAll(context.Background(), nil)
		close(done)
	}()

	// Wait a bit for goroutines to pile up at the limit
	time.Sleep(100 * time.Millisecond)

	// Release all fetches
	close(proceed)

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fetchAll to complete")
	}

	// Verify max concurrent was at most 5 (the limit) but at least 2 (parallelism happened)
	max := maxConcurrent.Load()
	if max > 5 {
		t.Errorf("max concurrent fetches was %d, expected at most 5", max)
	}
	if max < 2 {
		t.Errorf("max concurrent fetches was %d, expected at least 2 to prove parallelism", max)
	}
}

func TestCoordinatorEmbeddingWorkerProcessesItems(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Pre-populate store with items that need embedding
	now := time.Now()
	items := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}
	_, err = s.SaveItems(items)
	if err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	// Create embedder that tracks calls
	var embedCount atomic.Int32
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount.Add(1)
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}

	sources := []fetch.Source{}
	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Start embedding worker with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	// Wait for embeddings to be processed (worker runs every 2 seconds)
	// We wait up to 5 seconds for all 3 items to be embedded
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if embedCount.Load() >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Cancel and wait for worker to stop
	cancel()
	coord.Wait()

	// Verify all items were embedded
	count := embedCount.Load()
	if count < 3 {
		t.Errorf("expected at least 3 embed calls, got %d", count)
	}

	// Verify embeddings were saved
	for _, item := range items {
		emb, err := s.GetEmbedding(item.ID)
		if err != nil {
			t.Errorf("failed to get embedding for %s: %v", item.ID, err)
		}
		if emb == nil {
			t.Errorf("expected embedding for %s to be saved", item.ID)
		}
	}
}

func TestCoordinatorEmbeddingWorkerRespectsContext(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Pre-populate store with items that need embedding
	now := time.Now()
	items := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
	}
	_, err = s.SaveItems(items)
	if err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	// Create embedder that blocks until context is cancelled
	embedStarted := make(chan struct{})
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			close(embedStarted)
			<-ctx.Done() // Block until cancelled
			return nil, ctx.Err()
		},
	}

	sources := []fetch.Source{}
	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Start embedding worker
	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	// Wait for worker to complete with a timeout
	done := make(chan struct{})
	go func() {
		coord.Wait()
		close(done)
	}()

	// Cancel the context
	cancel()

	// Verify Wait returns quickly after cancellation
	select {
	case <-done:
		// Success - worker stopped
	case <-time.After(3 * time.Second):
		t.Fatal("embedding worker did not stop after context cancellation")
	}
}

// mockBatchEmbedder implements embed.BatchEmbedder for testing.
type mockBatchEmbedder struct {
	available      bool
	embedFunc      func(ctx context.Context, text string) ([]float32, error)
	embedBatchFunc func(ctx context.Context, texts []string) ([][]float32, error)
}

func (m *mockBatchEmbedder) Available() bool {
	return m.available
}

func (m *mockBatchEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockBatchEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedBatchFunc != nil {
		return m.embedBatchFunc(ctx, texts)
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{0.1, 0.2, 0.3}
	}
	return result, nil
}

func TestCoordinatorUsesBatchEmbedder(t *testing.T) {
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
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	var batchCalled bool
	var batchTexts []string
	embedder := &mockBatchEmbedder{
		available: true,
		embedBatchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			batchCalled = true
			batchTexts = texts
			result := make([][]float32, len(texts))
			for i := range texts {
				result[i] = []float32{0.1, 0.2, 0.3}
			}
			return result, nil
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if !batchCalled {
		t.Error("expected EmbedBatch to be called for BatchEmbedder")
	}
	if len(batchTexts) != 3 {
		t.Errorf("expected 3 texts in batch, got %d", len(batchTexts))
	}

	// Verify all embeddings were saved
	for _, item := range testItems {
		emb, err := s.GetEmbedding(item.ID)
		if err != nil {
			t.Errorf("failed to get embedding for %s: %v", item.ID, err)
		}
		if emb == nil {
			t.Errorf("expected embedding for %s", item.ID)
		}
	}
}

func TestCoordinatorBatchEmbedError(t *testing.T) {
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
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}

	mock := &mockFetcher{returnItems: testItems}
	embedder := &mockBatchEmbedder{
		available: true,
		embedBatchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			return nil, errors.New("batch embed failed")
		},
	}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	// Verify no embeddings were saved
	for _, item := range testItems {
		emb, _ := s.GetEmbedding(item.ID)
		if emb != nil {
			t.Errorf("expected no embedding for %s after batch error", item.ID)
		}
	}
}

func TestCoordinatorEmbeddingWorkerSkipsWhenUnavailable(t *testing.T) {
	// Setup
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Pre-populate store with items that need embedding
	now := time.Now()
	items := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}
	_, err = s.SaveItems(items)
	if err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	// Create embedder that is not available
	var embedCount atomic.Int32
	embedder := &mockEmbedder{
		available: false, // Not available
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount.Add(1)
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}

	sources := []fetch.Source{}
	mock := &mockFetcher{}
	coord := NewCoordinatorWithFetcher(s, mock, embedder, sources)

	// Start embedding worker
	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	// Wait for at least one worker cycle (worker runs every 2 seconds)
	time.Sleep(3 * time.Second)

	// Cancel and wait
	cancel()
	coord.Wait()

	// Verify embed was NOT called since embedder is unavailable
	count := embedCount.Load()
	if count != 0 {
		t.Errorf("expected 0 embed calls when unavailable, got %d", count)
	}

	// Verify no embeddings were saved
	for _, item := range items {
		emb, _ := s.GetEmbedding(item.ID)
		if emb != nil {
			t.Errorf("expected no embedding for %s when embedder unavailable", item.ID)
		}
	}
}
