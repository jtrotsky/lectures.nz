package scraper

import (
	"strings"
	"unicode"
)

// CleanTitle normalises a raw scraped title:
//  1. Strips trailing em/en dash lines (Studio One appends "\n—")
//  2. Converts ALL-CAPS titles to title case
//  3. Strips a trailing parenthetical "(…)" when the base title is already long enough
//  4. Strips trailing truncation artefacts like "F..." or ", Fri..."
//
// The function is intentionally conservative — it only changes what it can
// identify as artefacts, leaving unusual but correct titles alone.
func CleanTitle(title string) string {
	// 1. Strip trailing em/en dash and surrounding whitespace.
	//    Studio One titles arrive as "TITLE\n—" or "TITLE \n —".
	title = strings.TrimRight(title, " \t\n\r–—")
	title = strings.TrimSpace(title)

	// 2. Convert ALL-CAPS titles to title case.
	if isAllCaps(title) {
		title = toTitleCase(title)
	}

	// 3. Strip trailing parenthetical if the base is already substantial.
	//    "Work-Integrated Learning Research: ... (WACE International Research Symposium)"
	if idx := lastParenIdx(title); idx > 0 && idx >= 30 {
		title = strings.TrimSpace(title[:idx])
	}

	// 4. Strip descriptive subtitle after " - " when title is already long.
	//    "Mandarin Corner: Tai Chi - immerse yourself in traditional…" → "Mandarin Corner: Tai Chi"
	title = trimDescriptiveSuffix(title)

	// 5. Strip trailing truncation artefacts: anything after the last
	//    separator if the tail ends with "..." or similar.
	title = stripTruncation(title)

	return strings.TrimSpace(title)
}

// TruncateSummary shortens a summary to at most maxLen characters, always
// ending on a complete sentence.  If no sentence boundary exists before maxLen
// it extends up to maxLen+200 to reach the next one.  If still none found it
// returns the full string — never a mid-sentence truncation with "…".
func TruncateSummary(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	// Find the last sentence end (. ! ?) at or before maxLen.
	for i := maxLen - 1; i >= 0; i-- {
		if s[i] == '.' || s[i] == '!' || s[i] == '?' {
			return strings.TrimSpace(s[:i+1])
		}
	}
	// No sentence boundary before maxLen — extend to the next one.
	for i := maxLen; i < len(s) && i < maxLen+200; i++ {
		if s[i] == '.' || s[i] == '!' || s[i] == '?' {
			return strings.TrimSpace(s[:i+1])
		}
	}
	// No sentence boundary anywhere in range — return the full summary.
	return s
}

// isAllCaps returns true when more than 70 % of the letter runes in s are
// uppercase.  Short strings (< 4 letters) are excluded to avoid false positives
// on abbreviations.
func isAllCaps(s string) bool {
	var up, lo int
	for _, r := range s {
		if unicode.IsUpper(r) {
			up++
		} else if unicode.IsLower(r) {
			lo++
		}
	}
	total := up + lo
	if total < 4 {
		return false
	}
	return float64(up)/float64(total) > 0.7
}

// smallWords is the set of words that stay lowercase in title case
// (unless they're the first word).
var smallWords = map[string]bool{
	"a": true, "an": true, "the": true,
	"and": true, "or": true, "but": true, "nor": true,
	"at": true, "by": true, "for": true, "in": true,
	"of": true, "on": true, "to": true, "up": true,
	"as": true, "from": true, "into": true, "with": true,
}

// toTitleCase converts s to title case, keeping small words lowercase except
// at the start of the string or after a colon.
func toTitleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		lower := strings.ToLower(w)
		if i == 0 || !smallWords[lower] {
			words[i] = capitalise(w)
		} else {
			words[i] = lower
		}
		// After a colon or dash the next word gets capitalised.
		if i > 0 {
			prev := words[i-1]
			if strings.HasSuffix(prev, ":") || strings.HasSuffix(prev, "—") || strings.HasSuffix(prev, "–") || prev == "—" || prev == "–" {
				words[i] = capitalise(w)
			}
		}
	}
	return strings.Join(words, " ")
}

// capitalise uppercases the first rune and lowercases the rest.
func capitalise(w string) string {
	if w == "" {
		return w
	}
	runes := []rune(w)
	return string(unicode.ToUpper(runes[0])) + strings.ToLower(string(runes[1:]))
}

// lastParenIdx returns the byte index of the opening "(" of the last
// parenthetical group that closes at the end of the string, or 0 if none.
func lastParenIdx(s string) int {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, ")") {
		return 0
	}
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return 0
}

// trimDescriptiveSuffix removes " - <descriptive phrase>" from long titles.
// It only acts when:
//   - The title is over 70 characters, AND
//   - The base before " - " is at least 25 characters
//
// This catches patterns like "Mandarin Corner: Baduanjin wellness - explore
// the philosophy of baduanjin…" while leaving short titles like
// "WEN Outreach Workshop - Survive the Shake!" intact.
func trimDescriptiveSuffix(title string) string {
	if len(title) <= 70 {
		return title
	}
	const sep = " - "
	idx := strings.Index(title, sep)
	if idx < 25 {
		return title
	}
	return strings.TrimSpace(title[:idx])
}

// SplitTitleSpeaker splits a title of the form "Event Title | Speaker Name"
// into (title, speakerSuffix). If no pipe separator is present, speakerSuffix
// is empty. The caller decides what to do with the speaker suffix (e.g. parse
// it into a Speaker struct). Only splits on " | " with surrounding spaces to
// avoid false positives on titles that use | as a bullet.
func SplitTitleSpeaker(title string) (string, string) {
	const sep = " | "
	idx := strings.Index(title, sep)
	if idx < 5 {
		return title, ""
	}
	return strings.TrimSpace(title[:idx]), strings.TrimSpace(title[idx+len(sep):])
}

// stripTruncation removes trailing scraper artefacts like ", Fri..." or " F..."
// that arise when a source page truncates its title text.
func stripTruncation(s string) string {
	if !strings.HasSuffix(s, "...") && !strings.HasSuffix(s, "…") {
		return s
	}
	// Walk backwards past the "..." to find the last real separator (, - |)
	// and drop everything from there.
	end := strings.TrimRight(s, ".…")
	for _, sep := range []string{", ", " - ", " | ", " — "} {
		if idx := strings.LastIndex(end, sep); idx > 0 {
			return strings.TrimSpace(end[:idx])
		}
	}
	// No clean separator — just drop the ellipsis tail.
	return strings.TrimSpace(end)
}
