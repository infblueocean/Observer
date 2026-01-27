# v0.2 Architecture: Clean Slate

> **Principle:** If it's not earning its complexity, delete it.

## What We're Gutting

| Component | v0.1 LOC | v0.2 LOC | Reason |
|-----------|----------|----------|--------|
| 5 separate providers | 1,644 | 350 | One generic + config |
| 5 polling analyze methods | 203 | 0 | Channels, not polling |
| Thread scaffolding | 200 | 0 | Never used |
| BrainTrust alias | 50 | 0 | Just use Analyzer |
| Top stories state machine | 400 | 150 | Simpler lifecycle |
| Repeated panel sizing | 100 | 30 | One helper |

**Total reduction: ~2,000 LOC**

---

## New Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         app.go                               │
│                    (~800 LOC, down from 1,873)               │
│                                                             │
│  Model {                                                    │
│      stream      *stream.Model     // UI                    │
│      analyzer    *brain.Analyzer   // AI (unified)          │
│      correlator  *correlation.Engine // Stories             │
│      store       *store.Store      // Persistence           │
│  }                                                          │
│                                                             │
│  Update() → switch on msg type → return cmd                 │
│  View() → compose panels                                    │
└─────────────────────────────────────────────────────────────┘
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
    ┌──────────┐   ┌──────────────┐   ┌──────────────┐
    │  brain/  │   │ correlation/ │   │    store/    │
    │  (~400)  │   │   (~800)     │   │   (~300)     │
    └──────────┘   └──────────────┘   └──────────────┘
```

---

## brain/ Rewrite

### Before: 5 Files, 1,644 LOC

```
brain/
├── claude.go   (316)  ─┐
├── openai.go   (285)   │ 90% identical
├── gemini.go   (323)   │ boilerplate
├── grok.go     (289)   │
├── ollama.go   (431)  ─┘
└── trust.go    (varies)
```

### After: 3 Files, ~400 LOC

```
brain/
├── provider.go   (~200)  # Generic HTTP provider
├── config.go     (~100)  # Provider configs (endpoints, formats)
└── analyzer.go   (~100)  # Analysis orchestration
```

### provider.go - One Provider to Rule Them All

```go
package brain

import (
    "context"
    "encoding/json"
    "net/http"
)

// ProviderConfig defines how to talk to an LLM API
type ProviderConfig struct {
    Name        string
    Endpoint    string
    APIKeyEnv   string            // e.g., "ANTHROPIC_API_KEY"
    Model       string
    Headers     map[string]string // Static headers
    BuildRequest func(prompt string, model string) any
    ParseResponse func(body []byte) (string, error)
    StreamParse   func(line []byte) (string, bool) // token, done
}

// Provider is a generic LLM client
type Provider struct {
    config  ProviderConfig
    apiKey  string
    client  *http.Client
}

func NewProvider(cfg ProviderConfig) *Provider {
    return &Provider{
        config: cfg,
        apiKey: os.Getenv(cfg.APIKeyEnv),
        client: &http.Client{Timeout: 120 * time.Second},
    }
}

func (p *Provider) Name() string { return p.config.Name }

func (p *Provider) Available() bool {
    return p.apiKey != "" || p.config.APIKeyEnv == "" // Ollama needs no key
}

func (p *Provider) Generate(ctx context.Context, prompt string) (string, error) {
    req := p.config.BuildRequest(prompt, p.config.Model)
    body, _ := json.Marshal(req)

    httpReq, _ := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint, bytes.NewReader(body))
    httpReq.Header.Set("Content-Type", "application/json")
    for k, v := range p.config.Headers {
        httpReq.Header.Set(k, v)
    }
    if p.apiKey != "" {
        httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
    }

    resp, err := p.client.Do(httpReq)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    return p.config.ParseResponse(respBody)
}

func (p *Provider) Stream(ctx context.Context, prompt string) <-chan StreamChunk {
    ch := make(chan StreamChunk, 100)
    go func() {
        defer close(ch)
        // SSE streaming logic using p.config.StreamParse
    }()
    return ch
}
```

### config.go - Provider Definitions

```go
package brain

var Claude = ProviderConfig{
    Name:      "claude",
    Endpoint:  "https://api.anthropic.com/v1/messages",
    APIKeyEnv: "ANTHROPIC_API_KEY",
    Model:     "claude-sonnet-4-20250514",
    Headers: map[string]string{
        "anthropic-version": "2023-06-01",
        "x-api-key":         "", // Filled from apiKey
    },
    BuildRequest: func(prompt, model string) any {
        return map[string]any{
            "model":      model,
            "max_tokens": 4096,
            "messages":   []map[string]string{{"role": "user", "content": prompt}},
        }
    },
    ParseResponse: func(body []byte) (string, error) {
        var resp struct {
            Content []struct{ Text string } `json:"content"`
        }
        json.Unmarshal(body, &resp)
        if len(resp.Content) > 0 {
            return resp.Content[0].Text, nil
        }
        return "", errors.New("empty response")
    },
}

