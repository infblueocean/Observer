package ranking

import (
	"context"

	"github.com/abelbrown/observer/internal/feeds"
)

// Work represents a ranking task that can be processed asynchronously.
// This allows ranking to participate in a unified work dispatch system.
type Work struct {
	// ID uniquely identifies this work item
	ID string

	// Type of ranking work
	Type WorkType

	// Items to be ranked
	Items []feeds.Item

	// Ranker to use (if nil, use default)
	Ranker Ranker

	// Context for ranking
	Context *Context

	// Limit is max items to return (0 = all)
	Limit int

	// ResultChan receives the ranked results
	ResultChan chan<- WorkResult
}

// WorkType categorizes ranking work
type WorkType int

const (
	// WorkTypeRank scores and sorts items
	WorkTypeRank WorkType = iota

	// WorkTypeTopN returns top N items only
	WorkTypeTopN

	// WorkTypeAllocate distributes items across bands
	WorkTypeAllocate
)

// WorkResult is the output of a ranking work item
type WorkResult struct {
	// WorkID matches the input Work.ID
	WorkID string

	// Items in ranked order
	Items []feeds.Item

	// Scores for each item (same order as Items)
	Scores []float64

	// Error if ranking failed
	Error error
}

// Worker processes ranking work items
type Worker struct {
	workChan   chan Work
	ctx        context.Context
	cancel     context.CancelFunc
	defaultCtx *Context
}

// NewWorker creates a ranking worker
func NewWorker(bufferSize int) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		workChan:   make(chan Work, bufferSize),
		ctx:        ctx,
		cancel:     cancel,
		defaultCtx: NewContext(),
	}
}

// Start begins processing work items
func (w *Worker) Start() {
	go w.processLoop()
}

// Stop shuts down the worker
func (w *Worker) Stop() {
	w.cancel()
	close(w.workChan)
}

// Submit adds work to the queue
func (w *Worker) Submit(work Work) {
	select {
	case w.workChan <- work:
	case <-w.ctx.Done():
	}
}

// WorkChan returns the work channel for external dispatch integration
func (w *Worker) WorkChan() chan<- Work {
	return w.workChan
}

func (w *Worker) processLoop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case work, ok := <-w.workChan:
			if !ok {
				return
			}
			w.process(work)
		}
	}
}

func (w *Worker) process(work Work) {
	ctx := work.Context
	if ctx == nil {
		ctx = w.defaultCtx
	}

	ranker := work.Ranker
	if ranker == nil {
		ranker = DefaultRanker()
	}

	var result WorkResult
	result.WorkID = work.ID

	switch work.Type {
	case WorkTypeTopN:
		if work.Limit <= 0 {
			work.Limit = 10
		}
		result.Items = TopN(work.Items, work.Limit, ranker, ctx)

	case WorkTypeAllocate:
		// Rank within each time band
		result.Items = w.allocateRanked(work.Items, ranker, ctx)

	default: // WorkTypeRank
		ranked := Rank(work.Items, ranker, ctx)
		result.Items = make([]feeds.Item, len(ranked))
		result.Scores = make([]float64, len(ranked))
		for i, r := range ranked {
			result.Items[i] = r.Item
			result.Scores[i] = r.Score
		}
	}

	// Send result if channel provided
	if work.ResultChan != nil {
		select {
		case work.ResultChan <- result:
		case <-w.ctx.Done():
		}
	}
}

// allocateRanked ranks items within each time band and picks top N
func (w *Worker) allocateRanked(items []feeds.Item, ranker Ranker, ctx *Context) []feeds.Item {
	// Group by time band
	type timeBand int
	const (
		bandJustNow timeBand = iota
		bandPastHour
		bandToday
		bandYesterday
		bandOlder
	)

	getBand := func(item feeds.Item) timeBand {
		age := ctx.Now.Sub(item.Published)
		switch {
		case age < 15*60*1e9: // 15 min
			return bandJustNow
		case age < 3600*1e9: // 1 hour
			return bandPastHour
		case age < 24*3600*1e9: // 24 hours
			return bandToday
		case age < 48*3600*1e9: // 48 hours
			return bandYesterday
		default:
			return bandOlder
		}
	}

	slots := map[timeBand]int{
		bandJustNow:   10,
		bandPastHour:  20,
		bandToday:     15,
		bandYesterday: 10,
		bandOlder:     5,
	}

	byBand := make(map[timeBand][]feeds.Item)
	for _, item := range items {
		band := getBand(item)
		byBand[band] = append(byBand[band], item)
	}

	// Rank within each band and take top N
	var result []feeds.Item
	bands := []timeBand{bandJustNow, bandPastHour, bandToday, bandYesterday, bandOlder}
	for _, band := range bands {
		bandItems := byBand[band]
		limit := slots[band]
		if len(bandItems) > 0 {
			top := TopN(bandItems, limit, ranker, ctx)
			result = append(result, top...)
		}
	}

	return result
}

// Producer is a helper for components that need to submit ranking work
type Producer struct {
	workChan chan<- Work
	idPrefix string
	counter  int
}

// NewProducer creates a work producer
func NewProducer(workChan chan<- Work, idPrefix string) *Producer {
	return &Producer{
		workChan: workChan,
		idPrefix: idPrefix,
	}
}

// RankAsync submits items for ranking and returns immediately
func (p *Producer) RankAsync(items []feeds.Item, ranker Ranker, ctx *Context) <-chan WorkResult {
	p.counter++
	resultChan := make(chan WorkResult, 1)

	p.workChan <- Work{
		ID:         p.nextID(),
		Type:       WorkTypeRank,
		Items:      items,
		Ranker:     ranker,
		Context:    ctx,
		ResultChan: resultChan,
	}

	return resultChan
}

// TopNAsync submits items for top-N ranking
func (p *Producer) TopNAsync(items []feeds.Item, n int, ranker Ranker, ctx *Context) <-chan WorkResult {
	p.counter++
	resultChan := make(chan WorkResult, 1)

	p.workChan <- Work{
		ID:         p.nextID(),
		Type:       WorkTypeTopN,
		Items:      items,
		Ranker:     ranker,
		Context:    ctx,
		Limit:      n,
		ResultChan: resultChan,
	}

	return resultChan
}

func (p *Producer) nextID() string {
	p.counter++
	return p.idPrefix + "-" + string(rune('0'+p.counter%10))
}
