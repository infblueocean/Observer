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
	// AGGREGATORS (Anonymous - no login required)
	// These are curated views from other aggregators we can tap anonymously
	// ============================================
	{Name: "Techmeme", URL: "https://www.techmeme.com/feed.xml", Category: "aggregator", RefreshMinutes: RefreshNormal, Weight: 1.4},
	{Name: "Techmeme Firehose", URL: "https://www.techmeme.com/firehose.xml", Category: "aggregator", RefreshMinutes: RefreshFast, Weight: 1.2},
	{Name: "Memeorandum", URL: "https://www.memeorandum.com/feed.xml", Category: "aggregator", RefreshMinutes: RefreshNormal, Weight: 1.3},
	{Name: "AllSides", URL: "https://www.allsides.com/news/rss", Category: "aggregator", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "Google News Top", URL: "https://news.google.com/rss", Category: "aggregator", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Google News World", URL: "https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRGx1YlY4U0FtVnVHZ0pWVXlnQVAB", Category: "aggregator", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Google News Tech", URL: "https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRGRqTVhZU0FtVnVHZ0pWVXlnQVAB", Category: "aggregator", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Google News Sci", URL: "https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRFp0Y1RjU0FtVnVHZ0pWVXlnQVAB", Category: "aggregator", RefreshMinutes: RefreshLazy, Weight: 1.0},

	// ============================================
	// VIRAL / INTERNET CULTURE (Surfaces X/Twitter content)
	// These sites cover trending tweets, viral content, and memes
	// without requiring X API access
	// ============================================
	{Name: "Daily Dot", URL: "https://www.dailydot.com/feed/", Category: "viral", RefreshMinutes: RefreshNormal, Weight: 1.2},
	{Name: "Daily Dot Viral", URL: "https://www.dailydot.com/tags/viral/feed/", Category: "viral", RefreshMinutes: RefreshNormal, Weight: 1.3},
	{Name: "Daily Dot Social", URL: "https://www.dailydot.com/tags/social-media/feed/", Category: "viral", RefreshMinutes: RefreshNormal, Weight: 1.2},
	{Name: "BuzzFeed Internet", URL: "https://www.buzzfeed.com/bestoftheinternet.xml", Category: "viral", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "Know Your Meme", URL: "https://knowyourmeme.com/newsfeed.rss", Category: "viral", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "Mashable", URL: "https://mashable.com/feeds/rss/all", Category: "viral", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "Input Mag", URL: "https://www.inputmag.com/rss", Category: "viral", RefreshMinutes: RefreshSlow, Weight: 1.0},

	// ============================================
	// REDDIT PUBLIC (Anonymous via .rss suffix)
	// ============================================
	{Name: "r/worldnews", URL: "https://www.reddit.com/r/worldnews/top/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "r/technology", URL: "https://www.reddit.com/r/technology/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "r/science", URL: "https://www.reddit.com/r/science/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "r/programming", URL: "https://www.reddit.com/r/programming/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "r/MachineLearning", URL: "https://www.reddit.com/r/MachineLearning/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "r/LocalLLaMA", URL: "https://www.reddit.com/r/LocalLLaMA/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "r/singularity", URL: "https://www.reddit.com/r/singularity/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "r/Futurology", URL: "https://www.reddit.com/r/Futurology/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 0.9},
	{Name: "r/geopolitics", URL: "https://www.reddit.com/r/geopolitics/top/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "r/Economics", URL: "https://www.reddit.com/r/Economics/hot/.rss?limit=25", Category: "reddit", RefreshMinutes: RefreshLazy, Weight: 1.0},

	// ============================================
	// BLUESKY PUBLIC (Native RSS, no auth)
	// ============================================
	{Name: "Bluesky Official", URL: "https://bsky.app/profile/bsky.app/rss", Category: "bluesky", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Bluesky Engineering", URL: "https://bsky.app/profile/atproto.com/rss", Category: "bluesky", RefreshMinutes: RefreshLazy, Weight: 1.1},

	// ============================================
	// ARXIV (Academic preprints - public, no auth)
	// ============================================
	{Name: "arXiv cs.AI", URL: "https://rss.arxiv.org/rss/cs.AI", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "arXiv cs.LG", URL: "https://rss.arxiv.org/rss/cs.LG", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.3},
	{Name: "arXiv cs.CL", URL: "https://rss.arxiv.org/rss/cs.CL", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.2},
	{Name: "arXiv cs.CV", URL: "https://rss.arxiv.org/rss/cs.CV", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.1},
	{Name: "arXiv cs.CR", URL: "https://rss.arxiv.org/rss/cs.CR", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.2},
	{Name: "arXiv econ.GN", URL: "https://rss.arxiv.org/rss/econ.GN", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.0},
	{Name: "arXiv physics", URL: "https://rss.arxiv.org/rss/physics", Category: "arxiv", RefreshMinutes: RefreshHourly, Weight: 1.0},

	// ============================================
	// SEC EDGAR (Public filings - no auth, 10 req/sec limit)
	// ============================================
	{Name: "SEC Latest Filings", URL: "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=&company=&dateb=&owner=include&count=40&output=atom", Category: "sec", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "SEC 8-K Filings", URL: "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=8-K&company=&dateb=&owner=include&count=40&output=atom", Category: "sec", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "SEC 10-K Filings", URL: "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=10-K&company=&dateb=&owner=include&count=40&output=atom", Category: "sec", RefreshMinutes: RefreshLazy, Weight: 1.0},

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
	{Name: "Bloomberg Politics", URL: "https://feeds.bloomberg.com/politics/news.rss", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.2},
	{Name: "CNBC Top", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.1},
	{Name: "CNBC Markets", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=20910258", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.1},
	{Name: "Financial Times", URL: "https://www.ft.com/rss/home", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 1.3},
	{Name: "MarketWatch", URL: "http://feeds.marketwatch.com/marketwatch/topstories/", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "MarketWatch Breaking", URL: "http://feeds.marketwatch.com/marketwatch/marketpulse/", Category: "finance", RefreshMinutes: RefreshFast, Weight: 1.2},
	{Name: "Economist", URL: "https://www.economist.com/latest/rss.xml", Category: "finance", RefreshMinutes: RefreshHourly, Weight: 1.4},
	{Name: "Forbes", URL: "https://www.forbes.com/real-time/feed2/", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 0.9},
	{Name: "Yahoo Finance", URL: "https://finance.yahoo.com/news/rssindex", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.0},
	{Name: "Barron's", URL: "https://www.barrons.com/feed", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "Seeking Alpha", URL: "https://seekingalpha.com/feed.xml", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "Zero Hedge", URL: "https://feeds.feedburner.com/zerohedge/feed", Category: "finance", RefreshMinutes: RefreshNormal, Weight: 1.1},
	{Name: "Calculated Risk", URL: "https://www.calculatedriskblog.com/feeds/posts/default", Category: "finance", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "Naked Capitalism", URL: "https://www.nakedcapitalism.com/feed", Category: "finance", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Fed Reserve", URL: "https://www.federalreserve.gov/feeds/press_all.xml", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 1.4},
	{Name: "r/wallstreetbets", URL: "https://www.reddit.com/r/wallstreetbets/hot/.rss?limit=25", Category: "finance", RefreshMinutes: RefreshSlow, Weight: 0.9},
	{Name: "r/investing", URL: "https://www.reddit.com/r/investing/hot/.rss?limit=25", Category: "finance", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "r/stocks", URL: "https://www.reddit.com/r/stocks/hot/.rss?limit=25", Category: "finance", RefreshMinutes: RefreshLazy, Weight: 1.0},

	// ============================================
	// MILITARY & DEFENSE
	// ============================================
	{Name: "Defense News", URL: "https://www.defensenews.com/arc/outboundfeeds/rss/?outputType=xml", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.3},
	{Name: "Breaking Defense", URL: "https://breakingdefense.com/feed/", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.3},
	{Name: "Defense One", URL: "https://www.defenseone.com/rss/all/", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "The War Zone", URL: "https://www.thedrive.com/the-war-zone/feed", Category: "military", RefreshMinutes: RefreshNormal, Weight: 1.4},
	{Name: "Military Times", URL: "https://www.militarytimes.com/arc/outboundfeeds/rss/?outputType=xml", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.2},
	{Name: "C4ISRNET", URL: "https://www.c4isrnet.com/arc/outboundfeeds/rss/?outputType=xml", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Stars & Stripes", URL: "https://www.stripes.com/rss", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "War on the Rocks", URL: "https://warontherocks.com/feed/", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.3},
	{Name: "Naval News", URL: "https://www.navalnews.com/feed/", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Air & Space Forces", URL: "https://www.airandspaceforces.com/feed/", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.1},
	{Name: "Army Times", URL: "https://www.armytimes.com/arc/outboundfeeds/rss/?outputType=xml", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "Navy Times", URL: "https://www.navytimes.com/arc/outboundfeeds/rss/?outputType=xml", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "USNI News", URL: "https://news.usni.org/feed", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "Janes", URL: "https://www.janes.com/feeds/news", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.4},
	{Name: "r/CredibleDefense", URL: "https://www.reddit.com/r/CredibleDefense/hot/.rss?limit=25", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.2},
	{Name: "r/LessCredibleDefence", URL: "https://www.reddit.com/r/LessCredibleDefence/hot/.rss?limit=25", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.0},
	{Name: "r/ukraine", URL: "https://www.reddit.com/r/ukraine/hot/.rss?limit=25", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.1},
	{Name: "r/CombatFootage", URL: "https://www.reddit.com/r/CombatFootage/hot/.rss?limit=25", Category: "military", RefreshMinutes: RefreshSlow, Weight: 1.0},
	{Name: "ISW", URL: "https://www.understandingwar.org/rss.xml", Category: "military", RefreshMinutes: RefreshLazy, Weight: 1.4},

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
		"aggregator", // Anonymous third-party aggregators (Techmeme, etc)
		"reddit",     // Reddit public subreddits (no login via .rss)
		"bluesky",    // Bluesky native RSS (no auth)
		"arxiv",      // Academic preprints (public)
		"sec",        // SEC EDGAR filings (public)
		"wire",
		"tv-us",
		"newspaper-us",
		"newspaper-intl",
		"tech",
		"ai",
		"science",
		"finance",
		"military", // Defense, geopolitics, conflict
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