var OpenAI = ProviderConfig{
    Name:      "openai",
    Endpoint:  "https://api.openai.com/v1/chat/completions",
    APIKeyEnv: "OPENAI_API_KEY",
    Model:     "gpt-4o",
    BuildRequest: func(prompt, model string) any {
        return map[string]any{
            "model":    model,
            "messages": []map[string]string{{"role": "user", "content": prompt}},
        }
    },
    ParseResponse: func(body []byte) (string, error) {
        var resp struct {
            Choices []struct {
                Message struct{ Content string } `json:"message"`
            } `json:"choices"`
        }
        json.Unmarshal(body, &resp)
        if len(resp.Choices) > 0 {
            return resp.Choices[0].Message.Content, nil
        }
        return "", errors.New("empty response")
    },
}

// Similar for Gemini, Grok, Ollama...

var AllProviders = []ProviderConfig{Claude, OpenAI, Gemini, Grok, Ollama}
```

### analyzer.go - Simple Orchestration

```go
package brain

type Analyzer struct {
    providers []*Provider
    results   chan AnalysisResult  // Push results, don't poll
}

func NewAnalyzer() *Analyzer {
    a := &Analyzer{
        results: make(chan AnalysisResult, 100),
    }

    // Auto-initialize available providers
    for _, cfg := range AllProviders {
        p := NewProvider(cfg)
        if p.Available() {
            a.providers = append(a.providers, p)
        }
    }

    return a
}

// Results returns channel for completed analyses (Bubble Tea subscribes)
func (a *Analyzer) Results() <-chan AnalysisResult {
    return a.results
}

// Analyze kicks off async analysis, result comes via Results() channel
func (a *Analyzer) Analyze(ctx context.Context, item feeds.Item, prompt string) {
    go func() {
        provider := a.randomProvider()

        start := time.Now()
        text, err := provider.Generate(ctx, prompt)

        a.results <- AnalysisResult{
            ItemID:   item.ID,
            Provider: provider.Name(),
            Content:  text,
            Error:    err,
            Duration: time.Since(start),
        }
    }()
}

// AnalyzeStream for streaming responses
func (a *Analyzer) AnalyzeStream(ctx context.Context, item feeds.Item, prompt string) {
    go func() {
        provider := a.randomStreamingProvider()

        a.results <- AnalysisResult{
            ItemID:   item.ID,
            Provider: provider.Name(),
            Started:  true,
        }

        for chunk := range provider.Stream(ctx, prompt) {
            a.results <- AnalysisResult{
                ItemID:  item.ID,
                Chunk:   chunk.Text,
                TokenN:  chunk.TokenCount,
            }
        }

        a.results <- AnalysisResult{
            ItemID:    item.ID,
            Completed: true,
        }
    }()
}

func (a *Analyzer) randomProvider() *Provider {
    return a.providers[rand.Intn(len(a.providers))]
}
```

---

## app.go Simplification

### Before: Polling Loops (203 LOC)

```go
// 5 methods that all look like this:
func (m *Model) analyzeBrainTrustXxx(item feeds.Item) tea.Cmd {
    return func() tea.Msg {
        m.brainTrust.AnalyzeXxx(...)

        // POLL POLL POLL
        ticker := time.NewTicker(300 * time.Millisecond)
        for {
            select {
            case <-ticker.C:
                if done := m.brainTrust.GetAnalysis(item.ID); done != nil {
                    return BrainTrustAnalysisMsg{...}
                }
            case <-timeout:
                return BrainTrustAnalysisMsg{Error: ...}
            }
        }
    }
}
```

### After: Channel Subscription (20 LOC)

```go
// One subscription, handles all analysis events
func (m *Model) subscribeAnalyzer() tea.Cmd {
    return func() tea.Msg {
        result := <-m.analyzer.Results()
        return AnalysisResultMsg(result)
    }
}

// In Update():
case AnalysisResultMsg:
    if msg.Started {
        m.analysisPanel.StartStreaming(msg.Provider)
    } else if msg.Chunk != "" {
        m.analysisPanel.AppendChunk(msg.Chunk)
    } else if msg.Completed {
        m.analysisPanel.Complete()
    } else {
        m.analysisPanel.SetContent(msg.Content)
    }
    return m, m.subscribeAnalyzer() // Re-subscribe
```

**Savings: 183 LOC deleted, replaced with 20 LOC**

---

## correlation/ Cleanup

### Delete Entirely

```go
// These never worked:
func (e *Engine) storeCorrelations(...) error { return nil }    // DELETE
func (e *Engine) getActiveThreads() ([]Thread, error) { ... }   // DELETE
func (e *Engine) addItemToThread(...) error { return nil }      // DELETE

