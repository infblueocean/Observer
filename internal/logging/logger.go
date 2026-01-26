package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
)

var (
	// Logger is the global logger instance
	Logger *log.Logger

	// logFile is the file handle for the log file
	logFile *os.File
)

// Init initializes the logging system
func Init() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	logDir := filepath.Join(homeDir, ".observer", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create log file with date
	logFileName := fmt.Sprintf("observer-%s.log", time.Now().Format("2006-01-02"))
	logPath := filepath.Join(logDir, logFileName)

	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Create logger that writes to file
	Logger = log.NewWithOptions(logFile, log.Options{
		ReportTimestamp: true,
		TimeFormat:      time.RFC3339,
		Level:           log.DebugLevel,
	})

	Logger.Info("Observer started", "version", "0.1.0")
	return nil
}

// Close closes the log file
func Close() {
	if Logger != nil {
		Logger.Info("Observer shutting down")
	}
	if logFile != nil {
		logFile.Close()
	}
}

// Info logs an info message
func Info(msg string, keyvals ...interface{}) {
	if Logger != nil {
		Logger.Info(msg, keyvals...)
	}
}

// Debug logs a debug message
func Debug(msg string, keyvals ...interface{}) {
	if Logger != nil {
		Logger.Debug(msg, keyvals...)
	}
}

// Warn logs a warning message
func Warn(msg string, keyvals ...interface{}) {
	if Logger != nil {
		Logger.Warn(msg, keyvals...)
	}
}

// Error logs an error message
func Error(msg string, keyvals ...interface{}) {
	if Logger != nil {
		Logger.Error(msg, keyvals...)
	}
}

// Fatal logs an error message and exits
func Fatal(msg string, keyvals ...interface{}) {
	if Logger != nil {
		Logger.Fatal(msg, keyvals...)
	}
}

// WithPrefix returns a logger with a prefix
func WithPrefix(prefix string) *log.Logger {
	if Logger != nil {
		return Logger.WithPrefix(prefix)
	}
	return nil
}
