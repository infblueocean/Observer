package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	expect "github.com/Netflix/go-expect"
	"github.com/creack/pty"
)

// buildObserver builds the observer binary for testing.
// Returns the path to the binary and a cleanup function.
func buildObserver(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "observer")

	// Get the project root directory
	rootDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Assume we are in test/e2e, go up 2 levels
	rootDir = filepath.Join(rootDir, "..", "..")

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/observer")
	cmd.Dir = rootDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	return binPath, func() { os.RemoveAll(dir) }
}

func TestE2E_Search(t *testing.T) {
	binPath, cleanup := buildObserver(t)
	defer cleanup()

	// Setup a clean home directory for the test to avoid messing with real data
	homeDir := t.TempDir()

	if err := seedFixtureDB(homeDir); err != nil {
		t.Fatalf("failed to seed fixture db: %v", err)
	}

	// Run command
	cmd := exec.Command(binPath)
	// Point HOME to temp dir so it uses a fresh ~/.observer/observer.db
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"OBSERVER_E2E=1",
		"JINA_API_KEY=dummy-key",
	)

	// Create PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start pty: %v", err)
	}
	defer func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
	}()

	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("failed to set pty size: %v", err)
	}

	// Capture output for debugging
	var outputBuf bytes.Buffer

	// Create expect console
	console, err := expect.NewConsole(
		expect.WithStdin(ptmx),
		expect.WithStdout(&outputBuf),
		expect.WithDefaultTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create console: %v", err)
	}
	defer console.Close()

	// 1. Wait for startup (chronological view)
	t.Log("Waiting for startup (1/1)...")
	if _, err := console.ExpectString("1/1"); err != nil {
		// Dump logs
		if logs, err := os.ReadFile(filepath.Join(homeDir, ".observer", "observer.log")); err == nil {
			t.Logf("observer.log:\n%s", logs)
		}
		
		t.Fatalf("Startup failed: '1/1' not found: %v\nScreen:\n%s", err, outputBuf.String())
	}

	// 2. Open Search Mode
	t.Log("Sending slash...")
	time.Sleep(500 * time.Millisecond) // Allow UI to stabilize
	if _, err := console.Send("/"); err != nil {
		t.Fatalf("failed to send slash: %v", err)
	}

	// 3. Verify Search Input Appears
	t.Log("Waiting for search prompt...")
	if _, err := console.ExpectString("search..."); err != nil {
		t.Fatalf("search prompt not found: %v\nOutput buffer:\n%s", err, outputBuf.String())
	}

	// 4. Type a query
	t.Log("Typing 'test-query'")
	if _, err := console.Send("test-query"); err != nil {
		t.Fatalf("failed to send query: %v", err)
	}

	// 5. Submit Search
	t.Log("Sending Enter...")
	if _, err := console.Send("\n"); err != nil {
		t.Fatalf("failed to send Enter: %v", err)
	}

	// 6. Verify Search State
	t.Log("Waiting for searching status...")
	_, err = console.ExpectString("Searching for")
	if err != nil {
		// Retry check for query text which also appears in results bar
		_, err = console.ExpectString("test-query")
		if err != nil {
			t.Fatalf("searching status not found: %v\nOutput buffer:\n%s", err, outputBuf.String())
		}
	}

	// 7. Verify Results
	if _, err := console.ExpectString("Fixture Item One"); err != nil {
		t.Fatalf("expected fixture item to be visible: %v\nOutput buffer:\n%s", err, outputBuf.String())
	}

	// Wait a bit for async stuff
	time.Sleep(1 * time.Second)

	// Send 'q' to quit
	t.Log("Sending 'q'...")
	if _, err := console.Send("q"); err != nil {
		t.Fatalf("failed to send q: %v", err)
	}

	// Verify process exits
	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Log("Process exited successfully")
	case <-time.After(2 * time.Second):
		t.Error("Process did not exit after 'q'")
	}
}
