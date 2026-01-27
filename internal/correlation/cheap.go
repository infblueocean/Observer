package correlation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/abelbrown/observer/internal/feeds"
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

// CheapExtractor implements EntityExtractor using fast regex-based extraction
// No LLM required - runs instantly on every item
type CheapExtractor struct{}

// NewCheapExtractor creates a new cheap extractor
func NewCheapExtractor() *CheapExtractor {
	return &CheapExtractor{}
}

// Extract extracts entities from item title and content using regex patterns
func (e *CheapExtractor) Extract(item feeds.Item) ([]ItemEntity, error) {
	var entities []ItemEntity
	text := item.Title + " " + item.Summary + " " + item.Content

	// Extract stock tickers with high salience (explicit financial reference)
	tickers := ExtractTickers(text)
	for _, ticker := range tickers {
		entities = append(entities, ItemEntity{
			ItemID:   item.ID,
			EntityID: "ticker:" + ticker,
			Context:  extractContext(text, "$"+ticker),
			Salience: 0.9, // Tickers are highly specific
		})
	}

	// Extract countries with moderate salience
	countries := ExtractCountries(text)
	for _, country := range countries {
		entities = append(entities, ItemEntity{
			ItemID:   item.ID,
			EntityID: "country:" + country,
			Context:  extractContext(text, country),
			Salience: 0.6, // Countries are common, lower salience unless repeated
		})
	}

	// Extract source attribution (is this aggregated content?)
	if attr := ExtractSourceAttribution(text); attr != nil && attr.OriginalSource != "" {
		entities = append(entities, ItemEntity{
			ItemID:   item.ID,
			EntityID: "source:" + strings.ToLower(attr.OriginalSource),
			Context:  "Citing " + attr.OriginalSource,
			Salience: 0.5,
		})
	}

	return entities, nil
}

// extractContext extracts a snippet of text around the target phrase
func extractContext(text, target string) string {
	lower := strings.ToLower(text)
	targetLower := strings.ToLower(target)
	idx := strings.Index(lower, targetLower)
	if idx < 0 {
		return ""
	}

	// Extract 50 chars before and after
	start := idx - 50
	if start < 0 {
		start = 0
	}
	end := idx + len(target) + 50
	if end > len(text) {
		end = len(text)
	}

	return strings.TrimSpace(text[start:end])
}

// Claim extraction patterns
var (
	// Numbers with units (e.g., "$1.5 billion", "30%", "1,000 people")
	numberClaimRegex = regexp.MustCompile(`(\$?[\d,]+\.?\d*)\s*(billion|million|thousand|percent|%|people|deaths|cases|troops|dollars)`)

	// Quoted statements
	quoteClaimRegex = regexp.MustCompile(`"([^"]{10,200})"`)

	// Attribution patterns (X said/claims/denied)
	attributionRegex = regexp.MustCompile(`(?i)([\w\s]+)\s+(said|says|claims?|denied|announced|confirmed|reported|stated)\s+(?:that\s+)?(.{10,100})`)

	// Denial patterns
	denialRegex = regexp.MustCompile(`(?i)(denied|refuted|rejected|dismissed|contradicted|disputed)`)

	// Prediction patterns
	predictionRegex = regexp.MustCompile(`(?i)(will|expected to|forecast|predicted|projected)\s+(.{10,100})`)
)

// ExtractedClaim represents a claim extracted from text
type ExtractedClaim struct {
	Text       string
	Type       string // "number", "quote", "attribution", "denial", "prediction"
	Value      string // The specific value (number, quote text, etc.)
	Speaker    string // Who made the claim (if attribution)
	Confidence float64
}

