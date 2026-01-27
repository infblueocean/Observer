package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the persistent application configuration
type Config struct {
	// AI Models
	Models ModelConfig `json:"models"`

	// MCP Servers
	MCPServers []MCPServerConfig `json:"mcp_servers"`

	// UI Preferences
	UI UIConfig `json:"ui"`

	// AI Analysis settings
	Analysis AnalysisConfig `json:"brain_trust"` // JSON key kept for backwards compatibility

	// Filters are stored separately but referenced here
	FiltersFile string `json:"filters_file"`
}

// ModelConfig holds AI model settings
type ModelConfig struct {
	Claude  ModelSettings `json:"claude"`
	OpenAI  ModelSettings `json:"openai"`
	Gemini  ModelSettings `json:"gemini"`
	Grok    ModelSettings `json:"grok"`
	Ollama  ModelSettings `json:"ollama"`
}

// ModelSettings for a single AI provider
type ModelSettings struct {
	Enabled  bool   `json:"enabled"`
	APIKey   string `json:"api_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"` // For Ollama or custom endpoints
	Model    string `json:"model,omitempty"`    // Specific model to use
	Priority int    `json:"priority"`           // Lower = higher priority for fallback
}

// MCPServerConfig for Model Context Protocol servers
type MCPServerConfig struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`             // e.g., "npx"
	Args      []string          `json:"args"`                // e.g., ["-y", "@modelcontextprotocol/server-filesystem"]
	Env       map[string]string `json:"env,omitempty"`       // Environment variables
	Enabled   bool              `json:"enabled"`
}

// UIConfig holds UI preferences
type UIConfig struct {
	Theme           string `json:"theme"`
	ShowSourcePanel bool   `json:"show_source_panel"`
	ItemLimit       int    `json:"item_limit"`
	DensityMode     string `json:"density_mode"` // "comfortable" or "compact"
}

// AnalysisConfig holds AI analysis preferences
type AnalysisConfig struct {
	Enabled          bool   `json:"enabled"`
	AutoAnalyze      bool   `json:"auto_analyze"`       // Auto-analyze on dwell
	DwellTimeMs      int    `json:"dwell_time_ms"`      // Milliseconds before auto-analyze
	PreferLocal      bool   `json:"prefer_local"`       // Prefer local models (Ollama) for speed
	LocalForQuickOps bool   `json:"local_for_quick_ops"` // Use local for quick ops (top stories, etc)
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Models: ModelConfig{
			Claude: ModelSettings{
				Enabled:  true,
				Priority: 1,
				Model:    "claude-sonnet-4-5-20250929",
			},
			OpenAI: ModelSettings{
				Enabled:  false,
				Priority: 2,
				Model:    "gpt-5.2",
			},
			Gemini: ModelSettings{
				Enabled:  false,
				Priority: 3,
				Model:    "gemini-3-flash-preview",
			},
			Grok: ModelSettings{
				Enabled:  false,
				Priority: 4,
				Model:    "grok-4-1-fast-non-reasoning",
			},
			Ollama: ModelSettings{
				Enabled:  false,
				Priority: 5,
				Endpoint: "http://localhost:11434",
				// Model auto-detected from Ollama if not specified
			},
		},
		MCPServers: []MCPServerConfig{},
		UI: UIConfig{
			Theme:           "dark",
			ShowSourcePanel: false,
			ItemLimit:       500,
			DensityMode:     "comfortable",
		},
		Analysis: AnalysisConfig{
			Enabled:          true,
			AutoAnalyze:      false, // Manual trigger by default
			DwellTimeMs:      1000,  // 1 second dwell time if auto-analyze enabled
			PreferLocal:      true,  // Prefer local models for speed
			LocalForQuickOps: true,  // Use local for quick operations like top stories
		},
	}
}

// ConfigPath returns the path to the config file
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".observer", "config.json")
}

// Load reads config from disk, or returns defaults
func Load() (*Config, error) {
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults and try to auto-populate from environment
			cfg := DefaultConfig()
			cfg.AutoPopulateFromEnv()
			return cfg, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), nil
	}

	return &cfg, nil
}

// Save writes config to disk
func (c *Config) Save() error {
	path := ConfigPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600) // Restrictive permissions for API keys
}

// AutoPopulateFromEnv fills in API keys from environment variables
func (c *Config) AutoPopulateFromEnv() {
	if key := os.Getenv("CLAUDE_API_KEY"); key != "" {
		c.Models.Claude.APIKey = key
		c.Models.Claude.Enabled = true
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		c.Models.Claude.APIKey = key
		c.Models.Claude.Enabled = true
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		c.Models.OpenAI.APIKey = key
	}
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
		c.Models.Gemini.APIKey = key
	}
	if key := os.Getenv("XAI_API_KEY"); key != "" {
		c.Models.Grok.APIKey = key
	}
}

// LoadKeysFromFile loads keys from a shell script (like keys.sh)
func (c *Config) LoadKeysFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Simple parser for export KEY=value lines
	lines := string(data)
	for _, line := range splitLines(lines) {
		if len(line) < 8 {
			continue
		}
		if line[:7] == "export " {
			line = line[7:]
		}
		parts := splitFirst(line, '=')
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]

		switch key {
		case "CLAUDE_API_KEY", "ANTHROPIC_API_KEY":
			c.Models.Claude.APIKey = value
			c.Models.Claude.Enabled = true
		case "OPENAI_API_KEY":
			c.Models.OpenAI.APIKey = value
		case "GOOGLE_API_KEY":
			c.Models.Gemini.APIKey = value
		case "XAI_API_KEY":
			c.Models.Grok.APIKey = value
		}
	}

	return nil
}

// GetEnabledModels returns models that are enabled and have API keys
func (c *Config) GetEnabledModels() []string {
	var models []string
	if c.Models.Claude.Enabled && c.Models.Claude.APIKey != "" {
		models = append(models, "claude")
	}
	if c.Models.OpenAI.Enabled && c.Models.OpenAI.APIKey != "" {
		models = append(models, "openai")
	}
	if c.Models.Gemini.Enabled && c.Models.Gemini.APIKey != "" {
		models = append(models, "gemini")
	}
	if c.Models.Grok.Enabled && c.Models.Grok.APIKey != "" {
		models = append(models, "grok")
	}
	if c.Models.Ollama.Enabled {
		models = append(models, "ollama")
	}
	return models
}

// Helpers

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFirst(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
