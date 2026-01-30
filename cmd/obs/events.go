package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// eventRecord mirrors otel.Event for JSON decoding.
// We decode from JSONL rather than importing otel to keep this
// subcommand usable even if the event schema evolves.
type eventRecord struct {
	Time      time.Time      `json:"t"`
	Level     string         `json:"level"`
	Kind      string         `json:"kind"`
	Comp      string         `json:"comp"`
	SessionID string         `json:"session_id"`
	QueryID   string         `json:"qid"`
	DurMs     float64        `json:"dur_ms"`
	Count     int            `json:"count"`
	Source    string         `json:"source"`
	Query     string         `json:"query"`
	Dims      int            `json:"dims"`
	Err       string         `json:"err"`
	Msg       string         `json:"msg"`
	Extra     map[string]any `json:"extra"`
}

// levelRank returns a numeric rank for filtering (higher = more severe).
func levelRank(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	default:
		return 0
	}
}

func runEvents() {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	tail := fs.Int("tail", 50, "Number of recent lines to show")
	follow := fs.Bool("f", false, "Follow mode (like tail -f)")
	kind := fs.String("kind", "", "Filter by event kind prefix (e.g. 'search')")
	level := fs.String("level", "", "Minimum level: debug, info, warn, error")
	comp := fs.String("comp", "", "Filter by component name")
	qid := fs.String("qid", "", "Filter by query ID")
	rawJSON := fs.Bool("json", false, "Output raw JSON lines")
	fs.Parse(os.Args[1:])

	logPath := eventLogPath()

	f, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Event log not found at %s\n", logPath)
		fmt.Fprintf(os.Stderr, "  Run the observer TUI first to generate events.\n")
		os.Exit(1)
	}
	defer f.Close()

	minLevel := levelRank(*level)

	matchFn := func(ev eventRecord) bool {
		if *kind != "" && !strings.HasPrefix(ev.Kind, *kind) {
			return false
		}
		if *level != "" && levelRank(ev.Level) < minLevel {
			return false
		}
		if *comp != "" && ev.Comp != *comp {
			return false
		}
		if *qid != "" && ev.QueryID != *qid {
			return false
		}
		return true
	}

	formatFn := func(ev eventRecord, raw []byte) string {
		if *rawJSON {
			return string(raw)
		}
		ts := ev.Time.Format("15:04:05.000")
		lvl := strings.ToUpper(ev.Level)
		if lvl == "" {
			lvl = "?"
		}

		parts := []string{fmt.Sprintf("%s %-5s [%-6s] %-22s", ts, lvl, ev.Comp, ev.Kind)}

		if ev.Msg != "" {
			parts = append(parts, "— "+ev.Msg)
		}
		if ev.DurMs > 0 {
			parts = append(parts, fmt.Sprintf("(%.*fms)", durPrecision(ev.DurMs), ev.DurMs))
		}
		if ev.Count > 0 {
			parts = append(parts, fmt.Sprintf("n=%d", ev.Count))
		}
		if ev.Source != "" {
			parts = append(parts, "src="+ev.Source)
		}
		if ev.Query != "" {
			parts = append(parts, fmt.Sprintf("q=%q", ev.Query))
		}
		if ev.Err != "" {
			parts = append(parts, "err="+ev.Err)
		}

		return strings.Join(parts, " ")
	}

	// Read all lines, keep last N matching
	if !*follow {
		lines := readTailLines(f, *tail, matchFn)
		for _, l := range lines {
			fmt.Println(formatFn(l.ev, l.raw))
		}
		return
	}

	// Follow mode: print last N then poll for new lines
	lines := readTailLines(f, *tail, matchFn)
	for _, l := range lines {
		fmt.Println(formatFn(l.ev, l.raw))
	}

	// Now seek to end and poll
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return
		}
		line = trimLine(line)
		if len(line) == 0 {
			continue
		}
		var ev eventRecord
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if matchFn(ev) {
			fmt.Println(formatFn(ev, line))
		}
	}
}

type parsedLine struct {
	ev  eventRecord
	raw []byte
}

// readTailLines reads the file and returns the last n lines matching the filter.
func readTailLines(f *os.File, n int, match func(eventRecord) bool) []parsedLine {
	scanner := bufio.NewScanner(f)
	// Allow large lines (some events may have big Extra maps)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var ring []parsedLine
	if n > 0 {
		ring = make([]parsedLine, 0, n)
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var ev eventRecord
		if json.Unmarshal(raw, &ev) != nil {
			continue
		}
		if !match(ev) {
			continue
		}
		// Make a copy of raw since scanner reuses the buffer
		rawCopy := make([]byte, len(raw))
		copy(rawCopy, raw)

		if len(ring) < n {
			ring = append(ring, parsedLine{ev: ev, raw: rawCopy})
		} else {
			// Shift left
			copy(ring, ring[1:])
			ring[n-1] = parsedLine{ev: ev, raw: rawCopy}
		}
	}

	return ring
}

func trimLine(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func durPrecision(ms float64) int {
	if ms >= 100 {
		return 0
	}
	if ms >= 1 {
		return 1
	}
	return 2
}
