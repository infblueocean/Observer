package e2e

import (
	"os"
	"path/filepath"
	"time"

	"github.com/abelbrown/observer/internal/store"
)

func seedFixtureDB(homeDir string) error {
	dataDir := filepath.Join(homeDir, ".observer")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	dbPath := filepath.Join(dataDir, "observer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	now := time.Now().UTC()
	items := []store.Item{
		{
			ID:         "item-1",
			SourceType: "rss",
			SourceName: "fixture",
			Title:      "Fixture Item One",
			Summary:    "A deterministic item for UI tests.",
			URL:        "https://example.com/fixture-1",
			Author:     "Test",
			Published:  now.Add(-10 * time.Minute),
			Fetched:    now,
		},
	}
	if _, err := st.SaveItems(items); err != nil {
		return err
	}
	if err := st.SaveEmbedding("item-1", []float32{0.2, 0.1, 0.4}); err != nil {
		return err
	}
	return nil
}

func readSnapshot(f *os.File) string {
	if err := f.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		return ""
	}
	out := make([]byte, 0, 8192)
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(out)
}
