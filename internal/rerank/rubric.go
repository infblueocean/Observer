package rerank

// Rubrics define ranking criteria for different use cases.
// These are queries that the reranker scores documents against.
var Rubrics = map[string]string{
	// Default front page ranking
	"frontpage": `Most important news stories with real-world impact, broad relevance, and credible sourcing.
Prefer clear facts over speculation, multi-source confirmation.
Avoid duplicates, clickbait, promotional content.`,

	// Breaking/urgent news
	"breaking": `Breaking news and urgent developments from the last few hours.
High potential impact, developing situations, wire service reports.
Factual claims, official statements, confirmed events.`,

	// Top stories (replaces LLM classification)
	"topstories": `Most important breaking news and major world developments.
Significant events with broad impact. Major policy changes.
Breaking situations. High-impact technology or science news.`,

	// Technology focus
	"tech": `Significant technology news and developments.
AI/ML breakthroughs, major product launches, security incidents.
Infrastructure changes, developer tools, industry shifts.`,

	// Finance/markets
	"finance": `Important financial and economic news.
Market-moving events, policy changes, earnings surprises.
Economic indicators, regulatory actions, major deals.`,

	// Science
	"science": `Significant scientific discoveries and research.
Peer-reviewed findings, space exploration, climate science.
Medical breakthroughs, technology advances.`,

	// Security
	"security": `Cybersecurity news and threats.
Data breaches, vulnerabilities, nation-state attacks.
Security research, defensive tools, incident response.`,

	// Geopolitics
	"geopolitics": `International relations and geopolitical developments.
Diplomatic actions, conflicts, treaties, sanctions.
Elections, leadership changes, regional tensions.`,
}

// DefaultRubric is used when no specific rubric is requested.
const DefaultRubric = "topstories"

// GetRubric returns a rubric by name, or the default if not found.
func GetRubric(name string) string {
	if rubric, ok := Rubrics[name]; ok {
		return rubric
	}
	return Rubrics[DefaultRubric]
}

// ListRubrics returns all available rubric names.
func ListRubrics() []string {
	names := make([]string, 0, len(Rubrics))
	for name := range Rubrics {
		names = append(names, name)
	}
	return names
}
