package curation

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
)

// SourceMode determines how a source is used
type SourceMode string

const (
	ModeLive   SourceMode = "live"   // Always shown
	ModeSample SourceMode = "sample" // Probabilistic (uses Exposure)
	ModeAuto   SourceMode = "auto"   // AI-only (not in stream, but queryable)
	ModeOff    SourceMode = "off"    // Disabled
)

// SourceConfig holds user configuration for a source
type SourceConfig struct {
	Name     string     `json:"name"`
	Mode     SourceMode `json:"mode"`
	Exposure float64    `json:"exposure"` // 0.0-1.0 for Sample mode
}

// SourceManager manages source preferences
type SourceManager struct {
	mu      sync.RWMutex
	configs map[string]*SourceConfig
	path    string
}

// NewSourceManager creates a source manager
func NewSourceManager(configDir string) *SourceManager {
	sm := &SourceManager{
		configs: make(map[string]*SourceConfig),
		path:    filepath.Join(configDir, "sources.json"),
	}
	sm.load()
	return sm
}

// Get returns config for a source (defaults to Live/1.0 if not configured)
func (sm *SourceManager) Get(name string) *SourceConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if cfg, ok := sm.configs[name]; ok {
		return cfg
	}
	return &SourceConfig{Name: name, Mode: ModeLive, Exposure: 1.0}
}

// Set updates a source's config
func (sm *SourceManager) Set(name string, mode SourceMode, exposure float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.configs[name] = &SourceConfig{
		Name:     name,
		Mode:     mode,
		Exposure: clamp(exposure, 0, 1),
	}
	sm.save()
}

// ShouldFetch returns true if source should be fetched
func (sm *SourceManager) ShouldFetch(name string) bool {
	cfg := sm.Get(name)
	return cfg.Mode == ModeLive || cfg.Mode == ModeSample
}

// ShouldShow returns true if item should appear in stream (probabilistic for Sample)
func (sm *SourceManager) ShouldShow(name string) bool {
	cfg := sm.Get(name)
	switch cfg.Mode {
	case ModeLive:
		return true
	case ModeSample:
		return rand.Float64() < cfg.Exposure
	default:
		return false
	}
}

// CanAIAccess returns true if AI/personas can query this source
func (sm *SourceManager) CanAIAccess(name string) bool {
	cfg := sm.Get(name)
	return cfg.Mode != ModeOff
}

// GetByMode returns all sources with a given mode
func (sm *SourceManager) GetByMode(mode SourceMode) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []string
	for name, cfg := range sm.configs {
		if cfg.Mode == mode {
			result = append(result, name)
		}
	}
	return result
}

// Stats returns mode counts
func (sm *SourceManager) Stats() (live, sample, auto, off int) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, cfg := range sm.configs {
		switch cfg.Mode {
		case ModeLive:
			live++
		case ModeSample:
			sample++
		case ModeAuto:
			auto++
		case ModeOff:
			off++
		}
	}
	return
}

func (sm *SourceManager) load() {
	data, err := os.ReadFile(sm.path)
	if err != nil {
		return
	}
	var configs []*SourceConfig
	if json.Unmarshal(data, &configs) == nil {
		for _, cfg := range configs {
			sm.configs[cfg.Name] = cfg
		}
	}
}

func (sm *SourceManager) save() {
	configs := make([]*SourceConfig, 0, len(sm.configs))
	for _, cfg := range sm.configs {
		configs = append(configs, cfg)
	}
	if data, err := json.MarshalIndent(configs, "", "  "); err == nil {
		os.MkdirAll(filepath.Dir(sm.path), 0755)
		os.WriteFile(sm.path, data, 0644)
	}
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
