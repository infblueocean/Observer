package correlation

import (
	"regexp"
	"strings"
)

// Cheap extraction functions - no LLM required, instant processing
// These run on every item immediately and provide basic entity extraction

// tickerRegex matches stock tickers like $AAPL, $TSLA, $BRK.A
var tickerRegex = regexp.MustCompile(`\$([A-Z]{1,5}(?:\.[A-Z])?)`)

// ExtractTickers finds stock tickers in text
// Returns normalized tickers without the $ prefix
func ExtractTickers(text string) []string {
	matches := tickerRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var result []string

	for _, match := range matches {
		if len(match) > 1 {
			ticker := match[1]
			if !seen[ticker] {
				seen[ticker] = true
				result = append(result, ticker)
			}
		}
	}

	return result
}

// Countries list for extraction (ISO names + common variants)
var countryNames = map[string]string{
	// Major powers
	"united states": "united_states", "usa": "united_states", "us": "united_states", "u.s.": "united_states", "u.s": "united_states", "america": "united_states", "american": "united_states",
	"china": "china", "chinese": "china", "prc": "china", "beijing": "china",
	"russia": "russia", "russian": "russia", "moscow": "russia", "kremlin": "russia",
	"united kingdom": "united_kingdom", "uk": "united_kingdom", "britain": "united_kingdom", "british": "united_kingdom", "england": "united_kingdom",
	"germany": "germany", "german": "germany", "berlin": "germany",
	"france": "france", "french": "france", "paris": "france",
	"japan": "japan", "japanese": "japan", "tokyo": "japan",
	"india": "india", "indian": "india", "new delhi": "india",

	// Conflict zones / high news frequency
	"ukraine": "ukraine", "ukrainian": "ukraine", "kyiv": "ukraine", "kiev": "ukraine",
	"israel": "israel", "israeli": "israel", "tel aviv": "israel", "jerusalem": "israel",
	"palestine": "palestine", "palestinian": "palestine", "gaza": "palestine", "west bank": "palestine",
	"iran": "iran", "iranian": "iran", "tehran": "iran",
	"north korea": "north_korea", "dprk": "north_korea", "pyongyang": "north_korea",
	"south korea": "south_korea", "korea": "south_korea", "seoul": "south_korea",
	"taiwan": "taiwan", "taiwanese": "taiwan", "taipei": "taiwan",
	"syria": "syria", "syrian": "syria", "damascus": "syria",
	"afghanistan": "afghanistan", "afghan": "afghanistan", "kabul": "afghanistan",
	"iraq": "iraq", "iraqi": "iraq", "baghdad": "iraq",

	// Major economies
	"canada": "canada", "canadian": "canada", "ottawa": "canada",
	"australia": "australia", "australian": "australia", "sydney": "australia", "canberra": "australia",
	"brazil": "brazil", "brazilian": "brazil", "brasilia": "brazil",
	"mexico": "mexico", "mexican": "mexico", "mexico city": "mexico",
	"italy": "italy", "italian": "italy", "rome": "italy",
	"spain": "spain", "spanish": "spain", "madrid": "spain",
	"netherlands": "netherlands", "dutch": "netherlands", "amsterdam": "netherlands",
	"switzerland": "switzerland", "swiss": "switzerland", "zurich": "switzerland",
	"sweden": "sweden", "swedish": "sweden", "stockholm": "sweden",
	"norway": "norway", "norwegian": "norway", "oslo": "norway",
	"poland": "poland", "polish": "poland", "warsaw": "poland",
	"turkey": "turkey", "turkish": "turkey", "ankara": "turkey",
	"saudi arabia": "saudi_arabia", "saudi": "saudi_arabia", "riyadh": "saudi_arabia",
	"uae": "uae", "emirates": "uae", "dubai": "uae", "abu dhabi": "uae",
	"egypt": "egypt", "egyptian": "egypt", "cairo": "egypt",
	"south africa": "south_africa",
	"nigeria": "nigeria", "nigerian": "nigeria",
	"indonesia": "indonesia", "indonesian": "indonesia", "jakarta": "indonesia",
	"singapore": "singapore",
	"hong kong": "hong_kong",
	"vietnam": "vietnam", "vietnamese": "vietnam", "hanoi": "vietnam",
	"thailand": "thailand", "thai": "thailand", "bangkok": "thailand",
	"philippines": "philippines", "filipino": "philippines", "manila": "philippines",
	"malaysia": "malaysia", "malaysian": "malaysia", "kuala lumpur": "malaysia",
	"argentina": "argentina", "argentine": "argentina", "buenos aires": "argentina",
	"chile": "chile", "chilean": "chile", "santiago": "chile",
	"colombia": "colombia", "colombian": "colombia", "bogota": "colombia",
	"venezuela": "venezuela", "venezuelan": "venezuela", "caracas": "venezuela",

	// Blocs
	"european union": "european_union", "eu": "european_union", "brussels": "european_union",
	"nato": "nato",
	"asean": "asean",
	"opec": "opec",
}

