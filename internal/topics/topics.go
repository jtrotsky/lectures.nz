// Package topics infers topic tags for lectures from their title and summary.
package topics

import "strings"

// excludeKeywords are title substrings that indicate a non-lecture event.
// Matched case-insensitively against the full title.
//
// Guiding principle: exclude only when the event clearly has no educational
// intent — admin events, pure fitness classes, commercial markets.
// When in doubt, include. Cultural workshops, hands-on learning, and family
// events with intellectual content should pass through.
var excludeKeywords = []string{
	// Admin / institutional
	"open day",
	"open evening",
	"open house",
	"orientation",
	"graduation",
	"commencement",
	"job fair",
	"career fair",
	"enrolment",
	"enrollment",
	// Pure fitness (not cultural practice)
	"yoga class",
	"fitness class",
	"gym",
	"zumba",
	"pilates",
	// Commercial / market
	"market day",
	"food market",
	"night market",
	// Children's entertainment (not educational)
	"school holiday programme",
	"holiday programme",
	"holiday program",
	"kids party",
	"birthday party",
	"egg hunt",
	"year olds",
	// Live performances / concerts (not lectures)
	"live performance",
	"live day",
	"symphony orchestra",
	"bohm presents", // Te Papa live performance series
	// Ceremonies (not educational)
	"awards night",
	"awards ceremony",
	// Activity sessions (not lectures)
	"mandarin corner",   // VUW craft/cultural activity series
	"sit and sketch",    // art activity
	"sketch & sip",      // social art activity
	"sketch&sip",        // variant spelling
	"spell candle",      // craft activity
	"collage club",      // art activity club
	"super creative live", // AGANZ entertainment series
	"make art from plants",
	// Markets / social events
	"pride market",
	"queers & wares", // craft market, not educational
	// Wellness / self-help (not academic)
	"nourish me well", // student orientation wellness event
	// Student workshop series (not public lectures)
	"ideas challenge",
}

// IsExcluded reports whether a lecture title looks like a non-lecture event
// that should be filtered out (open days, orientations, children's events, etc.).
func IsExcluded(title string) bool {
	lower := strings.ToLower(title)
	for _, kw := range excludeKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// Topic represents a broad subject area used for filtering.
type Topic struct {
	Slug     string
	Label    string
	keywords []string
}

var topics = []Topic{
	{
		Slug:  "arts-culture",
		Label: "Arts & Culture",
		keywords: []string{
			"art", "artist", "curator", "gallery", "exhibition", "creative",
			"design", "craft", "sculpture", "photography", "drawing", "painting",
			"theatre", "theater", "performance", "dance", "opera", "film",
			"cinema", "music", "concert", "heritage", "cultural", "culture",
			"architecture", "fashion", "ceramics", "printmaking",
		},
	},
	{
		Slug:  "science-technology",
		Label: "Science & Tech",
		keywords: []string{
			"science", "scientific", "technology", "engineering", "physics",
			"chemistry", "biology", "mathematics", "maths", "computing",
			"computer", "software", "AI", "artificial intelligence",
			"robotics", "astronomy", "geology", "neuroscience", "genetics",
			"research", "innovation", "digital", "data", "quantum",
		},
	},
	{
		Slug:  "history-society",
		Label: "History & Society",
		keywords: []string{
			"history", "historical", "heritage", "archaeology", "archive",
			"society", "social", "community", "anthropology", "sociology",
			"culture", "colonial", "māori", "indigenous", "taonga",
			"whakapapa", "identity", "diversity", "equity", "gender",
		},
	},
	{
		Slug:  "environment",
		Label: "Environment",
		keywords: []string{
			"environment", "environmental", "climate", "ecology", "nature",
			"sustainability", "sustainable", "conservation", "biodiversity",
			"ocean", "marine", "wildlife", "forestry", "agriculture",
			"energy", "renewable", "carbon", "emissions", "green",
		},
	},
	{
		Slug:  "literature-writing",
		Label: "Literature & Writing",
		keywords: []string{
			"literature", "literary", "writing", "author", "writer", "book",
			"poetry", "poem", "fiction", "novel", "reading", "publishing",
			"storytelling", "narrative", "words", "language", "linguistics",
		},
	},
	{
		Slug:  "health-medicine",
		Label: "Health & Medicine",
		keywords: []string{
			"health", "medicine", "medical", "clinical", "wellbeing",
			"mental health", "psychology", "psychiatry", "neurology",
			"nutrition", "public health", "disease", "treatment", "therapy",
			"healthcare", "nursing", "pharmacy", "pathology",
		},
	},
	{
		Slug:  "politics-policy",
		Label: "Politics & Policy",
		keywords: []string{
			"politics", "political", "policy", "government", "democracy",
			"law", "legal", "justice", "rights", "economics", "economy",
			"finance", "trade", "geopolitics", "international", "diplomacy",
			"public policy", "legislation", "regulation", "treaty",
		},
	},
	{
		Slug:  "philosophy-ideas",
		Label: "Philosophy & Ideas",
		keywords: []string{
			"philosophy", "philosophical", "ethics", "ethics", "moral",
			"ideas", "theory", "thinking", "mind", "consciousness",
			"metaphysics", "logic", "epistemology", "religion", "theology",
			"spirituality", "meaning", "humanity",
		},
	},
}

// Infer returns a list of topic slugs that match the given title and summary.
// Returns at most 3 tags to avoid over-tagging.
func Infer(title, summary string) []string {
	text := strings.ToLower(title + " " + summary)
	var matched []string
	for _, t := range topics {
		for _, kw := range t.keywords {
			if strings.Contains(text, kw) {
				matched = append(matched, t.Slug)
				break
			}
		}
		if len(matched) == 3 {
			break
		}
	}
	return matched
}

// All returns all known topics (for UI rendering).
func All() []Topic {
	return topics
}

// LabelFor returns the display label for a given slug.
func LabelFor(slug string) string {
	for _, t := range topics {
		if t.Slug == slug {
			return t.Label
		}
	}
	return slug
}
