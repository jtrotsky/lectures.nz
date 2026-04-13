package scraper

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
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
