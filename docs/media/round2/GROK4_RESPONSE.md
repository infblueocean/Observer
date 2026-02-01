# Grok-4 Response — Round 2: Implementation Details

**Model:** grok4
**Date:** 2026-01-31
**Note:** Response was truncated by the model (564 bytes). Only partial output received.

---

```go
// internal/ui/media/headline.go
package media

import (
	"fmt"
	"math"
	"time"
)

type Headline struct {
	ID         string
	Title      string
	Source     string
	Timestamp  time.Time
	HoursOld   float64
	Semantic   float64
	Rerank     float64
	Arousal    float64
	Negativity float64
	Curiosity  float64
}

type Weights struct {
	ArousalWeight      float64 // 0-100
	NegativityBias     float64 // 0.5-2.0
	CuriosityGapWeight
