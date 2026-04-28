package scraper

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

// garbagePatterns are substrings that indicate the extracted text is navigation,
// error, or bot-detection content rather than an event description.
var garbagePatterns = []string{
	"you are using an outdated browser",
	"please upgrade your browser",
	"javascript is required",
	"enable javascript",
	"cookies are required",
	"page not found",
	"access denied",
	"login to continue",
	// Victoria University nav
	"the menu is here now",
	"apply to study",
	"close close menu",
	// Royal Society / generic breadcrumb nav
	"share our content",
	// Eventbrite UI
	"find events",
	"create events",
	"sign in",
}

// LooksLikeGarbage returns true when s appears to be navigation or error content
// rather than a real event description.
func LooksLikeGarbage(s string) bool {
	lower := strings.ToLower(s)
	for _, pat := range garbagePatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// ExtractDescription attempts to extract a meaningful description from a raw
// HTML page body. It tries structured sources first (JSON-LD, meta tags), then
// falls back to first-paragraph extraction only if nothing better was found.
//
// Priority order (highest first):
//  1. JSON-LD Event / Article "description" field
//  2. OpenGraph og:description meta tag
//  3. HTML meta name="description" tag
//  4. First paragraph with > 80 characters (fallback only)
func ExtractDescription(body []byte) string {
	// Try structured sources in priority order.
	structured := []string{
		strings.TrimSpace(extractJSONLDDescription(body)),
		strings.TrimSpace(extractMetaContent(body, `og:description`)),
		strings.TrimSpace(extractMetaContent(body, `description`)),
	}
	for _, s := range structured {
		if len(s) >= 40 {
			return s
		}
	}
	// Fallback: first substantial paragraph — may be navigation text on some
	// sites, so callers should still run LooksLikeGarbage on the result.
	return strings.TrimSpace(extractFirstParagraph(body))
}

// ldDesc is the minimal shape we need from JSON-LD.
type ldDesc struct {
	Description string `json:"description"`
}

func extractJSONLDDescription(body []byte) string {
	const open = `<script type="application/ld+json">`
	const close = `</script>`
	remaining := body
	best := ""
	for {
		start := bytes.Index(remaining, []byte(open))
		if start < 0 {
			break
		}
		jsonStart := start + len(open)
		end := bytes.Index(remaining[jsonStart:], []byte(close))
		if end < 0 {
			break
		}
		chunk := bytes.TrimSpace(remaining[jsonStart : jsonStart+end])
		remaining = remaining[jsonStart+end:]

		// Try single object.
		var obj ldDesc
		if err := json.Unmarshal(chunk, &obj); err == nil && len(obj.Description) > len(best) {
			best = obj.Description
		}
		// Try array.
		var arr []ldDesc
		if err := json.Unmarshal(chunk, &arr); err == nil {
			for _, o := range arr {
				if len(o.Description) > len(best) {
					best = o.Description
				}
			}
		}
	}
	return best
}

// ebArtistRe extracts the JSON array from Eventbrite's embedded "artistInfo"
// speakers block, which is present in the page HTML for events with speakers.
var ebArtistRe = regexp.MustCompile(`"artistInfo":\{"artistType":"speakers","artists":\[([^\]]+)\]`)
var ebNameRe = regexp.MustCompile(`"name":"([^"]+)"`)

// ExtractEventbriteSpeakers parses the embedded artistInfo JSON from an
// Eventbrite event page — used for events scraped by other hosts (e.g. Auckland
// University) whose detail pages link out to Eventbrite.
func ExtractEventbriteSpeakers(body []byte) []model.Speaker {
	m := ebArtistRe.FindSubmatch(body)
	if m == nil {
		return nil
	}
	var speakers []model.Speaker
	for _, nm := range ebNameRe.FindAllSubmatch(m[1], -1) {
		if name := strings.TrimSpace(string(nm[1])); name != "" {
			speakers = append(speakers, model.Speaker{Name: name})
		}
	}
	return speakers
}

var (
	// presentedByRe matches "Presented by X" — captures the name and everything
	// after it (including any affiliation line) using [\s\S] so CRLF is included.
	presentedByRe = regexp.MustCompile(`(?i)presented by\s+([A-Z][^\n\r<,]{2,60})([\s\S]{0,150})?`)
	// speakersListRe matches a "Speakers include:" paragraph followed by a <ul>.
	speakersListRe = regexp.MustCompile(`(?i)speakers include[^<]*</p>\s*<ul[^>]*>([\s\S]*?)</ul>`)
	// liContentRe extracts Name (bio) from a <li> element.
	liContentRe = regexp.MustCompile(`(?i)<li[^>]*>\s*([^<(]+?)(?:\s*\(([^)]+)\))?\s*</li>`)
	// bioHonourificRe matches a formal name at the start of a bio paragraph, preceded
	// by an honorific. Catches "H.E. Mr Lawrence Meredith is the EU Ambassador..." or
	// "Dr Jane Smith is a visiting researcher..." — the honorific guards against false
	// positives like "New Zealand is the..." or "Auckland Museum is a...".
	bioHonourificRe = regexp.MustCompile(
		`(?m)(?:^|<p[^>]*>)\s*((?:H\.E\.|A/Prof\.?|Dr\.?|Prof(?:essor)?\.?|Ambassador|Sir|Dame)` +
			`(?:\s+(?:Mr|Ms|Mrs|Miss)\.?)?\s+[A-Z][a-zA-Z]+(?:\s+[A-Z][a-zA-Z]+)+)\s+is\b`)
)

// HasSpeakerInfo returns true when text contains speaker-attribution keywords.
func HasSpeakerInfo(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "presented by") ||
		strings.Contains(lower, "speakers include") ||
		strings.Contains(lower, "speaker:")
}

