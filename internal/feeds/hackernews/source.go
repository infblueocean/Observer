package hackernews

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/httpclient"
)

const (
	baseURL       = "https://hacker-news.firebaseio.com/v0"
	maxItems      = 50 // Fetch top 50 stories
	maxConcurrent = 10 // Parallel fetches
)

// Story represents a Hacker News story from the API
type Story struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Text        string `json:"text"` // For Ask HN, etc.
	By          string `json:"by"`
	Time        int64  `json:"time"`
	Score       int    `json:"score"`
	Descendants int    `json:"descendants"` // Comment count
	Kids        []int  `json:"kids"`        // Comment IDs
}

// Source fetches items from Hacker News API
type Source struct {
	name     string
	endpoint string // topstories, newstories, beststories
	client   *http.Client
}

// New creates a new HN source
func New(name, endpoint string) *Source {
	return &Source{
		name:     name,
		endpoint: endpoint,
		client:   httpclient.Default(),
	}
}

// NewTop creates a source for top stories
func NewTop() *Source {
	return New("HN Top", "topstories")
}

// NewBest creates a source for best stories
func NewBest() *Source {
	return New("HN Best", "beststories")
}

// NewNew creates a source for new stories
func NewNew() *Source {
	return New("HN New", "newstories")
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourceHN
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	// Get story IDs
	ids, err := s.fetchStoryIDs()
	if err != nil {
		return nil, err
	}

	// Limit to maxItems
	if len(ids) > maxItems {
		ids = ids[:maxItems]
	}

	// Fetch stories in parallel
	stories := s.fetchStoriesParallel(ids)

	// Convert to feed items
	items := make([]feeds.Item, 0, len(stories))
	now := time.Now()

	for _, story := range stories {
		if story.Title == "" {
			continue
		}

		// For Ask HN / Show HN without URL, link to HN
		url := story.URL
		if url == "" {
			url = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", story.ID)
		}

		// Build summary with score and comments
		summary := ""
		if story.Text != "" {
			summary = truncate(story.Text, 200)
		}

		items = append(items, feeds.Item{
			ID:         fmt.Sprintf("hn-%d", story.ID),
			Source:     feeds.SourceHN,
			SourceName: s.name,
			SourceURL:  fmt.Sprintf("https://news.ycombinator.com/item?id=%d", story.ID),
			Title:      story.Title,
			Summary:    summary,
			URL:        url,
			Author:     story.By,
			Published:  time.Unix(story.Time, 0),
			Fetched:    now,
			// Extra HN-specific data could go in a metadata field
		})
	}

	return items, nil
}

func (s *Source) fetchStoryIDs() ([]int, error) {
	url := fmt.Sprintf("%s/%s.json", baseURL, s.endpoint)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch story IDs: %w", err)
	}
	defer resp.Body.Close()

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, fmt.Errorf("failed to decode story IDs: %w", err)
	}

	return ids, nil
}

func (s *Source) fetchStoriesParallel(ids []int) []Story {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		stories []Story
		sem     = make(chan struct{}, maxConcurrent)
	)

	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			story, err := s.fetchStory(id)
			if err != nil {
				return
			}

			mu.Lock()
			stories = append(stories, story)
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	return stories
}

func (s *Source) fetchStory(id int) (Story, error) {
	url := fmt.Sprintf("%s/item/%d.json", baseURL, id)

	resp, err := s.client.Get(url)
	if err != nil {
		return Story{}, err
	}
	defer resp.Body.Close()

	var story Story
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return Story{}, err
	}

	return story, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
