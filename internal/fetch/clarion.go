package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/infblueocean/clarion"
	_ "github.com/infblueocean/clarion/catalog"

	"github.com/abelbrown/observer/internal/otel"
	"github.com/abelbrown/observer/internal/store"
)

// ClarionProvider fetches items from Clarion sources.
type ClarionProvider struct {
	sources []clarion.Source
	opts    clarion.FetchOptions
	logger  *otel.Logger
}

// NewClarionProvider creates a ClarionProvider.
// If sources is nil, all registered Clarion sources are used.
func NewClarionProvider(sources []clarion.Source, opts clarion.FetchOptions, l *otel.Logger) *ClarionProvider {
	if sources == nil {
		sources = clarion.AllSources()
	}
	if l == nil {
		l = otel.NewNullLogger()
	}
	return &ClarionProvider{sources: sources, opts: opts, logger: l}
}

// Fetch retrieves items from all configured Clarion sources.
func (p *ClarionProvider) Fetch(ctx context.Context) ([]store.Item, error) {
	results := clarion.FetchWithOptions(ctx, p.opts, p.sources...)

	var items []store.Item
	var errCount int
	for _, r := range results {
		if r.Err != nil {
			p.logger.Emit(otel.Event{Kind: otel.KindFetchError, Level: otel.LevelWarn, Comp: "fetch", Source: r.Source.Name, Err: r.Err.Error()})
			errCount++
			continue
		}
		for _, ci := range r.Items {
			items = append(items, convertItem(ci))
		}
	}

	if errCount > 0 && errCount == len(results) {
		return nil, fmt.Errorf("all %d sources failed", errCount)
	}
	return items, nil
}

func convertItem(ci clarion.Item) store.Item {
	id := ci.ID
	if id == "" {
		id = ci.URL
	}
	if id == "" {
		id = ci.Title
	}

	summary := ci.Summary
	if summary == "" && ci.Content != "" {
		summary = truncate(ci.Content, 500)
	}

	author := ci.Author
	if author == "" && len(ci.Authors) > 0 {
		author = ci.Authors[0]
	}

	published := ci.Published
	if published.IsZero() {
		published = ci.Fetched
	}
	fetched := ci.Fetched
	if fetched.IsZero() {
		fetched = time.Now()
	}

	return store.Item{
		ID:         hashString(id),
		SourceType: string(ci.SourceType),
		SourceName: ci.SourceName,
		Title:      ci.Title,
		Summary:    summary,
		URL:        ci.URL,
		Author:     author,
		Published:  published,
		Fetched:    fetched,
	}
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
