//go:build ignore

// Observer v0.5 - MVC Architecture Rewrite
//
// Architecture Overview:
//   Model (internal/model)       - SQLite store, source of truth
//   Controller (internal/controller) - Filter pipelines, data flow
//   View (internal/view)         - Bubble Tea UI components
//
// This is a clean-room implementation following the v0.5 architecture plan.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abelbrown/observer/internal/controller/controllers"
	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/view"
	"github.com/abelbrown/observer/internal/work"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Initialize logging
	if err := logging.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logging: %v\n", err)
	}
	defer logging.Close()

	logging.Info("Observer v0.5 starting", "architecture", "MVC")

	// Ensure data directory exists
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fatal("Failed to get home directory: %v", err)
	}
	dataDir := filepath.Join(homeDir, ".observer")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fatal("Failed to create data directory: %v", err)
	}

	// Initialize MODEL layer (SQLite store)
	dbPath := filepath.Join(dataDir, "observer-v05.db")
	store, err := model.NewStore(dbPath)
	if err != nil {
		fatal("Failed to initialize store: %v", err)
	}
	defer store.Close()
	logging.Info("Store initialized", "path", dbPath)

	// Initialize work pool (GCD pattern - central async hub)
	pool := work.NewPool(0) // 0 = use NumCPU
	pool.Start(context.Background())
	defer pool.Stop()
	logging.Info("Work pool started")

	// Initialize CONTROLLER layer

	// Fetch controller - manages periodic feed fetching
	sources := fetch.DefaultSources()
	fetchCtrl := controllers.NewFetchController(sources, store, pool)
	fetchCtrl.Start(context.Background())
	defer fetchCtrl.Stop()
	logging.Info("Fetch controller started", "sources", len(sources))

	// Main feed controller - filter pipeline for stream view
	mainFeedCtrl := controllers.NewMainFeedController(controllers.DefaultMainFeedConfig())
	logging.Info("Main feed controller initialized")

	// Initialize VIEW layer
	app := view.New(store, pool, fetchCtrl, mainFeedCtrl)

	// Start session tracking
	sessionID, err := store.StartSession()
	if err != nil {
		logging.Warn("Failed to start session", "error", err)
	}
	defer func() {
		if sessionID > 0 {
			store.EndSession(sessionID)
		}
	}()

	// Run the Bubble Tea program
	p := tea.NewProgram(app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	logging.Info("Starting UI")
	if _, err := p.Run(); err != nil {
		logging.Error("Application error", "error", err)
		fatal("Error: %v", err)
	}

	logging.Info("Observer v0.5 exiting normally")
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
