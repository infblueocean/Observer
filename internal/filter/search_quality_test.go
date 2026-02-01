package filter

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/store"
)

// Topic-clustered embeddings for search quality testing.
//
// Each embedding is a 6-dimensional vector where dimensions represent:
//   [0] sports   [1] tech   [2] finance   [3] politics   [4] science   [5] weather
//
// Items have high values in their topic dimension and low noise elsewhere.
// This lets us test that cosine similarity correctly surfaces topically
// relevant items, which is the core of what makes search results "good."

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// qualityCorpus returns items and their topic embeddings.
func qualityCorpus() ([]store.Item, map[string][]float32) {
	now := time.Now()

	items := []store.Item{
		// Sports cluster
		{ID: "nfl1", Title: "NFL Draft 2025: Top Prospects", SourceName: "espn", Published: now},
		{ID: "nfl2", Title: "Mahomes Leads Chiefs to Victory", SourceName: "espn", Published: now},
		{ID: "nfl3", Title: "Super Bowl LVIII Highlights", SourceName: "fox", Published: now},
		{ID: "nba1", Title: "NBA Playoffs: Lakers vs Celtics Preview", SourceName: "espn", Published: now},

		// Tech cluster
		{ID: "tech1", Title: "GPT-5 Released with Multimodal Reasoning", SourceName: "hn", Published: now},
		{ID: "tech2", Title: "Rust 2.0 Announced with Async Improvements", SourceName: "hn", Published: now},
		{ID: "tech3", Title: "Apple Vision Pro Sales Disappoint", SourceName: "ars", Published: now},
		{ID: "tech4", Title: "SQLite Adds Vector Search Extension", SourceName: "hn", Published: now},

		// Finance cluster
		{ID: "fin1", Title: "Federal Reserve Holds Rates Steady", SourceName: "wsj", Published: now},
		{ID: "fin2", Title: "NVIDIA Stock Hits All-Time High", SourceName: "wsj", Published: now},
		{ID: "fin3", Title: "Bitcoin Surges Past $100K", SourceName: "ft", Published: now},

		// Politics cluster
		{ID: "pol1", Title: "EU Passes AI Regulation Act", SourceName: "bbc", Published: now},
		{ID: "pol2", Title: "Ukraine Peace Talks Resume", SourceName: "bbc", Published: now},

		// Science cluster
		{ID: "sci1", Title: "James Webb Detects New Exoplanet", SourceName: "nature", Published: now},
		{ID: "sci2", Title: "CRISPR Gene Therapy for Sickle Cell", SourceName: "nature", Published: now},

		// Weather (noise)
		{ID: "wx1", Title: "Severe Thunderstorm Warning Texas", SourceName: "weather", Published: now},
	}

	//                          sports  tech  finance  politics  science  weather
	embeddings := map[string][]float32{
		"nfl1":  normalize([]float32{0.95, 0.02, 0.01, 0.01, 0.00, 0.00}),
		"nfl2":  normalize([]float32{0.93, 0.01, 0.01, 0.00, 0.00, 0.00}),
		"nfl3":  normalize([]float32{0.97, 0.01, 0.00, 0.01, 0.00, 0.00}),
		"nba1":  normalize([]float32{0.90, 0.02, 0.01, 0.01, 0.00, 0.00}),
		"tech1": normalize([]float32{0.01, 0.92, 0.02, 0.05, 0.02, 0.00}),
		"tech2": normalize([]float32{0.00, 0.96, 0.01, 0.01, 0.01, 0.00}),
		"tech3": normalize([]float32{0.01, 0.88, 0.10, 0.01, 0.00, 0.00}), // slight finance overlap (stock price)
		"tech4": normalize([]float32{0.01, 0.94, 0.01, 0.01, 0.02, 0.00}),
		"fin1":  normalize([]float32{0.00, 0.02, 0.95, 0.05, 0.00, 0.00}), // slight politics overlap (Fed policy)
		"fin2":  normalize([]float32{0.00, 0.15, 0.90, 0.01, 0.00, 0.00}), // slight tech overlap (NVIDIA)
		"fin3":  normalize([]float32{0.00, 0.08, 0.92, 0.01, 0.00, 0.00}),
		"pol1":  normalize([]float32{0.01, 0.10, 0.02, 0.90, 0.01, 0.00}), // slight tech overlap (AI regulation)
		"pol2":  normalize([]float32{0.00, 0.01, 0.01, 0.96, 0.00, 0.00}),
		"sci1":  normalize([]float32{0.00, 0.05, 0.00, 0.01, 0.95, 0.00}),
		"sci2":  normalize([]float32{0.00, 0.08, 0.00, 0.01, 0.93, 0.00}), // slight tech overlap (CRISPR tech)
		"wx1":   normalize([]float32{0.01, 0.00, 0.00, 0.00, 0.02, 0.95}),
	}

	return items, embeddings
}

