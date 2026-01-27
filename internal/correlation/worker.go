package correlation

import (
	"context"

	"github.com/abelbrown/observer/internal/logging"
)

// Worker is a generic worker pool that processes items in parallel.
// It uses Go generics for type-safe input/output channels.
type Worker[In, Out any] struct {
	name    string
	input   chan In
	output  chan Out
	process func(In) Out
	workers int
}

// NewWorker creates a new worker pool.
//   - name: identifier for logging
//   - workers: number of concurrent goroutines
//   - buffer: channel buffer size
//   - fn: the processing function
func NewWorker[In, Out any](name string, workers, buffer int, fn func(In) Out) *Worker[In, Out] {
	return &Worker[In, Out]{
		name:    name,
		input:   make(chan In, buffer),
		output:  make(chan Out, buffer),
		process: fn,
		workers: workers,
	}
}

// Start launches all worker goroutines.
func (w *Worker[In, Out]) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		go w.run(ctx, i)
	}
	logging.Debug("Worker pool started", "name", w.name, "workers", w.workers)
}

func (w *Worker[In, Out]) run(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-w.input:
			if !ok {
				return
			}
			result := w.process(item)
			select {
			case w.output <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// In returns the input channel for sending work.
func (w *Worker[In, Out]) In() chan<- In { return w.input }

// Out returns the output channel for receiving results.
func (w *Worker[In, Out]) Out() <-chan Out { return w.output }

// Close closes the input channel, signaling workers to finish.
func (w *Worker[In, Out]) Close() {
	close(w.input)
}
