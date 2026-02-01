package media

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// GlitchEngine provides the logic for distorting text based on "Arousal" scores.
type GlitchEngine struct {
	Styles Styles
}

// Glyph set for glitch effects, synthesized from Gemini-3.
var glitchGlyphs = []rune("#/\\█▓░⟟⟊⟡? معمولاً!")

// fnv32aTriplet combines three inputs into a single hash for stable glitching.
func fnv32aTriplet(tick uint32, pos uint32, titleHash uint32) uint32 {
	hash := uint32(2166136261)
	inputs := [3]uint32{tick, pos, titleHash}
	for _, val := range inputs {
		hash ^= val
		hash *= 16777619
	}
	return hash
}

// Glitchify returns a distorted version of the input title string.
// intensity: 0.0 (no distortion) to 1.0 (full distortion)
// tick: an incrementing value to animate the glitch
func (g *GlitchEngine) Glitchify(title string, intensity float64, tick uint32) string {
	if intensity <= 0.05 {
		return title
	}

	runes := []rune(title)
	var b strings.Builder

	for i, r := range runes {
		hash := fnv32aTriplet(tick, uint32(i), uint32(len(title)))
		
		// Determine if this character should be distorted
		// Probability increases with intensity
		distortionThreshold := uint32(intensity * 0xFFFFFFFF)
		
		if hash < distortionThreshold {
			// Preservation logic: 30% chance to keep alphanumeric characters
			// but apply color flicker. This keeps it somewhat readable.
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				if (hash % 100) < 30 {
					b.WriteString(g.flickerStyle(hash).Render(string(r)))
					continue
				}
			}

			// Substitution
			glyph := glitchGlyphs[hash%uint32(len(glitchGlyphs))]
			b.WriteString(g.flickerStyle(hash).Render(string(glyph)))
		} else {
			// No distortion
			b.WriteRune(r)
		}
	}

	return b.String()
}

// flickerStyle returns one of the glitch colors based on the hash.
func (g *GlitchEngine) flickerStyle(hash uint32) lipgloss.Style {
	choice := hash % 100
	switch {
	case choice < 55:
		return g.Styles.GlitchCyan
	case choice < 80:
		return g.Styles.GlitchPink
	default:
		return g.Styles.GlitchYellow
	}
}

// GetIntensity calculates distortion intensity based on arousal and threshold.
// threshold: minimum arousal before glitching starts (usually 0.55)
func GetIntensity(arousal float64, threshold float64) float64 {
	if arousal < threshold {
		return 0
	}
	// Scale 0.0 to 1.0 based on how far past threshold it is
	return (arousal - threshold) / (1.0 - threshold)
}