// ExtractClaims extracts verifiable claims from text
func ExtractClaims(text string) []ExtractedClaim {
	var claims []ExtractedClaim

	// Extract number claims
	for _, match := range numberClaimRegex.FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			claims = append(claims, ExtractedClaim{
				Text:       match[0],
				Type:       "number",
				Value:      match[1] + " " + match[2],
				Confidence: 0.8,
			})
		}
	}

	// Extract quotes
	for _, match := range quoteClaimRegex.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			claims = append(claims, ExtractedClaim{
				Text:       match[0],
				Type:       "quote",
				Value:      match[1],
				Confidence: 0.9,
			})
		}
	}

	// Extract attributions
	for _, match := range attributionRegex.FindAllStringSubmatch(text, -1) {
		if len(match) >= 4 {
			claims = append(claims, ExtractedClaim{
				Text:       match[0],
				Type:       "attribution",
				Value:      match[3],
				Speaker:    strings.TrimSpace(match[1]),
				Confidence: 0.7,
			})
		}
	}

	// Check for denials
	if denialRegex.MatchString(text) {
		for _, match := range attributionRegex.FindAllStringSubmatch(text, -1) {
			if len(match) >= 4 && denialRegex.MatchString(match[2]) {
				claims = append(claims, ExtractedClaim{
					Text:       match[0],
					Type:       "denial",
					Value:      match[3],
					Speaker:    strings.TrimSpace(match[1]),
					Confidence: 0.8,
				})
			}
		}
	}

	// Extract predictions
	for _, match := range predictionRegex.FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			claims = append(claims, ExtractedClaim{
				Text:       match[0],
				Type:       "prediction",
				Value:      match[2],
				Confidence: 0.6,
			})
		}
	}

	return claims
}

// DetectConflicts checks if two sets of claims have conflicts
func DetectConflicts(claims1, claims2 []ExtractedClaim) []DisagreementInfo {
	var conflicts []DisagreementInfo

	for _, c1 := range claims1 {
		for _, c2 := range claims2 {
			// Check for conflicting numbers on same topic
			if c1.Type == "number" && c2.Type == "number" {
				if numbersConflict(c1.Value, c2.Value) {
					conflicts = append(conflicts, DisagreementInfo{
						Type:        "factual",
						Description: fmt.Sprintf("Conflicting figures: %s vs %s", c1.Value, c2.Value),
						ClaimA:      c1.Text,
						ClaimB:      c2.Text,
					})
				}
			}

			// Check for denials
			if c1.Type == "denial" || c2.Type == "denial" {
				conflicts = append(conflicts, DisagreementInfo{
					Type:        "denial",
					Description: "Source denies claim made by another",
					ClaimA:      c1.Text,
					ClaimB:      c2.Text,
				})
			}

			// Check for contradictory quotes from same speaker
			if c1.Type == "attribution" && c2.Type == "attribution" {
				if c1.Speaker == c2.Speaker && c1.Value != c2.Value {
					conflicts = append(conflicts, DisagreementInfo{
						Type:        "framing",
						Description: fmt.Sprintf("Different statements attributed to %s", c1.Speaker),
						ClaimA:      c1.Text,
						ClaimB:      c2.Text,
					})
				}
			}
		}
	}

	return conflicts
}

// DisagreementInfo holds information about a detected disagreement
type DisagreementInfo struct {
	Type        string // "factual", "framing", "denial", "omission"
	Description string
	ClaimA      string
	ClaimB      string
}

// numbersConflict checks if two number claims are significantly different
func numbersConflict(val1, val2 string) bool {
	// Extract numeric parts
	num1 := extractNumber(val1)
	num2 := extractNumber(val2)

	if num1 == 0 || num2 == 0 {
		return false
	}

	// Check if numbers differ by more than 20%
	diff := num1 - num2
	if diff < 0 {
		diff = -diff
	}
	avg := (num1 + num2) / 2

	return diff/avg > 0.2
}

// extractNumber extracts a float from a string like "1.5 billion"
func extractNumber(s string) float64 {
	// Remove $ and commas
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")

	// Parse multiplier
	multiplier := 1.0
	sLower := strings.ToLower(s)
	if strings.Contains(sLower, "billion") {
		multiplier = 1e9
	} else if strings.Contains(sLower, "million") {
		multiplier = 1e6
	} else if strings.Contains(sLower, "thousand") {
		multiplier = 1e3
	}

	// Extract the number
	var num float64
	fmt.Sscanf(s, "%f", &num)

	return num * multiplier
}
