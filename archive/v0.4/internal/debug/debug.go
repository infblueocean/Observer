// Package debug provides observability endpoints for debugging and profiling.
package debug

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers
	"runtime"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

var (
	server   *http.Server
	serverMu sync.Mutex
)

// StartDebugServer starts the debug HTTP server with pprof and custom endpoints.
// Call StopDebugServer to gracefully shut down.
// Safe to call multiple times; subsequent calls are no-ops.
func StartDebugServer(addr string) {
	serverMu.Lock()
	defer serverMu.Unlock()

	if server != nil {
		return // Already started
	}

	mux := http.NewServeMux()

	// Custom endpoints
	mux.HandleFunc("/debug/goroutines", handleGoroutines)
	mux.HandleFunc("/debug/memory", handleMemory)
	mux.HandleFunc("/debug/health", handleHealth)

	// pprof handlers (registered via import side effect on DefaultServeMux)
	// Forward pprof requests to default mux
	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)

	server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		logging.Info("Debug server starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("Debug server error", "error", err)
		}
	}()
}

// StopDebugServer gracefully shuts down the debug server.
// Safe to call multiple times; subsequent calls are no-ops.
func StopDebugServer() {
	serverMu.Lock()
	defer serverMu.Unlock()

	if server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logging.Info("Debug server stopping")
	if err := server.Shutdown(ctx); err != nil {
		logging.Error("Debug server shutdown error", "error", err)
	}
	server = nil
}

func handleGoroutines(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	count := runtime.NumGoroutine()
	fmt.Fprintf(w, "goroutines: %d\n", count)
}

func handleMemory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintf(w, "alloc: %d MB\n", m.Alloc/1024/1024)
	fmt.Fprintf(w, "total_alloc: %d MB\n", m.TotalAlloc/1024/1024)
	fmt.Fprintf(w, "sys: %d MB\n", m.Sys/1024/1024)
	fmt.Fprintf(w, "heap_alloc: %d MB\n", m.HeapAlloc/1024/1024)
	fmt.Fprintf(w, "heap_inuse: %d MB\n", m.HeapInuse/1024/1024)
	fmt.Fprintf(w, "num_gc: %d\n", m.NumGC)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "ok\n")
}
