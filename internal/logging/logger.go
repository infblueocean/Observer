package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	KeyVals []interface{}
}

// Ring buffer for recent log entries
const maxLogEntries = 100

var (
	// Logger is the global logger instance
	Logger *log.Logger

	// logFile is the file handle for the log file
	logFile *os.File

	// recentLogs is a ring buffer of recent log entries
	recentLogs   []LogEntry
	recentLogsMu sync.RWMutex
	logIndex     int
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

	// Initialize ring buffer
	recentLogs = make([]LogEntry, maxLogEntries)
	logIndex = 0

	Logger.Info("Observer started", "version", "0.1.0")
	return nil
}

// addToBuffer adds a log entry to the ring buffer
func addToBuffer(level, msg string, keyvals []interface{}) {
	recentLogsMu.Lock()
	defer recentLogsMu.Unlock()

	// Skip if logging not initialized (e.g., in tests)
	if recentLogs == nil {
		return
	}

	recentLogs[logIndex] = LogEntry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
		KeyVals: keyvals,
	}
	logIndex = (logIndex + 1) % maxLogEntries
}

// GetRecentLogs returns the most recent log entries (newest first)
func GetRecentLogs(count int) []LogEntry {
	recentLogsMu.RLock()
	defer recentLogsMu.RUnlock()

	if recentLogs == nil {
		return nil
	}

	if count > maxLogEntries {
		count = maxLogEntries
	}

	result := make([]LogEntry, 0, count)

	// Start from most recent and go backwards
	idx := (logIndex - 1 + maxLogEntries) % maxLogEntries
	for i := 0; i < count; i++ {
		entry := recentLogs[idx]
		if entry.Time.IsZero() {
			break // Hit uninitialized entries
		}
		result = append(result, entry)
		idx = (idx - 1 + maxLogEntries) % maxLogEntries
	}

	return result
}

// FormatEntry formats a log entry for display
func (e LogEntry) Format() string {
	if e.Time.IsZero() {
		return ""
	}

	// Format key-value pairs
	var kvParts []string
	for i := 0; i < len(e.KeyVals)-1; i += 2 {
		key := fmt.Sprintf("%v", e.KeyVals[i])
		val := fmt.Sprintf("%v", e.KeyVals[i+1])
		// Truncate long values
		if len(val) > 40 {
			val = val[:37] + "..."
		}
		kvParts = append(kvParts, fmt.Sprintf("%s=%s", key, val))
	}

	kvStr := ""
	if len(kvParts) > 0 {
		kvStr = " " + strings.Join(kvParts, " ")
	}

	return fmt.Sprintf("%s [%s] %s%s",
		e.Time.Format("15:04:05"),
		e.Level,
		e.Message,
		kvStr)
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
	addToBuffer("INFO", msg, keyvals)
	if Logger != nil {
		Logger.Info(msg, keyvals...)
	}
}

// Debug logs a debug message
func Debug(msg string, keyvals ...interface{}) {
	addToBuffer("DEBUG", msg, keyvals)
	if Logger != nil {
		Logger.Debug(msg, keyvals...)
	}
}

// Warn logs a warning message
func Warn(msg string, keyvals ...interface{}) {
	addToBuffer("WARN", msg, keyvals)
	if Logger != nil {
		Logger.Warn(msg, keyvals...)
	}
}

// Error logs an error message
func Error(msg string, keyvals ...interface{}) {
	addToBuffer("ERROR", msg, keyvals)
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
