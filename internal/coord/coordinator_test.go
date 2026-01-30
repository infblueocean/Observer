package coord

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/store"
)

// mockProvider implements the Provider interface for testing.
type mockProvider struct {
	items []store.Item
	err   error
	count atomic.Int32
	delay time.Duration
}

func (m *mockProvider) Fetch(ctx context.Context) ([]store.Item, error) {
	m.count.Add(1)
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return m.items, m.err
}

func TestCoordinatorRespectsContextCancellation(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	mock := &mockProvider{delay: 100 * time.Millisecond}
	coord := NewCoordinator(s, mock, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.fetchAll(ctx, nil)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("fetchAll did not respect context cancellation")
	}
}

func TestCoordinatorHandlesFetchTimeout(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	mock := &mockProvider{delay: 5 * time.Second}
	coord := NewCoordinator(s, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	coord.fetchAll(ctx, nil)

	count := mock.count.Load()
	if count != 1 {
		t.Errorf("expected 1 fetch attempt, got %d", count)
	}
}

func TestCoordinatorSavesItems(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

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

	mock := &mockProvider{items: testItems}
	coord := NewCoordinator(s, mock, nil)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	items, err := s.GetItems(100, true)
	if err != nil {
		t.Fatalf("failed to get items: %v", err)
	}

	if len(items) != len(testItems) {
		t.Errorf("expected %d items saved, got %d", len(testItems), len(items))
	}
}

func TestCoordinatorStartAndWait(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	mock := &mockProvider{}
	coord := NewCoordinator(s, mock, nil)

	ctx, cancel := context.WithCancel(context.Background())

	coord.Start(ctx, nil)

	time.Sleep(50 * time.Millisecond)
	cancel()

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

	count := mock.count.Load()
	if count < 1 {
		t.Errorf("expected at least 1 fetch, got %d", count)
	}
}

func TestCoordinatorHandlesNilProgram(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	mock := &mockProvider{}
	coord := NewCoordinator(s, mock, nil)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	count := mock.count.Load()
	if count != 1 {
		t.Errorf("expected 1 fetch, got %d", count)
	}
}

func TestCoordinatorHandlesFetchError(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	mock := &mockProvider{err: errors.New("fetch failed")}
	coord := NewCoordinator(s, mock, nil)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	items, err := s.GetItems(100, true)
	if err != nil {
		t.Fatalf("failed to get items: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("expected 0 items when fetch fails, got %d", len(items))
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
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

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

	mock := &mockProvider{items: testItems}
	embedCount := 0
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinator(s, mock, embedder)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if embedCount != 1 {
		t.Errorf("expected 1 embed call, got %d", embedCount)
	}

	emb, err := s.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("failed to get embedding: %v", err)
	}
	if emb == nil {
		t.Fatal("expected embedding to be saved, got nil")
	}

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
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

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

	mock := &mockProvider{items: testItems}
	embedCount := 0
	embedder := &mockEmbedder{
		available: false,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinator(s, mock, embedder)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if embedCount != 0 {
		t.Errorf("expected 0 embed calls when unavailable, got %d", embedCount)
	}
}

func TestCoordinatorRespectsCancellationDuringEmbedding(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
	embedCount := 0
	ctx, cancel := context.WithCancel(context.Background())
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(embCtx context.Context, text string) ([]float32, error) {
			embedCount++
			if embedCount == 1 {
				cancel()
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	coord := NewCoordinator(s, mock, embedder)

	coord.fetchAll(ctx, nil)

	if embedCount >= 3 {
		t.Errorf("expected fewer than 3 embed calls due to cancellation, got %d", embedCount)
	}
}

func TestCoordinatorHandlesEmbedErrors(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
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
	coord := NewCoordinator(s, mock, embedder)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if embedCount != 2 {
		t.Errorf("expected 2 embed attempts despite error, got %d", embedCount)
	}

	emb1, _ := s.GetEmbedding("item1")
	if emb1 != nil {
		t.Error("expected item1 embedding NOT to be saved due to error")
	}

	emb2, _ := s.GetEmbedding("item2")
	if emb2 == nil {
		t.Error("expected item2 embedding to be saved")
	}
}

func TestCoordinatorSkipsEmbeddingWhenNilEmbedder(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
	coord := NewCoordinator(s, mock, nil)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	items, _ := s.GetItems(100, true)
	if len(items) != 1 {
		t.Errorf("expected 1 item saved, got %d", len(items))
	}

	emb, _ := s.GetEmbedding("item1")
	if emb != nil {
		t.Error("expected no embedding when embedder is nil")
	}
}

func TestCoordinatorStopsWhenOllamaDisappears(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
	embedCount := 0
	available := true

	coord := NewCoordinator(s, mock, &mockEmbedderWithDynamicAvailable{
		available: &available,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount++
			if embedCount == 1 {
				available = false
			}
			return []float32{0.1, 0.2, 0.3}, nil
		},
	})

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

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

func TestCoordinatorEmbeddingWorkerProcessesItems(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

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

	var embedCount atomic.Int32
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount.Add(1)
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}

	mock := &mockProvider{}
	coord := NewCoordinator(s, mock, embedder)

	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if embedCount.Load() >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	coord.Wait()

	count := embedCount.Load()
	if count < 3 {
		t.Errorf("expected at least 3 embed calls, got %d", count)
	}

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
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	items := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
	}
	_, err = s.SaveItems(items)
	if err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	embedStarted := make(chan struct{})
	embedder := &mockEmbedder{
		available: true,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			close(embedStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	mock := &mockProvider{}
	coord := NewCoordinator(s, mock, embedder)

	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	done := make(chan struct{})
	go func() {
		coord.Wait()
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Success
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

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
		{ID: "item3", SourceType: "rss", SourceName: "TestSource", Title: "Test 3", URL: "http://example.com/3", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
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
	coord := NewCoordinator(s, mock, embedder)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	if !batchCalled {
		t.Error("expected EmbedBatch to be called for BatchEmbedder")
	}
	if len(batchTexts) != 3 {
		t.Errorf("expected 3 texts in batch, got %d", len(batchTexts))
	}

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

	now := time.Now()
	testItems := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}

	mock := &mockProvider{items: testItems}
	embedder := &mockBatchEmbedder{
		available: true,
		embedBatchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			return nil, errors.New("batch embed failed")
		},
	}
	coord := NewCoordinator(s, mock, embedder)

	ctx := context.Background()
	coord.fetchAll(ctx, nil)

	for _, item := range testItems {
		emb, _ := s.GetEmbedding(item.ID)
		if emb != nil {
			t.Errorf("expected no embedding for %s after batch error", item.ID)
		}
	}
}

func TestCoordinatorEmbeddingWorkerSkipsWhenUnavailable(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	items := []store.Item{
		{ID: "item1", SourceType: "rss", SourceName: "TestSource", Title: "Test 1", URL: "http://example.com/1", Published: now, Fetched: now},
		{ID: "item2", SourceType: "rss", SourceName: "TestSource", Title: "Test 2", URL: "http://example.com/2", Published: now, Fetched: now},
	}
	_, err = s.SaveItems(items)
	if err != nil {
		t.Fatalf("failed to save items: %v", err)
	}

	var embedCount atomic.Int32
	embedder := &mockEmbedder{
		available: false,
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			embedCount.Add(1)
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}

	mock := &mockProvider{}
	coord := NewCoordinator(s, mock, embedder)

	ctx, cancel := context.WithCancel(context.Background())
	coord.StartEmbeddingWorker(ctx)

	time.Sleep(3 * time.Second)

	cancel()
	coord.Wait()

	count := embedCount.Load()
	if count != 0 {
		t.Errorf("expected 0 embed calls when unavailable, got %d", count)
	}

	for _, item := range items {
		emb, _ := s.GetEmbedding(item.ID)
		if emb != nil {
			t.Errorf("expected no embedding for %s when embedder unavailable", item.ID)
		}
	}
}
