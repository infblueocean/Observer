package ranking

import (
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

func TestFreshnessRanker(t *testing.T) {
	ranker := NewFreshnessRanker()
	ctx := NewContext()

	// Item just now
	now := feeds.Item{Published: ctx.Now}
	// Item 1 hour ago
	hourAgo := feeds.Item{Published: ctx.Now.Add(-time.Hour)}
	// Item 2 hours ago
	twoHoursAgo := feeds.Item{Published: ctx.Now.Add(-2 * time.Hour)}

	scoreNow := ranker.Score(&now, ctx)
	scoreHour := ranker.Score(&hourAgo, ctx)
	scoreTwoHours := ranker.Score(&twoHoursAgo, ctx)

	// Newer items should score higher
	if scoreNow <= scoreHour {
		t.Errorf("now (%f) should score higher than hourAgo (%f)", scoreNow, scoreHour)
	}
	if scoreHour <= scoreTwoHours {
		t.Errorf("hourAgo (%f) should score higher than twoHoursAgo (%f)", scoreHour, scoreTwoHours)
	}

	// Score at halfLife should be ~0.5
	if scoreHour < 0.45 || scoreHour > 0.55 {
		t.Errorf("score at halfLife should be ~0.5, got %f", scoreHour)
	}
}

func TestCompositeRanker(t *testing.T) {
	composite := NewComposite("test").
		Add(NewConstantRanker(0.8), 1.0).
		Add(NewConstantRanker(0.4), 1.0)

	ctx := NewContext()
	item := feeds.Item{}

	score := composite.Score(&item, ctx)
	// Expected: (0.8 + 0.4) / 2 = 0.6

	if score < 0.59 || score > 0.61 {
		t.Errorf("expected weighted average ~0.6, got %f", score)
	}
}

func TestCompositeRankerWeighted(t *testing.T) {
	// Weight the first ranker 3x more than second
	composite := NewComposite("test").
		Add(NewConstantRanker(1.0), 3.0).
		Add(NewConstantRanker(0.0), 1.0)

	ctx := NewContext()
	item := feeds.Item{}

	score := composite.Score(&item, ctx)
	// Expected: (1.0*3 + 0.0*1) / 4 = 0.75

	if score < 0.74 || score > 0.76 {
		t.Errorf("expected weighted average ~0.75, got %f", score)
	}
}

func TestRank(t *testing.T) {
	items := []feeds.Item{
		{ID: "old", Published: time.Now().Add(-24 * time.Hour)},
		{ID: "new", Published: time.Now()},
		{ID: "mid", Published: time.Now().Add(-1 * time.Hour)},
	}

	ranker := NewFreshnessRanker()
	ctx := NewContext()

	results := Rank(items, ranker, ctx)

	// Should be sorted newest first
	if results[0].Item.ID != "new" {
		t.Errorf("expected 'new' first, got '%s'", results[0].Item.ID)
	}
	if results[1].Item.ID != "mid" {
		t.Errorf("expected 'mid' second, got '%s'", results[1].Item.ID)
	}
	if results[2].Item.ID != "old" {
		t.Errorf("expected 'old' last, got '%s'", results[2].Item.ID)
	}
}

func TestTopN(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", Published: time.Now().Add(-4 * time.Hour)},
		{ID: "2", Published: time.Now().Add(-3 * time.Hour)},
		{ID: "3", Published: time.Now().Add(-2 * time.Hour)},
		{ID: "4", Published: time.Now().Add(-1 * time.Hour)},
		{ID: "5", Published: time.Now()},
	}

	ranker := NewFreshnessRanker()
	ctx := NewContext()

	top := TopN(items, 3, ranker, ctx)

	if len(top) != 3 {
		t.Fatalf("expected 3 items, got %d", len(top))
	}

	// Should be newest 3
	if top[0].ID != "5" || top[1].ID != "4" || top[2].ID != "3" {
		t.Errorf("expected [5,4,3], got [%s,%s,%s]", top[0].ID, top[1].ID, top[2].ID)
	}
}

func TestDiversityRanker(t *testing.T) {
	ranker := NewDiversityRanker()
	ranker.MaxPerSource = 2

	item := feeds.Item{SourceName: "CNN"}

	// No prior selections - full score
	ctx := NewContext()
	ctx.SourceCounts = map[string]int{}
	if score := ranker.Score(&item, ctx); score != 1.0 {
		t.Errorf("expected 1.0 with no prior selections, got %f", score)
	}

	// 2 prior selections - at limit, still full
	ctx.SourceCounts["CNN"] = 2
	if score := ranker.Score(&item, ctx); score != 1.0 {
		t.Errorf("expected 1.0 at limit, got %f", score)
	}

	// 3 prior selections - over limit, penalized
	ctx.SourceCounts["CNN"] = 3
	score := ranker.Score(&item, ctx)
	if score >= 1.0 {
		t.Errorf("expected penalty for over-representation, got %f", score)
	}
}

func TestRankerSet(t *testing.T) {
	set := NewRankerSet()

	r1 := NewFreshnessRanker()
	r2 := NewDiversityRanker()

	set.Register(r1)
	set.Register(r2)

	// First registered should be active
	if set.Active().Name() != "freshness" {
		t.Errorf("expected 'freshness' active, got '%s'", set.Active().Name())
	}

	// Switch active
	set.SetActive("diversity")
	if set.Active().Name() != "diversity" {
		t.Errorf("expected 'diversity' active, got '%s'", set.Active().Name())
	}

	// List names
	names := set.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 rankers, got %d", len(names))
	}
}
