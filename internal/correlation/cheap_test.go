package correlation

import (
	"testing"
)

func TestExtractTickers(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"$AAPL is up 5%", []string{"AAPL"}},
		{"$TSLA and $AAPL both rallied", []string{"TSLA", "AAPL"}},
		{"No tickers here", nil},
		{"Check out $BRK.A and $BRK.B", []string{"BRK.A", "BRK.B"}},
		{"$NVDA hits all-time high, $NVDA continues to soar", []string{"NVDA"}}, // Deduped
		{"Price is $100", nil}, // Not a ticker
	}

	for _, tt := range tests {
		result := ExtractTickers(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("ExtractTickers(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, r := range result {
			if r != tt.expected[i] {
				t.Errorf("ExtractTickers(%q)[%d] = %q, want %q", tt.input, i, r, tt.expected[i])
			}
		}
	}
}

func TestExtractCountries(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Russia invades Ukraine", []string{"russia", "ukraine"}},
		{"US and China trade tensions", []string{"united_states", "china"}},
		{"The American president met with Chinese officials", []string{"united_states", "china"}},
		{"No countries mentioned here", nil},
		{"The Kremlin issued a statement", []string{"russia"}},
		{"EU sanctions against Moscow", []string{"european_union", "russia"}},
		{"NATO alliance strengthens", []string{"nato"}},
	}

	for _, tt := range tests {
		result := ExtractCountries(tt.input)
		// Convert to map for easier comparison (order doesn't matter)
		resultMap := make(map[string]bool)
		for _, r := range result {
			resultMap[r] = true
		}
		expectedMap := make(map[string]bool)
		for _, e := range tt.expected {
			expectedMap[e] = true
		}

		if len(resultMap) != len(expectedMap) {
			t.Errorf("ExtractCountries(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for e := range expectedMap {
			if !resultMap[e] {
				t.Errorf("ExtractCountries(%q) missing %q", tt.input, e)
			}
		}
	}
}

func TestExtractSourceAttribution(t *testing.T) {
	tests := []struct {
		input          string
		expectedSource string
		isAggregation  bool
	}{
		{"According to Reuters, the deal is done", "Reuters", true},
		{"Bloomberg reports earnings beat expectations", "Bloomberg", true},
		{"The company announced (Reuters)", "Reuters", true},
		{"Original reporting without citations", "", false},
		{"Citing sources familiar with the matter", "sources", true},
	}

	for _, tt := range tests {
		result := ExtractSourceAttribution(tt.input)
		if tt.expectedSource == "" {
			if result != nil {
				t.Errorf("ExtractSourceAttribution(%q) = %v, want nil", tt.input, result)
			}
		} else {
			if result == nil {
				t.Errorf("ExtractSourceAttribution(%q) = nil, want source=%q", tt.input, tt.expectedSource)
				continue
			}
			if result.OriginalSource != tt.expectedSource {
				t.Errorf("ExtractSourceAttribution(%q).OriginalSource = %q, want %q", tt.input, result.OriginalSource, tt.expectedSource)
			}
			if result.IsAggregation != tt.isAggregation {
				t.Errorf("ExtractSourceAttribution(%q).IsAggregation = %v, want %v", tt.input, result.IsAggregation, tt.isAggregation)
			}
		}
	}
}

func TestSimHash(t *testing.T) {
	// Identical strings should have identical hashes
	hash1 := SimHash("Breaking news: earthquake hits Tokyo")
	hash2 := SimHash("Breaking news: earthquake hits Tokyo")
	if hash1 != hash2 {
		t.Errorf("Identical strings should have identical hashes")
	}

	// Similar strings should have similar hashes
	hash3 := SimHash("Breaking news: earthquake hits Tokyo region")
	sim := SimilarityScore(hash1, hash3)
	t.Logf("Similar strings similarity: %f", sim)
	if sim < 0.5 {
		t.Errorf("Similar strings should have similar hashes, got similarity %f", sim)
	}

	// Very different strings should have lower similarity than similar ones
	hash4 := SimHash("Apple announces new iPhone 15 with upgraded camera and processor improvements for photographers")
	sim2 := SimilarityScore(hash1, hash4)
	t.Logf("Different strings similarity: %f", sim2)
	// Just verify different is less similar than same (not a hard threshold)
	if sim2 > sim {
		t.Errorf("Different strings should be less similar than similar strings")
	}
}

func TestAreDuplicates(t *testing.T) {
	// Same headline from different sources
	h1 := SimHash("Boeing 737 MAX grounded indefinitely by FAA after safety concerns raised")
	h2 := SimHash("FAA grounds Boeing 737 MAX indefinitely citing ongoing safety review")
	h3 := SimHash("Boeing 737 MAX: FAA extends grounding indefinitely pending investigation")

	// Log similarities for debugging
	t.Logf("Similarity h1-h2: %f", SimilarityScore(h1, h2))
	t.Logf("Similarity h1-h3: %f", SimilarityScore(h1, h3))
	t.Logf("Similarity h2-h3: %f", SimilarityScore(h2, h3))

	// Completely different story
	h4 := SimHash("Apple announces revolutionary new iPhone 15 Pro with advanced titanium design and A17 chip")
	t.Logf("Similarity boeing-apple: %f", SimilarityScore(h1, h4))

	// The Boeing stories should be more similar to each other than to Apple story
	boeingSim := SimilarityScore(h1, h2)
	appleSim := SimilarityScore(h1, h4)
	if appleSim >= boeingSim {
		t.Errorf("Related stories should be more similar than unrelated: boeing=%f, apple=%f", boeingSim, appleSim)
	}
}

func TestContainsWord(t *testing.T) {
	tests := []struct {
		text     string
		word     string
		expected bool
	}{
		{"the american flag", "american", true},
		{"panamerican highway", "american", false},
		{"america first", "america", true},
		{"southamerica", "america", false},
		{"usa today", "usa", true},
		{"usable tools", "usa", false},
	}

	for _, tt := range tests {
		result := containsWord(tt.text, tt.word)
		if result != tt.expected {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tt.text, tt.word, result, tt.expected)
		}
	}
}
