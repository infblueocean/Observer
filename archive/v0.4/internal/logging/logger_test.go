package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoggingInit(t *testing.T) {
	// Initialize logging
	if err := Init(); err != nil {
		t.Fatalf("Failed to initialize logging: %v", err)
	}
	defer Close()

	// Check that log directory was created
	homeDir, _ := os.UserHomeDir()
	logDir := filepath.Join(homeDir, ".observer", "logs")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Errorf("Log directory was not created: %s", logDir)
	}

	// Test logging functions
	Info("Test info message", "key", "value")
	Debug("Test debug message", "count", 42)
	Warn("Test warning message", "source", "test")
	Error("Test error message", "error", "test error")

	// Check that log file exists
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("Failed to read log directory: %v", err)
	}
	if len(files) == 0 {
		t.Error("No log files were created")
	}
}
