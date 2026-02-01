package store

import (
	"testing"
	"time"
)

// Corpus of realistic news headlines for search quality testing.
// Each item has a clear topic; tests assert that queries return
// topically relevant items and NOT irrelevant ones.
func ftsCorpus() []Item {
	now := time.Now()
	return []Item{
		// NFL / Football
		{ID: "nfl1", SourceType: "rss", SourceName: "espn", Title: "NFL Draft 2025: Top Prospects and Mock Draft Analysis", Summary: "Breaking down the top quarterback and wide receiver prospects for the upcoming NFL draft", URL: "http://espn.com/nfl-draft", Published: now, Fetched: now},
		{ID: "nfl2", SourceType: "rss", SourceName: "espn", Title: "Patrick Mahomes Leads Chiefs to Overtime Victory", Summary: "Kansas City Chiefs quarterback Patrick Mahomes threw four touchdowns in Sunday's game", URL: "http://espn.com/mahomes", Published: now, Fetched: now},
		{ID: "nfl3", SourceType: "rss", SourceName: "fox", Title: "Super Bowl LVIII: Biggest Plays and Highlights", Summary: "A recap of the best moments from this year's Super Bowl championship game", URL: "http://fox.com/superbowl", Published: now, Fetched: now},
		{ID: "nfl4", SourceType: "rss", SourceName: "espn", Title: "NFL Free Agency: Top Available Players and Predictions", Summary: "Which teams will land the biggest free agent signings this offseason", URL: "http://espn.com/freeagency", Published: now, Fetched: now},

		// Tech / AI
		{ID: "tech1", SourceType: "rss", SourceName: "hn", Title: "OpenAI Releases GPT-5 with Multimodal Reasoning", Summary: "The latest large language model features improved code generation and image understanding", URL: "http://hn.com/gpt5", Published: now, Fetched: now},
		{ID: "tech2", SourceType: "rss", SourceName: "hn", Title: "Rust 2.0 Announced with Async Improvements", Summary: "Major release brings simplified async/await patterns and better compile times", URL: "http://hn.com/rust2", Published: now, Fetched: now},
		{ID: "tech3", SourceType: "rss", SourceName: "ars", Title: "Apple Vision Pro Sales Disappoint in Q4", Summary: "Mixed reality headset struggles to find mainstream adoption despite developer interest", URL: "http://ars.com/visionpro", Published: now, Fetched: now},
		{ID: "tech4", SourceType: "rss", SourceName: "hn", Title: "SQLite Adds Built-in Vector Search Extension", Summary: "The embedded database now supports approximate nearest neighbor search for embeddings", URL: "http://hn.com/sqlite-vec", Published: now, Fetched: now},

		// Finance / Markets
		{ID: "fin1", SourceType: "rss", SourceName: "wsj", Title: "Federal Reserve Holds Interest Rates Steady", Summary: "The Fed signals patience on rate cuts amid persistent inflation concerns", URL: "http://wsj.com/fed", Published: now, Fetched: now},
		{ID: "fin2", SourceType: "rss", SourceName: "wsj", Title: "NVIDIA Stock Hits All-Time High on AI Demand", Summary: "Chipmaker's market cap surpasses $3 trillion as data center revenue surges", URL: "http://wsj.com/nvda", Published: now, Fetched: now},
		{ID: "fin3", SourceType: "rss", SourceName: "ft", Title: "Bitcoin Surges Past $100K as ETF Inflows Accelerate", Summary: "Institutional investors drive cryptocurrency rally following spot ETF approvals", URL: "http://ft.com/btc", Published: now, Fetched: now},

		// Politics
		{ID: "pol1", SourceType: "rss", SourceName: "bbc", Title: "EU Parliament Passes Comprehensive AI Regulation Act", Summary: "New rules will require transparency and risk assessments for AI systems deployed in Europe", URL: "http://bbc.com/eu-ai", Published: now, Fetched: now},
		{ID: "pol2", SourceType: "rss", SourceName: "bbc", Title: "Ukraine Peace Talks Resume in Geneva", Summary: "Diplomatic efforts intensify as both sides agree to new round of negotiations", URL: "http://bbc.com/ukraine", Published: now, Fetched: now},

		// Science
		{ID: "sci1", SourceType: "rss", SourceName: "nature", Title: "James Webb Telescope Detects New Exoplanet Atmosphere", Summary: "Scientists find water vapor and methane in the atmosphere of a rocky planet 40 light-years away", URL: "http://nature.com/jwst", Published: now, Fetched: now},
		{ID: "sci2", SourceType: "rss", SourceName: "nature", Title: "CRISPR Gene Therapy Shows Promise for Sickle Cell Disease", Summary: "Clinical trials demonstrate lasting remission in patients with inherited blood disorder", URL: "http://nature.com/crispr", Published: now, Fetched: now},

		// Weather (noise — should never match topical queries)
		{ID: "wx1", SourceType: "rss", SourceName: "weather", Title: "Severe Thunderstorm Warning for Central Texas", Summary: "Large hail and damaging winds expected through Tuesday evening", URL: "http://weather.com/tx", Published: now, Fetched: now},
	}
}

