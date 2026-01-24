package feeds

// DefaultRSSFeeds is the curated list of RSS sources
// Organized by category for transparency - you see exactly what you're subscribed to
// Weight: 1.0 = normal, >1 = more important, <1 = less important
var DefaultRSSFeeds = []RSSFeedConfig{
	// ============================================
	// WIRE SERVICES & PRIMARY NEWS (High Signal, Fast Refresh)
	// ============================================
	{Name: "Reuters", URL: "https://www.reutersagency.com/feed/?taxonomy=best-sectors&post_type=best", Category: "wire", RefreshMinutes: RefreshNormal, Weight: 1.5},
	{Name: "AP News", URL: "https://rsshub.app/apnews/topics/apf-topnews", Category: "wire", RefreshMinutes: RefreshNormal, Weight: 1.5},
	{Name: "BBC World", URL: "https://feeds.bbci.co.uk/news/world/rss.xml", Category: "wire", RefreshMinutes: RefreshNormal, Weight: 1.3},
	{Name: "BBC Top", URL: "https://feeds.bbci.co.uk/news/rss.xml", Category: "wire", RefreshMinutes: RefreshNormal, Weight: 1.3},
	{Name: "Al Jazeera", URL: "https://www.aljazeera.com/xml/rss/all.xml", Category: "wire", RefreshMinutes: RefreshNormal, Weight: 1.2},
	{Name: "NPR News", URL: "https://feeds.npr.org/1001/rss.xml", Category: "wire", RefreshMinutes: RefreshSlow, Weight: 1.2},

	// ============================================
	// US TV NETWORKS (Secondary/Corroboration)
	// ============================================
	{Name: "CNN Top", URL: "http://rss.cnn.com/rss/edition.rss", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "CNN World", URL: "http://rss.cnn.com/rss/edition_world.rss", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "CNN Politics", URL: "http://rss.cnn.com/rss/cnn_allpolitics.rss", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "NBC News", URL: "http://feeds.nbcnews.com/feeds/topstories", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "CBS News", URL: "https://www.cbsnews.com/latest/rss/main", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "ABC News", URL: "https://feeds.abcnews.com/abcnews/topstories", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "Fox News", URL: "http://feeds.foxnews.com/foxnews/latest", Category: "tv-us", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "PBS NewsHour", URL: "https://www.pbs.org/newshour/feeds/rss/headlines", Category: "tv-us", RefreshMinutes: RefreshLazy, Weight: 1.1},

	// ============================================
	// NEWSPAPERS - US
	// ============================================
	{Name: "NY Times", URL: "https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "NY Times World", URL: "https://rss.nytimes.com/services/xml/rss/nyt/World.xml", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Washington Post", URL: "http://feeds.washingtonpost.com/rss/world", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "LA Times", URL: "https://www.latimes.com/world-nation/rss2.0.xml", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Wall St Journal", URL: "https://feeds.a.dj.com/rss/RSSWorldNews.xml", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "USA Today", URL: "http://rssfeeds.usatoday.com/usatoday-NewsTopStories", Category: "newspaper-us", RefreshMinutes: RefreshLazy, Weight: 0.9},

	// ============================================
	// NEWSPAPERS - INTERNATIONAL
	// ============================================
	{Name: "The Guardian", URL: "https://www.theguardian.com/world/rss", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "Guardian US", URL: "https://www.theguardian.com/us-news/rss", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "The Telegraph", URL: "https://www.telegraph.co.uk/rss.xml", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Der Spiegel", URL: "https://www.spiegel.de/international/index.rss", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "France 24", URL: "https://www.france24.com/en/rss", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "DW News", URL: "https://rss.dw.com/rdf/rss-en-all", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Japan Times", URL: "https://www.japantimes.co.jp/feed/", Category: "newspaper-intl", RefreshMinutes: RefreshHourly, Weight: 1.0},
	{Name: "South China MP", URL: "https://www.scmp.com/rss/91/feed", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Times of India", URL: "https://timesofindia.indiatimes.com/rssfeedstopstories.cms", Category: "newspaper-intl", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Sydney Morning Herald", URL: "https://www.smh.com.au/rss/feed.xml", Category: "newspaper-intl", RefreshMinutes: RefreshHourly, Weight: 1.0},

	// ============================================
	// TECH NEWS (Fast refresh - moves quickly)
	// ============================================
	{Name: "Hacker News", URL: "https://news.ycombinator.com/rss", Category: "tech", RefreshMinutes: RefreshFast, Weight: 1.3},
	{Name: "Lobsters", URL: "https://lobste.rs/rss", Category: "tech", RefreshMinutes: RefreshNormal, Weight: 1.2},
	{Name: "Ars Technica", URL: "https://feeds.arstechnica.com/arstechnica/index", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "The Verge", URL: "https://www.theverge.com/rss/index.xml", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "Wired", URL: "https://www.wired.com/feed/rss", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "TechCrunch", URL: "https://techcrunch.com/feed/", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "Engadget", URL: "https://www.engadget.com/rss.xml", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 0.9},
	{Name: "AnandTech", URL: "https://www.anandtech.com/rss/", Category: "tech", RefreshMinutes: RefreshHourly, Weight: 1.1},
	{Name: "Slashdot", URL: "http://rss.slashdot.org/Slashdot/slashdotMain", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "dev.to", URL: "https://dev.to/feed", Category: "tech", RefreshMinutes: RefreshSlow, Weight: 0.8},
	{Name: "HackerNoon", URL: "https://hackernoon.com/feed", Category: "tech", RefreshMinutes: RefreshLazy, Weight: 0.8},

	// ============================================
	// AI & ML SPECIFIC (High signal, less frequent)
	// ============================================
	{Name: "OpenAI Blog", URL: "https://openai.com/blog/rss/", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.5},
	{Name: "Anthropic News", URL: "https://www.anthropic.com/rss.xml", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.5},
	{Name: "Google AI Blog", URL: "https://blog.google/technology/ai/rss/", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "DeepMind Blog", URL: "https://deepmind.com/blog/feed/basic/", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "MIT AI News", URL: "https://news.mit.edu/topic/artificial-intelligence2/feed", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.2},
	{Name: "AI News (Sebastian)", URL: "https://buttondown.email/ainews/rss", Category: "ai", RefreshMinutes: RefreshHourly, Weight: 1.1},

	// ============================================
	// SCIENCE
	// ============================================
	{Name: "Nature", URL: "https://www.nature.com/nature.rss", Category: "science", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "Science Magazine", URL: "https://www.science.org/rss/news_current.xml", Category: "science", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "Scientific American", URL: "http://rss.sciam.com/ScientificAmerican-Global", Category: "science", RefreshMinutes: RefreshHourly, Weight: 1.1},
	{Name: "New Scientist", URL: "https://www.newscientist.com/feed/home/", Category: "science", RefreshMinutes: RefreshHourly, Weight: 1.1},
	{Name: "Quanta Magazine", URL: "https://api.quantamagazine.org/feed/", Category: "science", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "Phys.org", URL: "https://phys.org/rss-feed/", Category: "science", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Space.com", URL: "https://www.space.com/feeds/all", Category: "science", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "NASA Breaking", URL: "https://www.nasa.gov/rss/dyn/breaking_news.rss", Category: "science", RefreshMinutes: RefreshSlow, Weight: 1.2},

	// ============================================
	// FINANCE & BUSINESS
	// ============================================
	{Name: "Bloomberg", URL: "https://feeds.bloomberg.com/markets/news.rss", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.3},
	{Name: "CNBC Top", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.1},
	{Name: "Financial Times", URL: "https://www.ft.com/rss/home", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 1.3},
	{Name: "MarketWatch", URL: "http://feeds.marketwatch.com/marketwatch/topstories/", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Economist", URL: "https://www.economist.com/latest/rss.xml", Category: "finance", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "Forbes", URL: "https://www.forbes.com/real-time/feed2/", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 0.9},

	// ============================================
	// POLITICS & POLICY
	// ============================================
	{Name: "Politico", URL: "https://www.politico.com/rss/politicopicks.xml", Category: "politics", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "The Hill", URL: "https://thehill.com/feed/", Category: "politics", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "Roll Call", URL: "https://www.rollcall.com/feed/", Category: "politics", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "FiveThirtyEight", URL: "https://fivethirtyeight.com/features/feed/", Category: "politics", RefreshMinutes: RefreshHourly, Weight: 1.2},
	{Name: "ProPublica", URL: "http://feeds.propublica.org/propublica/main", Category: "politics", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "The Intercept", URL: "https://theintercept.com/feed/?rss", Category: "politics", RefreshMinutes: RefreshHourly, Weight: 1.1},

	// ============================================
	// SECURITY & CYBER
	// ============================================
	{Name: "Krebs on Security", URL: "https://krebsonsecurity.com/feed/", Category: "security", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "Schneier on Security", URL: "https://www.schneier.com/feed/", Category: "security", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "The Hacker News", URL: "https://feeds.feedburner.com/TheHackersNews", Category: "security", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "Bleeping Computer", URL: "https://www.bleepingcomputer.com/feed/", Category: "security", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "Dark Reading", URL: "https://www.darkreading.com/rss.xml", Category: "security", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "CISA Alerts", URL: "https://www.cisa.gov/uscert/ncas/alerts.xml", Category: "security", RefreshMinutes: RefreshNormal, Weight: 1.5},

	// ============================================
	// CRYPTO & WEB3
	// ============================================
	{Name: "CoinDesk", URL: "https://www.coindesk.com/arc/outboundfeeds/rss/", Category: "crypto", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Cointelegraph", URL: "https://cointelegraph.com/rss", Category: "crypto", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Decrypt", URL: "https://decrypt.co/feed", Category: "crypto", RefreshMinutes: RefreshSlow, Weight: 1.0},

	// ============================================
	// LONGFORM & ANALYSIS (Slow refresh, high quality)
	// ============================================
	{Name: "The Atlantic", URL: "https://www.theatlantic.com/feed/all/", Category: "longform", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "New Yorker", URL: "https://www.newyorker.com/feed/everything", Category: "longform", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "Vox", URL: "https://www.vox.com/rss/index.xml", Category: "longform", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Slate", URL: "https://slate.com/feeds/all.rss", Category: "longform", RefreshMinutes: RefreshLazy, Weight: 0.9},
	{Name: "Aeon", URL: "https://aeon.co/feed.rss", Category: "longform", RefreshMinutes: RefreshHourly, Weight: 1.2},
	{Name: "Nautilus", URL: "https://nautil.us/feed/", Category: "longform", RefreshMinutes: RefreshHourly, Weight: 1.2},

	// ============================================
	// CULTURE & ENTERTAINMENT
	// ============================================
	{Name: "Variety", URL: "https://variety.com/feed/", Category: "entertainment", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Hollywood Reporter", URL: "https://www.hollywoodreporter.com/feed/", Category: "entertainment", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Pitchfork", URL: "https://pitchfork.com/feed/feed-news/rss", Category: "entertainment", RefreshMinutes: RefreshHourly, Weight: 1.0},
	{Name: "Rolling Stone", URL: "https://www.rollingstone.com/feed/", Category: "entertainment", RefreshMinutes: RefreshLazy, Weight: 1.0},

	// ============================================
	// SPORTS
	// ============================================
	{Name: "ESPN", URL: "https://www.espn.com/espn/rss/news", Category: "sports", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "BBC Sport", URL: "https://feeds.bbci.co.uk/sport/rss.xml", Category: "sports", RefreshMinutes: RefreshSlow, Weight: 1.0},

	// ============================================
	// REGIONAL US
	// ============================================
	{Name: "Chicago Tribune", URL: "https://www.chicagotribune.com/arcio/rss/", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
	{Name: "Boston Globe", URL: "https://www.bostonglobe.com/rss/hpheadlines", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
	{Name: "SF Chronicle", URL: "https://www.sfchronicle.com/rss/feed/RSS-Bay-Area-News-702.php", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
	{Name: "Miami Herald", URL: "https://www.miamiherald.com/latest-news/index.rss", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
	{Name: "Seattle Times", URL: "https://www.seattletimes.com/feed/", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
	{Name: "Denver Post", URL: "https://www.denverpost.com/feed/", Category: "regional-us", RefreshMinutes: RefreshHourly, Weight: 0.8},
}

// RefreshInterval presets
const (
	RefreshRealtime = 1  // 1 minute - earthquakes, breaking
	RefreshFast     = 2  // 2 minutes - HN, fast-moving
	RefreshNormal   = 5  // 5 minutes - wire services
	RefreshSlow     = 15 // 15 minutes - blogs, tech
	RefreshLazy     = 30 // 30 minutes - newspapers, longform
	RefreshHourly   = 60 // 1 hour - very slow sources
)

// RSSFeedConfig represents a configured RSS feed
type RSSFeedConfig struct {
	Name            string
	URL             string
	Category        string
	RefreshMinutes  int     // How often to poll (default: 5)
	Weight          float64 // Importance weight (default: 1.0)
	Enabled         bool    // For user filtering
}

// Categories returns all unique categories
func Categories() []string {
	return []string{
		"wire",
		"tv-us",
		"newspaper-us",
		"newspaper-intl",
		"tech",
		"ai",
		"science",
		"finance",
		"politics",
		"security",
		"crypto",
		"longform",
		"entertainment",
		"sports",
		"regional-us",
	}
}

// FeedsByCategory returns feeds filtered by category
func FeedsByCategory(category string) []RSSFeedConfig {
	var result []RSSFeedConfig
	for _, f := range DefaultRSSFeeds {
		if f.Category == category {
			result = append(result, f)
		}
	}
	return result
}
