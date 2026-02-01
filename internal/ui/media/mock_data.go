package media

import (
	"time"
)

// GenerateMockHeadlines returns a set of cyber-noir news items for UI testing.
// Follows Gemini-3's clustering pattern: every 5th item has high arousal/negativity.
func GenerateMockHeadlines() []Headline {
	titles := []string{
		"Bio-Link Hacks Spike in Lower Sectors",
		"NightWire: Neon Shortage Looming",
		"CipherPost: Ghost AI Found in Grid 7",
		"Market Crash: Synth-Credits Nullified",
		"RIOT IN SECTOR 4: ENFORCERS DEPLOYED", // High Arousal #5
		"New Patch for Cortex-Dampers Released",
		"Arasaka Rumors: Deep-Dive Incident",
		"Solar Flare Disrupts Satellite Comms",
		"Water Rations Cut for Fringe Citizens",
		"BLACKOUT: GRID COLLAPSE IMMINENT",    // High Arousal #10
		"Synth-Beef Prices Hit All-Time High",
		"Luxury Blimps Spotted Over Slums",
		"Zero-Day Vulnerability in Bio-Ports",
		"Rain Forecast: Acid Levels Rising",
		"EMERGENCY: PLAGUE IN SECTOR 9",       // High Arousal #15
		"Mega-Corp Merger Approved by Council",
		"Underground Net-Running Competition",
		"Neural-Link Latency Issues Reported",
		"Smog-Eaters Fail in Industrial Zone",
		"LEAK: COUNCIL SCANDAL REVEALED",      // High Arousal #20
		"Orbital Colony Reaches Capacity",
		"Memory-Wipe Services See Surge",
		"Street-Doc Arrested for Unlicensed Mods",
		"Rogue Drones Sighted in Cargo Hub",
		"TERROR AT THE APEX: SUICIDE DRONES",  // High Arousal #25
		"Virtual Reality Escapism Addiction",
		"Oxygen Tax Increase Effective Monday",
		"Android Rights Activists Protest",
	}

	sources := []string{"NightWire", "CipherPost", "GridNews", "ApexLink", "VoidFeed"}
	now := time.Now()
	headlines := make([]Headline, len(titles))

	for i, title := range titles {
		isHigh := (i+1)%5 == 0
		
		arousal := 0.2 + (0.3 * (float64(i % 3)))
		if isHigh {
			arousal = 0.85 + (0.1 * float64(i%2))
		}

		neg := 0.1 + (0.2 * float64(i%4))
		if isHigh {
			neg = 0.7 + (0.2 * float64(i%2))
		}

		h := Headline{
			ID:        string(rune('A' + i)),
			Title:     title,
			Source:    sources[i%len(sources)],
			Published: now.Add(-time.Duration(i*10) * time.Minute),
			Breakdown: Breakdown{
				Semantic:     0.4 + (0.1 * float64(i%5)),
				Rerank:       0.3 + (0.1 * float64(i%6)),
				Arousal:      arousal,
				RecencyDecay: 1.0 - (float64(i) * 0.02),
				Diversity:    0.5,
				NegBoost:     neg,
			},
		}
		
		// Final Score Equation (Simplified for mock)
		h.Breakdown.FinalScore = (h.Breakdown.Semantic*0.3 + h.Breakdown.Rerank*0.4 + h.Breakdown.Arousal*0.3) * h.Breakdown.RecencyDecay
		
		h.EnsureHash()
		headlines[i] = h
	}

	return headlines
}