type Thread struct { ... }           // DELETE
type AICorrelator interface { ... }  // DELETE
```

### Keep and Enhance

```go
// These work and are needed:
- SimHash() / AreDuplicates()           // Dedup
- ExtractTickers() / ExtractCountries() // Entities
- ProcessItem()                          // Pipeline entry
```

### New Clean Structure

```
correlation/
├── engine.go      (~300)  # Pipeline coordinator
├── dedup.go       (~100)  # SimHash only
├── entities.go    (~150)  # Extraction only
├── clusters.go    (~200)  # New: actual clustering
└── velocity.go    (~100)  # New: spike detection
```

---

## Top Stories Simplification

### Before: Complex State Machine

```
NEW → EMERGING → ONGOING → MAJOR → SUSTAINED → FADING
      (6 states, complex transition rules, hit/miss counting)
```

### After: Simple Confidence Score

```go
type TopStory struct {
    Item       *feeds.Item
    Confidence float64  // 0.0 - 1.0
    FirstSeen  time.Time
    LastSeen   time.Time
    Sources    int      // How many sources reporting
}

// Confidence = f(sources, freshness, velocity)
func (s *TopStory) UpdateConfidence() {
    age := time.Since(s.FirstSeen).Hours()
    freshness := math.Max(0, 1 - age/24)  // Decays over 24h

    s.Confidence = (float64(s.Sources) / 10) * freshness
}

// Display based on confidence:
// > 0.8: ★ MAJOR (bold)
// > 0.5: ◉ TOP
// > 0.3: ◐ RISING
// else:  ○ (dimmed)
```

**Savings: 250 LOC → 50 LOC**

---

## Message Types (Final)

```go
// messages.go - Only what we need

type ItemsLoadedMsg struct {
    Items []feeds.Item
}

type RefreshMsg struct{}

type AnalysisResultMsg struct {
    ItemID    string
    Provider  string
    Content   string
    Chunk     string    // For streaming
    Started   bool
    Completed bool
    Error     error
}

type CorrelationMsg struct {
    Type      string    // "duplicate", "cluster", "entity", "velocity"
    ItemID    string
    Data      any
}

type TickMsg time.Time
```

**That's it. 6 message types instead of 10.**

---

## File Structure: Before vs After

### Before (v0.1)

```
internal/
├── app/
│   ├── app.go           (1,873 LOC)
│   └── messages.go      (68 LOC)
├── brain/
│   ├── claude.go        (316 LOC)
│   ├── openai.go        (285 LOC)
│   ├── gemini.go        (323 LOC)
│   ├── grok.go          (289 LOC)
│   ├── ollama.go        (431 LOC)
│   └── trust.go         (varies)
├── correlation/
│   ├── engine.go        (1,133 LOC)  # Much dead code
│   ├── cheap.go         (219 LOC)
│   └── types.go         (122 LOC)
└── ...

Total brain/: ~1,644 LOC
Total app.go: ~1,873 LOC
```

### After (v0.2)

```
internal/
├── app/
│   ├── app.go           (~800 LOC)   # -1,073
│   └── messages.go      (~40 LOC)    # -28
├── brain/
│   ├── provider.go      (~200 LOC)   # Generic
│   ├── config.go        (~100 LOC)   # All providers
│   └── analyzer.go      (~100 LOC)   # Orchestration
├── correlation/
│   ├── engine.go        (~300 LOC)   # Clean pipeline
│   ├── dedup.go         (~100 LOC)
│   ├── entities.go      (~150 LOC)
│   ├── clusters.go      (~200 LOC)
│   └── velocity.go      (~100 LOC)
└── ...

Total brain/: ~400 LOC (-1,244)
Total app.go: ~800 LOC (-1,073)
```

---

## Migration Path

### Phase 1: Gut brain/ (Day 1)

1. Create new `provider.go` with generic Provider
2. Create `config.go` with all provider configs
3. Create simple `analyzer.go` with channel-based results
4. Delete: claude.go, openai.go, gemini.go, grok.go, ollama.go
5. Update app.go imports

### Phase 2: Simplify app.go (Day 2)

1. Delete all 5 `analyzeBrainTrustXxx` methods
2. Add single `subscribeAnalyzer()` subscription
3. Simplify Update() switch cases
4. Extract panel sizing to one helper

### Phase 3: Clean correlation/ (Day 3)

1. Delete Thread types and methods
2. Delete AICorrelator interface
3. Split engine.go into focused files
4. Wire up actual clustering

### Phase 4: Test & Polish (Day 4)

1. Verify all providers work
2. Verify streaming works
3. Verify correlation pipeline
4. Tag v0.2

---

## Success Criteria

- [ ] `go build` succeeds
- [ ] All 5 providers work (Claude, OpenAI, Gemini, Grok, Ollama)
- [ ] Streaming analysis works
- [ ] Top stories work
- [ ] Duplicate detection works
- [ ] Total LOC reduced by >1,500
- [ ] No polling loops in app.go
- [ ] Single channel subscription pattern throughout

---

## What We're NOT Changing

Keep as-is (working well):
- `feeds/` - RSS parsing, aggregator
- `store/sqlite.go` - Persistence
- `ui/stream/model.go` - Main UI (complex but necessary)
- `config/` - Configuration loading
- Keyboard shortcuts
- Visual design / styles
