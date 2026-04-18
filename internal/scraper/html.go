package scraper

import (
	"strconv"
	"strings"
	"time"
)

// NZLocation is the New Zealand timezone (Pacific/Auckland), used for
// formatting and parsing event times. Falls back to UTC if the timezone
// database is unavailable.
var NZLocation = func() *time.Location {
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		return time.UTC
	}
	return loc
}()

// InnerText strips HTML tags, decodes common entities, and normalises
// whitespace. Use this when extracting visible text from an HTML fragment.
func InnerText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&#039;", "'")
	s = strings.ReplaceAll(s, "&#8217;", "\u2019")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&ndash;", "–")
	s = strings.ReplaceAll(s, "&mdash;", "—")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// FullMonthMap maps lowercase full month names to time.Month values.
var FullMonthMap = map[string]time.Month{
	"january": time.January, "february": time.February, "march": time.March,
	"april": time.April, "may": time.May, "june": time.June,
	"july": time.July, "august": time.August, "september": time.September,
	"october": time.October, "november": time.November, "december": time.December,
}

// AbbrevMonthMap maps lowercase 3-letter month abbreviations to time.Month values.
var AbbrevMonthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// ParseTime12h parses a 12-hour time string like "10:00am", "12:30pm", "5pm".
// Returns hour and minute in 24-hour format, and whether parsing succeeded.
func ParseTime12h(s string) (hour, min int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, 0, false
	}
	isPM := strings.HasSuffix(s, "pm")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "pm"), "am")
	parts := strings.SplitN(s, ":", 2)
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	mn := 0
	if len(parts) == 2 {
		mn, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	if isPM && h != 12 {
		h += 12
	} else if !isPM && h == 12 {
		h = 0
	}
	return h, mn, true
}