// ExtractSpeakers attempts to pull speaker names and bios from an HTML page body.
// It handles three patterns found on NZ academic event pages:
//  1. "Presented by X, affiliation" — common for single-presenter seminars
//  2. "Speakers include:" followed by <ul><li>Name (bio)</li></ul>
//  3. Bio opening "H.E. Mr Name is the [title]" — Eventbrite and similar bio paragraphs
func ExtractSpeakers(body []byte) []model.Speaker {
	text := string(body)

	// Pattern 1: single presenter via "Presented by" in meta or body.
	if m := presentedByRe.FindStringSubmatch(text); m != nil {
		name := normaliseSpaces(m[1])
		bio := ""
		if len(m) > 2 && m[2] != "" {
			// m[2] is everything after the name — strip leading punctuation/whitespace
			// then take the first clause as the affiliation.
			rest := normaliseSpaces(m[2])
			rest = strings.TrimLeft(rest, " ,;:\r\n")
			// Stop at the next sentence boundary or HTML tag.
			if idx := strings.IndexAny(rest, "<\n"); idx > 0 {
				rest = rest[:idx]
			}
			bio = truncateWords(rest, 6)
		}
		if name != "" {
			return []model.Speaker{{Name: name, Bio: bio}}
		}
	}

	// Pattern 2: multi-speaker list following "Speakers include:".
	if m := speakersListRe.FindStringSubmatch(text); m != nil {
		var speakers []model.Speaker
		for _, li := range liContentRe.FindAllStringSubmatch(m[1], -1) {
			name := normaliseSpaces(tagRe.ReplaceAllString(li[1], ""))
			bio := ""
			if len(li) > 2 {
				bio = truncateWords(normaliseSpaces(li[2]), 6)
			}
			if name != "" {
				speakers = append(speakers, model.Speaker{Name: name, Bio: bio})
			}
		}
		if len(speakers) > 0 {
			return speakers
		}
	}

	// Pattern 3: honorific-prefixed bio opening — "H.E. Mr Lawrence Meredith is the
	// EU Ambassador..." or "Dr Jane Smith is a visiting researcher...".
	if m := bioHonourificRe.FindStringSubmatch(text); m != nil {
		if name := normaliseSpaces(m[1]); name != "" {
			return []model.Speaker{{Name: name}}
		}
	}

	return nil
}

// normaliseSpaces collapses all whitespace to single spaces and trims the result.
// Also handles literal escape sequences (\r\n, \n, \r) that some CMS systems
// embed in HTML meta content attributes as plain text.
func normaliseSpaces(s string) string {
	// Replace literal escape sequences (4-char sequences like backslash-r-backslash-n).
	s = strings.NewReplacer(`\r\n`, " ", `\r`, " ", `\n`, " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// truncateWords limits s to n words, then strips trailing punctuation and
// hanging conjunctions that look odd at a cut point.
func truncateWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) > n {
		words = words[:n]
	}
	result := strings.Join(words, " ")
	// Strip trailing punctuation.
	result = strings.TrimRight(result, " ,;:.")
	// Strip hanging conjunctions left by truncation.
	for _, suffix := range []string{" and", " or", " &"} {
		if strings.HasSuffix(result, suffix) {
			result = strings.TrimRight(result[:len(result)-len(suffix)], " ,;:.")
			break
		}
	}
	return result
}

var (
	// og:description or name="description" meta tags — content may use either
	// quote style; capture up to the closing quote of the same type.
	metaOGRe   = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content="([^"]*)"`)
	metaOGSqRe = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content='([^']*)'`)
	metaNameRe   = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content="([^"]*)"`)
	metaNameSqRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content='([^']*)'`)
	// Reversed attribute order variants.
	metaOGRevRe   = regexp.MustCompile(`(?i)<meta[^>]+content="([^"]+)"[^>]+property=["']og:description["']`)
	metaOGRevSqRe = regexp.MustCompile(`(?i)<meta[^>]+content='([^']+)'[^>]+property=["']og:description["']`)
	metaNameRevRe   = regexp.MustCompile(`(?i)<meta[^>]+content="([^"]+)"[^>]+name=["']description["']`)
	metaNameRevSqRe = regexp.MustCompile(`(?i)<meta[^>]+content='([^']+)'[^>]+name=["']description["']`)
	// Paragraph extractor.
	pTagRe = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
	tagRe  = regexp.MustCompile(`<[^>]+>`)
)

func extractMetaContent(body []byte, prop string) string {
	var candidates []*regexp.Regexp
	switch strings.ToLower(prop) {
	case "og:description":
		candidates = []*regexp.Regexp{metaOGRe, metaOGSqRe, metaOGRevRe, metaOGRevSqRe}
	default: // "description"
		candidates = []*regexp.Regexp{metaNameRe, metaNameSqRe, metaNameRevRe, metaNameRevSqRe}
	}
	for _, re := range candidates {
		if m := re.FindSubmatch(body); m != nil {
			return string(m[1])
		}
	}
	return ""
}

func extractFirstParagraph(body []byte) string {
	matches := pTagRe.FindAllSubmatch(body, -1)
	for _, m := range matches {
		text := strings.TrimSpace(tagRe.ReplaceAllString(string(m[1]), " "))
		text = strings.Join(strings.Fields(text), " ")
		if len(text) > 80 {
			return text
		}
	}
	return ""
}