// --- Cosine Ranking Quality Tests ---

func TestSearchQuality_SportsQuery(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Query: "football NFL" → sports dimension dominant
	queryEmb := normalize([]float32{0.98, 0.01, 0.00, 0.00, 0.00, 0.00})

	result := RerankByQuery(items, embeddings, queryEmb)

	// Top 4 results should be all sports items
	top4 := idSet(result[:4])
	for _, want := range []string{"nfl1", "nfl2", "nfl3", "nba1"} {
		if !top4[want] {
			t.Errorf("expected sports item %s in top 4 for sports query, got: %v", want, idsOf(result[:4]))
		}
	}

	// Bottom items should not be sports
	bottom := result[len(result)-3:]
	for _, item := range bottom {
		if item.ID == "nfl1" || item.ID == "nfl2" || item.ID == "nfl3" || item.ID == "nba1" {
			t.Errorf("sports item %s should not be in bottom 3", item.ID)
		}
	}
}

func TestSearchQuality_TechQuery(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Query: "programming language AI" → tech dimension dominant
	queryEmb := normalize([]float32{0.00, 0.97, 0.01, 0.01, 0.02, 0.00})

	result := RerankByQuery(items, embeddings, queryEmb)

	// Top 4 should be tech items
	top4 := idSet(result[:4])
	for _, want := range []string{"tech1", "tech2", "tech3", "tech4"} {
		if !top4[want] {
			t.Errorf("expected tech item %s in top 4 for tech query, got: %v", want, idsOf(result[:4]))
		}
	}

	// Weather and pure sports should be bottom
	lastID := result[len(result)-1].ID
	if lastID != "wx1" {
		// Weather is orthogonal to tech, so it should be dead last or near last
		// Allow some flexibility — just ensure it's not in top half
		wxRank := rankOf(result, "wx1")
		if wxRank < len(result)/2 {
			t.Errorf("weather item should not be in top half for tech query (rank %d/%d)", wxRank+1, len(result))
		}
	}
}

func TestSearchQuality_FinanceQuery(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Query: "stock market interest rates" → finance dimension
	queryEmb := normalize([]float32{0.00, 0.02, 0.97, 0.02, 0.00, 0.00})

	result := RerankByQuery(items, embeddings, queryEmb)

	// Top 3 should be finance items
	top3 := idSet(result[:3])
	for _, want := range []string{"fin1", "fin2", "fin3"} {
		if !top3[want] {
			t.Errorf("expected finance item %s in top 3 for finance query, got: %v", want, idsOf(result[:3]))
		}
	}

	// Sports items should not be in top 5
	top5 := idSet(result[:5])
	for _, bad := range []string{"nfl1", "nfl2", "nfl3", "nba1"} {
		if top5[bad] {
			t.Errorf("sports item %s should not appear in top 5 for finance query", bad)
		}
	}
}

func TestSearchQuality_CrossDomainQuery(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Query: "AI technology stocks" → mix of tech + finance
	queryEmb := normalize([]float32{0.00, 0.60, 0.50, 0.01, 0.01, 0.00})

	result := RerankByQuery(items, embeddings, queryEmb)

	// Top results should be tech and finance items, not sports/weather
	top6 := idSet(result[:6])
	techOrFin := 0
	for id := range top6 {
		if id == "tech1" || id == "tech2" || id == "tech3" || id == "tech4" ||
			id == "fin1" || id == "fin2" || id == "fin3" {
			techOrFin++
		}
	}
	if techOrFin < 5 {
		t.Errorf("expected at least 5 tech/finance items in top 6 for cross-domain query, got %d: %v", techOrFin, idsOf(result[:6]))
	}

	// NVIDIA (tech + finance overlap) should rank highly
	nvdaRank := rankOf(result, "fin2")
	if nvdaRank > 4 {
		t.Errorf("NVIDIA (tech+finance overlap) should be in top 5, was rank %d", nvdaRank+1)
	}
}

