package main

import (
	"fmt"
	"os"

	"github.com/abelbrown/observer/internal/app"
	"github.com/abelbrown/observer/internal/debug"
	"github.com/abelbrown/observer/internal/logging"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Initialize logging
	if err := logging.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logging: %v\n", err)
	}
	defer logging.Close()

	// Start debug server for pprof and metrics
	debug.StartDebugServer("localhost:6060")
	defer debug.StopDebugServer()

	// Create the app
	m, err := app.New()
	if err != nil {
		logging.Error("Failed to initialize application", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Run with alt screen and mouse support
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Enable mouse tracking for hover/scroll
	)

	if _, err := p.Run(); err != nil {
		logging.Error("Application error", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