// ExtractCountries finds country/region mentions in text
// Returns normalized country IDs
func ExtractCountries(text string) []string {
	lower := strings.ToLower(text)
	seen := make(map[string]bool)
	var result []string

	for name, normalized := range countryNames {
		// Check for word boundary matches to avoid false positives
		// e.g., "american" shouldn't match inside "panamerican"
		if containsWord(lower, name) {
			if !seen[normalized] {
				seen[normalized] = true
				result = append(result, normalized)
			}
		}
	}

	return result
}

// containsWord checks if text contains word as a whole word (not substring)
func containsWord(text, word string) bool {
	idx := strings.Index(text, word)
	if idx < 0 {
		return false
	}

	// Check left boundary
	if idx > 0 {
		prev := text[idx-1]
		if isAlphaNum(prev) {
			// Not a word boundary, might be substring - check for other occurrences
			return containsWord(text[idx+len(word):], word)
		}
	}

	// Check right boundary
	end := idx + len(word)
	if end < len(text) {
		next := text[end]
		if isAlphaNum(next) {
			// Not a word boundary
			return containsWord(text[end:], word)
		}
	}

	return true
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// sourceAttributionPatterns detects when an article cites another source
var sourceAttributionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)according to (Reuters|AP|AFP|Bloomberg|NYT|New York Times|Washington Post|WSJ|Wall Street Journal|CNN|BBC|Fox News|CNBC)`),
	regexp.MustCompile(`(?i)(Reuters|AP|AFP|Bloomberg) reports?`),
	regexp.MustCompile(`(?i)reported by (Reuters|AP|AFP|Bloomberg|NYT|Washington Post)`),
	regexp.MustCompile(`(?i)citing (Reuters|AP|AFP|Bloomberg|sources)`),
	regexp.MustCompile(`(?i)\((Reuters|AP|AFP|Bloomberg)\)`),
}

// ExtractSourceAttribution detects if article is aggregating from another source
func ExtractSourceAttribution(text string) *SourceAttribution {
	for _, pattern := range sourceAttributionPatterns {
		if matches := pattern.FindStringSubmatch(text); len(matches) > 1 {
			return &SourceAttribution{
				OriginalSource: matches[1],
				IsAggregation:  true,
			}
		}
	}
	return nil
}

// SimHash calculates a similarity hash for deduplication
// Uses a simple character n-gram approach
func SimHash(text string) uint64 {
	// Normalize text
	text = strings.ToLower(text)
	text = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			return r
		}
		return ' '
	}, text)

	// Generate 3-grams and hash them
	var hash uint64
	words := strings.Fields(text)

	for i := 0; i < len(words)-2; i++ {
		trigram := words[i] + " " + words[i+1] + " " + words[i+2]
		// Simple hash function
		var h uint64 = 5381
		for _, c := range trigram {
			h = ((h << 5) + h) + uint64(c)
		}
		// Set bit based on hash
		hash |= (1 << (h % 64))
	}

	return hash
}

// SimilarityScore calculates how similar two SimHash values are
// Returns 0.0 to 1.0 (1.0 = identical)
func SimilarityScore(hash1, hash2 uint64) float64 {
	// Count matching bits (Hamming distance)
	xor := hash1 ^ hash2
	diff := 0
	for xor != 0 {
		diff++
		xor &= xor - 1
	}
	// 64 bits total, so similarity is (64 - diff) / 64
	return float64(64-diff) / 64.0
}

// AreDuplicates returns true if two hashes are similar enough to be duplicates
// Threshold of 0.8 means at least 80% of bits match
func AreDuplicates(hash1, hash2 uint64) bool {
	return SimilarityScore(hash1, hash2) >= 0.8
}