func TestSearchQuality_IrrelevantItemsRankLow(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Query: "NFL football" → pure sports
	queryEmb := normalize([]float32{0.99, 0.00, 0.00, 0.00, 0.00, 0.00})

	result := RerankByQuery(items, embeddings, queryEmb)

	// All 4 sports items should occupy the top 4 positions.
	sportsIDs := map[string]bool{"nfl1": true, "nfl2": true, "nfl3": true, "nba1": true}
	top4 := result[:4]
	for _, item := range top4 {
		if !sportsIDs[item.ID] {
			t.Errorf("non-sports item %s (%s) should not be in top 4 for NFL query", item.ID, item.Title)
		}
	}

	// No non-sports item should appear above any sports item.
	// This is the key quality assertion: the topic boundary is clean.
	for i := 4; i < len(result); i++ {
		if sportsIDs[result[i].ID] {
			t.Errorf("sports item %s ranked %d — should be in top 4", result[i].ID, i+1)
		}
	}
}

// --- Cross-Encoder Reranking Quality Tests ---

func TestSearchQuality_RerankReordersCorrectly(t *testing.T) {
	items := []store.Item{
		{ID: "nfl1", Title: "NFL Draft 2025: Top Prospects"},
		{ID: "tech1", Title: "GPT-5 Released with Multimodal Reasoning"},
		{ID: "nfl3", Title: "Super Bowl LVIII Highlights"},
		{ID: "wx1", Title: "Severe Thunderstorm Warning Texas"},
		{ID: "nfl2", Title: "Mahomes Leads Chiefs to Victory"},
	}

	// Mock reranker that gives correct relevance scores for "nfl" query
	reranker := &mockReranker{
		available: true,
		scores: []Score{
			{Index: 0, Score: 0.95}, // NFL Draft — very relevant
			{Index: 1, Score: 0.05}, // GPT-5 — not relevant
			{Index: 2, Score: 0.92}, // Super Bowl — very relevant
			{Index: 3, Score: 0.02}, // Weather — not relevant
			{Index: 4, Score: 0.88}, // Mahomes — relevant
		},
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "nfl football", reranker)

	// Top 3 should be all NFL items
	top3 := idSet(result[:3])
	for _, want := range []string{"nfl1", "nfl2", "nfl3"} {
		if !top3[want] {
			t.Errorf("expected NFL item %s in top 3 after reranking, got: %v", want, idsOf(result[:3]))
		}
	}

	// Weather should be last
	if result[len(result)-1].ID != "wx1" {
		t.Errorf("expected weather item last after reranking, got %s", result[len(result)-1].ID)
	}
}

func TestSearchQuality_RerankFixesBadCosineOrder(t *testing.T) {
	// Simulate a case where cosine gives a bad order (the user's "nfl" complaint):
	// Items are pre-sorted by cosine, but cosine put irrelevant items high.
	// The reranker should fix this.
	items := []store.Item{
		{ID: "tech1", Title: "New Programming Framework Released"},  // Cosine thought this was #1
		{ID: "fin1", Title: "Markets Rally on Economic Data"},       // Cosine thought this was #2
		{ID: "nfl1", Title: "NFL Season Preview and Predictions"},   // Cosine ranked this #3
		{ID: "wx1", Title: "Weekend Weather Forecast"},              // Cosine ranked this #4
		{ID: "nfl2", Title: "Patrick Mahomes Contract Extension"},   // Cosine ranked this #5
		{ID: "nfl3", Title: "Super Bowl LVIII Betting Odds"},        // Cosine ranked this #6
	}

	// Reranker correctly identifies NFL items as relevant
	reranker := &mockReranker{
		available: true,
		scores: []Score{
			{Index: 0, Score: 0.08}, // tech → irrelevant
			{Index: 1, Score: 0.05}, // finance → irrelevant
			{Index: 2, Score: 0.94}, // NFL preview → highly relevant
			{Index: 3, Score: 0.02}, // weather → irrelevant
			{Index: 4, Score: 0.91}, // Mahomes → relevant
			{Index: 5, Score: 0.89}, // Super Bowl → relevant
		},
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "nfl", reranker)

	// After reranking, all NFL items should be in top 3
	top3 := idSet(result[:3])
	for _, want := range []string{"nfl1", "nfl2", "nfl3"} {
		if !top3[want] {
			t.Errorf("reranker should have promoted NFL item %s to top 3, got: %v", want, idsOf(result[:3]))
		}
	}

	// Tech and finance should have been demoted
	for _, bad := range []string{"tech1", "fin1", "wx1"} {
		rank := rankOf(result, bad)
		if rank < 3 {
			t.Errorf("irrelevant item %s should be ranked below NFL items after reranking (rank %d)", bad, rank+1)
		}
	}
}

