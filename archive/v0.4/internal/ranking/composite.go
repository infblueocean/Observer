package ranking

import (
	"github.com/abelbrown/observer/internal/feeds"
)

// CompositeRanker combines multiple rankers with weights.
// Final score = sum(ranker.Score * weight) / sum(weights)
type CompositeRanker struct {
	name     string
	rankers  []Ranker
	weights  []float64
	strategy CombineStrategy
}

// CombineStrategy defines how to combine multiple ranker scores
type CombineStrategy int

const (
	// StrategyWeightedAverage computes weighted average of all scores
	StrategyWeightedAverage CombineStrategy = iota

	// StrategyMax takes the maximum score from any ranker
	StrategyMax

	// StrategyMin takes the minimum score (all rankers must agree)
	StrategyMin

	// StrategyProduct multiplies all scores (AND-like behavior)
	StrategyProduct
)

// NewComposite creates a composite ranker with weighted average strategy
func NewComposite(name string) *CompositeRanker {
	return &CompositeRanker{
		name:     name,
		strategy: StrategyWeightedAverage,
	}
}

// Add adds a ranker with a weight
func (c *CompositeRanker) Add(ranker Ranker, weight float64) *CompositeRanker {
	c.rankers = append(c.rankers, ranker)
	c.weights = append(c.weights, weight)
	return c
}

// WithStrategy sets the combination strategy
func (c *CompositeRanker) WithStrategy(strategy CombineStrategy) *CompositeRanker {
	c.strategy = strategy
	return c
}

func (c *CompositeRanker) Name() string {
	return c.name
}

func (c *CompositeRanker) Score(item *feeds.Item, ctx *Context) float64 {
	if len(c.rankers) == 0 {
		return 0
	}

	scores := make([]float64, len(c.rankers))
	for i, ranker := range c.rankers {
		scores[i] = ranker.Score(item, ctx)
	}

	switch c.strategy {
	case StrategyMax:
		return c.maxScore(scores)
	case StrategyMin:
		return c.minScore(scores)
	case StrategyProduct:
		return c.productScore(scores)
	default:
		return c.weightedAverage(scores)
	}
}

func (c *CompositeRanker) weightedAverage(scores []float64) float64 {
	var sum, weightSum float64
	for i, score := range scores {
		sum += score * c.weights[i]
		weightSum += c.weights[i]
	}
	if weightSum == 0 {
		return 0
	}
	return sum / weightSum
}

func (c *CompositeRanker) maxScore(scores []float64) float64 {
	max := scores[0]
	for _, s := range scores[1:] {
		if s > max {
			max = s
		}
	}
	return max
}

func (c *CompositeRanker) minScore(scores []float64) float64 {
	min := scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
	}
	return min
}

func (c *CompositeRanker) productScore(scores []float64) float64 {
	product := 1.0
	for _, s := range scores {
		product *= s
	}
	return product
}

// RankerSet manages a collection of named rankers that can be switched
type RankerSet struct {
	rankers map[string]Ranker
	active  string
}

// NewRankerSet creates an empty ranker set
func NewRankerSet() *RankerSet {
	return &RankerSet{
		rankers: make(map[string]Ranker),
	}
}

// Register adds a ranker to the set
func (s *RankerSet) Register(ranker Ranker) {
	s.rankers[ranker.Name()] = ranker
	if s.active == "" {
		s.active = ranker.Name()
	}
}

// SetActive switches to a different ranker
func (s *RankerSet) SetActive(name string) bool {
	if _, ok := s.rankers[name]; ok {
		s.active = name
		return true
	}
	return false
}

// Active returns the currently active ranker
func (s *RankerSet) Active() Ranker {
	return s.rankers[s.active]
}

// Names returns all registered ranker names
func (s *RankerSet) Names() []string {
	names := make([]string, 0, len(s.rankers))
	for name := range s.rankers {
		names = append(names, name)
	}
	return names
}

// Get returns a ranker by name
func (s *RankerSet) Get(name string) (Ranker, bool) {
	r, ok := s.rankers[name]
	return r, ok
}