func TestFTSQuality_NFL(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// FTS5 is lexical: "nfl" only matches items containing the literal word "NFL".
	// Items about football that don't say "NFL" (e.g. "Super Bowl", "Mahomes")
	// won't match — that's expected and is why semantic search exists.
	results, err := s.SearchFTS("nfl", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 NFL results, got %d", len(results))
	}

	// Items with literal "NFL" in title/summary should be present
	ids := idSet(results)
	for _, want := range []string{"nfl1", "nfl4"} {
		if !ids[want] {
			t.Errorf("expected %s (contains literal 'NFL') in results", want)
		}
	}

	// No tech/finance/weather items should appear
	for _, bad := range []string{"tech1", "tech2", "tech3", "tech4", "fin1", "fin2", "fin3", "wx1"} {
		if ids[bad] {
			t.Errorf("irrelevant item %s should not appear in NFL results", bad)
		}
	}
}

func TestFTSQuality_NFLBroadQuery(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// OR query to find all football-related items by various keywords
	results, err := s.SearchFTS("nfl OR touchdown OR quarterback OR chiefs", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	ids := idSet(results)
	// nfl1 matches "NFL" and "quarterback"
	// nfl2 matches "Chiefs" and "quarterback" and "touchdowns"
	// nfl4 matches "NFL"
	for _, want := range []string{"nfl1", "nfl2", "nfl4"} {
		if !ids[want] {
			t.Errorf("expected %s in broad NFL query results", want)
		}
	}

	// No tech/weather items should appear
	for _, bad := range []string{"tech1", "tech2", "tech4", "wx1"} {
		if ids[bad] {
			t.Errorf("irrelevant item %s should not appear in broad NFL results", bad)
		}
	}
}

func TestFTSQuality_SuperBowl(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchFTS("super bowl", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 Super Bowl result, got 0")
	}

	// The Super Bowl article must be the top result
	if results[0].ID != "nfl3" {
		t.Errorf("expected Super Bowl article (nfl3) first, got %s: %q", results[0].ID, results[0].Title)
	}
}

func TestFTSQuality_AI(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// "AI" matches items with the literal abbreviation
	results, err := s.SearchFTS("AI", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	ids := idSet(results)
	// tech1 has "AI" in summary ("large language model" doesn't match, but it has no "AI")
	// pol1 has "AI Regulation Act" in title
	// Let's check what we actually get
	if !ids["pol1"] {
		t.Errorf("expected pol1 (AI Regulation Act) in AI results")
	}

	// NFL and weather should not appear
	for _, bad := range []string{"nfl1", "nfl2", "nfl3", "nfl4", "wx1"} {
		if ids[bad] {
			t.Errorf("irrelevant item %s should not appear in AI results", bad)
		}
	}
}

func TestFTSQuality_Cryptocurrency(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchFTS("bitcoin cryptocurrency", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 crypto result, got 0")
	}

	ids := idSet(results)
	if !ids["fin3"] {
		t.Error("expected Bitcoin article (fin3) in crypto results")
	}

	// Sports items should not appear
	for _, bad := range []string{"nfl1", "nfl2", "nfl3", "nfl4"} {
		if ids[bad] {
			t.Errorf("irrelevant item %s should not appear in crypto results", bad)
		}
	}
}

func TestFTSQuality_SpaceScience(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// FTS5 treats multiple words as implicit AND.
	// "telescope exoplanet" matches sci1 which has both words.
	results, err := s.SearchFTS("telescope exoplanet", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 space science result, got 0")
	}

	if results[0].ID != "sci1" {
		t.Errorf("expected JWST article (sci1) first, got %s: %q", results[0].ID, results[0].Title)
	}
}

func TestFTSQuality_NoFalsePositives(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// A query that matches nothing in the corpus
	results, err := s.SearchFTS("volleyball olympics swimming", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) != 0 {
		titles := make([]string, len(results))
		for i, r := range results {
			titles[i] = r.Title
		}
		t.Errorf("expected 0 results for unrelated query, got %d: %v", len(results), titles)
	}
}

func TestFTSQuality_PartialMatch(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SaveItems(ftsCorpus()); err != nil {
		t.Fatal(err)
	}

	// "mahomes" — should find the Patrick Mahomes article
	results, err := s.SearchFTS("mahomes", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result for 'mahomes', got %d", len(results))
	}
	if results[0].ID != "nfl2" {
		t.Errorf("expected Mahomes article (nfl2), got %s", results[0].ID)
	}
}

func TestFTSQuality_ColumnWeighting(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	items := []Item{
		// "rust" in title — should rank higher (title weight=10)
		{ID: "title", SourceType: "rss", SourceName: "a", Title: "Rust Programming Language Update", Summary: "New features added", URL: "http://a.com/1", Published: now, Fetched: now},
		// "rust" only in summary — lower rank (summary weight=5)
		{ID: "summary", SourceType: "rss", SourceName: "b", Title: "Programming Language Trends", Summary: "Rust continues to grow in popularity among systems developers", URL: "http://b.com/1", Published: now, Fetched: now},
	}

	if _, err := s.SaveItems(items); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchFTS("rust", 50)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Title match should rank above summary-only match
	if results[0].ID != "title" {
		t.Errorf("expected title-match item first (BM25 weighting), got %s", results[0].ID)
	}
}

// idSet returns a set of item IDs for easy lookup.
func idSet(items []Item) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item.ID] = true
	}
	return m
}