func TestSearchQuality_GracefulDegradationPreservesOrder(t *testing.T) {
	// When reranker fails, items should remain in their input order (cosine order)
	items := []store.Item{
		{ID: "nfl1", Title: "NFL Draft Prospects"},
		{ID: "nfl2", Title: "Mahomes Highlights"},
		{ID: "tech1", Title: "New AI Model"},
	}

	reranker := &mockReranker{
		available: true,
		err:       context.DeadlineExceeded,
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "nfl", reranker)

	// Order should be unchanged (graceful degradation)
	if result[0].ID != "nfl1" || result[1].ID != "nfl2" || result[2].ID != "tech1" {
		t.Errorf("on reranker failure, items should retain original order, got: %v", idsOf(result))
	}
}

// --- Full Pipeline Quality Tests ---

func TestSearchQuality_FullPipeline_SportsQuery(t *testing.T) {
	items, embeddings := qualityCorpus()

	// Step 1: Cosine ranking
	queryEmb := normalize([]float32{0.98, 0.01, 0.00, 0.00, 0.00, 0.00})
	cosineResult := RerankByQuery(items, embeddings, queryEmb)

	// Cosine should get sports items to the top
	cosineTop4 := idSet(cosineResult[:4])
	sportsInTop4 := 0
	for _, id := range []string{"nfl1", "nfl2", "nfl3", "nba1"} {
		if cosineTop4[id] {
			sportsInTop4++
		}
	}
	if sportsInTop4 < 4 {
		t.Errorf("cosine step: expected 4 sports items in top 4, got %d: %v", sportsInTop4, idsOf(cosineResult[:4]))
	}

	// Step 2: Cross-encoder reranking (top 6 candidates from cosine)
	candidates := cosineResult[:6]
	reranker := &mockReranker{
		available: true,
		scores: func() []Score {
			// Score based on NFL relevance specifically (not just "sports")
			s := make([]Score, len(candidates))
			for i, item := range candidates {
				switch {
				case item.ID == "nfl1" || item.ID == "nfl2" || item.ID == "nfl3":
					s[i] = Score{Index: i, Score: 0.9 + float32(i)*0.01} // NFL items score high
				case item.ID == "nba1":
					s[i] = Score{Index: i, Score: 0.4} // Basketball — related but not NFL
				default:
					s[i] = Score{Index: i, Score: 0.1} // Everything else low
				}
			}
			return s
		}(),
	}

	ctx := context.Background()
	rerankResult := RerankByCrossEncoder(ctx, candidates, "nfl football", reranker)

	// After reranking, NFL items should be above NBA
	nbaRank := rankOf(rerankResult, "nba1")
	for _, nflID := range []string{"nfl1", "nfl2", "nfl3"} {
		nflRank := rankOf(rerankResult, nflID)
		if nflRank == -1 {
			continue // item might not be in candidates
		}
		if nflRank > nbaRank && nbaRank != -1 {
			t.Errorf("NFL item %s (rank %d) should be above NBA item (rank %d) for 'nfl' query",
				nflID, nflRank+1, nbaRank+1)
		}
	}
}

func TestSearchQuality_FullPipeline_NoEmbeddingsFallback(t *testing.T) {
	items, _ := qualityCorpus()

	// No embeddings at all — cosine should return items in original order
	emptyEmb := map[string][]float32{}
	queryEmb := normalize([]float32{0.98, 0.01, 0.00, 0.00, 0.00, 0.00})

	result := RerankByQuery(items, emptyEmb, queryEmb)

	// With no embeddings, items stay in input order
	for i, item := range result {
		if item.ID != items[i].ID {
			t.Errorf("with no embeddings, items should maintain original order; position %d: expected %s, got %s",
				i, items[i].ID, item.ID)
		}
	}
}

// --- Helpers ---

func idSet(items []store.Item) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item.ID] = true
	}
	return m
}

func idsOf(items []store.Item) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func rankOf(items []store.Item, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}
