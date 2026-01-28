package ranking

import (
	"math"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// FreshnessRanker scores items by recency.
// Newer items score higher, with exponential decay.
type FreshnessRanker struct {
	// HalfLife is how long until an item's freshness score drops to 0.5
	// Default: 1 hour
	HalfLife time.Duration
}

func NewFreshnessRanker() *FreshnessRanker {
	return &FreshnessRanker{HalfLife: time.Hour}
}

func (r *FreshnessRanker) Name() string { return "freshness" }

func (r *FreshnessRanker) Score(item *feeds.Item, ctx *Context) float64 {
	age := ctx.Now.Sub(item.Published)
	if age < 0 {
		age = 0 // Future-dated items treated as brand new
	}

	// Exponential decay: score = 0.5^(age/halfLife)
	halfLives := float64(age) / float64(r.HalfLife)
	return math.Pow(0.5, halfLives)
}

// SourceWeightRanker scores items by their source's weight.
// Higher-weight sources (e.g., wire services) score higher.
type SourceWeightRanker struct {
	// DefaultWeight for sources not in the weights map
	DefaultWeight float64
}

func NewSourceWeightRanker() *SourceWeightRanker {
	return &SourceWeightRanker{DefaultWeight: 1.0}
}

func (r *SourceWeightRanker) Name() string { return "source_weight" }

func (r *SourceWeightRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if ctx.SourceWeights == nil {
		return r.DefaultWeight
	}
	if weight, ok := ctx.SourceWeights[item.SourceName]; ok {
		// Normalize to [0,1] assuming weights are 0-2 range
		return math.Min(weight/2.0, 1.0)
	}
	return r.DefaultWeight / 2.0
}

// DiversityRanker penalizes items from over-represented sources.
// Helps ensure variety in the feed.
type DiversityRanker struct {
	// MaxPerSource is how many items from one source before penalty kicks in
	MaxPerSource int
}

func NewDiversityRanker() *DiversityRanker {
	return &DiversityRanker{MaxPerSource: 3}
}

func (r *DiversityRanker) Name() string { return "diversity" }

func (r *DiversityRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if ctx.SourceCounts == nil {
		return 1.0
	}

	count := ctx.SourceCounts[item.SourceName]
	if count < r.MaxPerSource {
		return 1.0
	}

	// Exponential penalty for over-representation
	excess := count - r.MaxPerSource
	return math.Pow(0.7, float64(excess))
}

// ClusterRanker boosts items that are part of story clusters.
// Stories covered by multiple sources are likely more important.
type ClusterRanker struct {
	// MinClusterSize to trigger a boost
	MinClusterSize int
}

func NewClusterRanker() *ClusterRanker {
	return &ClusterRanker{MinClusterSize: 2}
}

func (r *ClusterRanker) Name() string { return "cluster" }

func (r *ClusterRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if ctx.Correlation == nil {
		return 0.5 // Neutral score when no correlation data
	}

	cluster := ctx.Correlation.GetClusterInfo(item.ID)
	if cluster == nil {
		return 0.5
	}

	// Boost based on cluster size (log scale to avoid runaway scores)
	size := cluster.Size
	if size < r.MinClusterSize {
		return 0.5
	}

	// Score: 0.5 + 0.5 * (1 - 1/size)
	// Size 2 -> 0.75, Size 5 -> 0.9, Size 10 -> 0.95
	return 0.5 + 0.5*(1.0-1.0/float64(size))
}

// EntityRanker boosts items with recognized entities.
// Items mentioning known entities (companies, people) are likely more substantive.
type EntityRanker struct{}

func NewEntityRanker() *EntityRanker { return &EntityRanker{} }

func (r *EntityRanker) Name() string { return "entity" }

func (r *EntityRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if ctx.Correlation == nil {
		return 0.5
	}

	entities := ctx.Correlation.GetItemEntities(item.ID)
	if len(entities) == 0 {
		return 0.5
	}

	// More entities = higher score (diminishing returns)
	// 1 entity -> 0.6, 3 entities -> 0.8, 5+ entities -> 0.9
	score := 0.5 + 0.4*(1.0-1.0/float64(len(entities)+1))
	return math.Min(score, 1.0)
}

// BreakingNewsRanker boosts items that look like breaking news.
type BreakingNewsRanker struct {
	// Keywords that indicate breaking news
	Keywords []string
	// MaxAge for breaking news consideration
	MaxAge time.Duration
}

func NewBreakingNewsRanker() *BreakingNewsRanker {
	return &BreakingNewsRanker{
		Keywords: []string{"BREAKING", "URGENT", "DEVELOPING", "JUST IN"},
		MaxAge:   30 * time.Minute,
	}
}

func (r *BreakingNewsRanker) Name() string { return "breaking" }

func (r *BreakingNewsRanker) Score(item *feeds.Item, ctx *Context) float64 {
	age := ctx.Now.Sub(item.Published)
	if age > r.MaxAge {
		return 0.5 // Too old to be breaking
	}

	title := strings.ToUpper(item.Title)
	for _, keyword := range r.Keywords {
		if strings.Contains(title, keyword) {
			// Breaking news in the last 30 min gets a big boost
			freshness := 1.0 - float64(age)/float64(r.MaxAge)
			return 0.7 + 0.3*freshness
		}
	}

	return 0.5 // Neutral
}

// ReadStateRanker penalizes already-read items.
type ReadStateRanker struct {
	// ReadPenalty is how much to reduce score for read items (0-1)
	ReadPenalty float64
}

func NewReadStateRanker() *ReadStateRanker {
	return &ReadStateRanker{ReadPenalty: 0.3}
}

func (r *ReadStateRanker) Name() string { return "read_state" }

func (r *ReadStateRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if item.Read {
		return 1.0 - r.ReadPenalty
	}
	return 1.0
}

// ConstantRanker always returns the same score (useful for testing/baseline)
type ConstantRanker struct {
	score float64
}

func NewConstantRanker(score float64) *ConstantRanker {
	return &ConstantRanker{score: score}
}

func (r *ConstantRanker) Name() string { return "constant" }

func (r *ConstantRanker) Score(item *feeds.Item, ctx *Context) float64 {
	return r.score
}

// DefaultRanker returns a sensible default composite ranker
func DefaultRanker() Ranker {
	return NewComposite("default").
		Add(NewFreshnessRanker(), 3.0).    // Freshness is most important
		Add(NewSourceWeightRanker(), 1.5). // Source quality matters
		Add(NewDiversityRanker(), 1.0).    // Variety is good
		Add(NewBreakingNewsRanker(), 2.0). // Breaking news gets priority
		Add(NewReadStateRanker(), 0.5)     // Slight penalty for read items
}
