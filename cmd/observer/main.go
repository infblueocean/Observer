package main

import (
	"fmt"
	"os"

	"github.com/abelbrown/observer/internal/app"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Create the app
	m := app.New()

	// Run with alt screen (full terminal takeover)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
