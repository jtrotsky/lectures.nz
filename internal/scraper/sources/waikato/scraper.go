// Package waikato scrapes public lecture events from the University of Waikato.
//
// The university runs two free public lecture series:
//
//   - Hamilton Public Lectures: https://www.waikato.ac.nz/news-events/events/lecture-series/hamilton-public-lectures/
//   - Tauranga Public Lectures: https://www.waikato.ac.nz/news-events/events/lecture-series/tauranga-public-lectures/
//
// Each listing page links to individual event pages under:
//
//	https://www.waikato.ac.nz/news-events/events/find-event/{slug}/
//
// Event pages contain structured HTML (class="event container") with date, time,
// location, description, and an optional Eventbrite registration link.
package waikato

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const baseURL = "https://www.waikato.ac.nz"

// lectureSeriesPages lists the public lecture series listing pages to scrape.
var lectureSeriesPages = []string{
	baseURL + "/news-events/events/lecture-series/hamilton-public-lectures/",
	baseURL + "/news-events/events/lecture-series/tauranga-public-lectures/",
}

// Scraper implements scraper.Scraper for University of Waikato.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "waikato",
		Name:        "University of Waikato",
		Website:     "https://www.waikato.ac.nz/news-events/events/lecture-series/",
		Description: "The University of Waikato hosts free public lectures in Hamilton and Tauranga, featuring professorial lectures, research talks, and community events across its campuses.",
	}
}

var (
	findEventRe  = regexp.MustCompile(`href="(https://www\.waikato\.ac\.nz/news-events/events/find-event/[^"]+)"`)
	h1Re         = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	infoListRe   = regexp.MustCompile(`(?s)<ul class="event__info-list">(.*?)</ul>`)
	liRe         = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	descParaRe   = regexp.MustCompile(`(?s)<p class="restricted-width-element">(.*?)</p>`)
	eventbriteRe = regexp.MustCompile(`(?i)href="(https?://[^"]*eventbrite[^"]*)"`)
	tagRe        = regexp.MustCompile(`<[^>]+>`)
	// timeItemRe matches time strings like "5.30pm", "1pm", "10am - 2pm", "5:30 PM".
	timeItemRe = regexp.MustCompile(`(?i)\b\d{1,2}[.:]\d{2}\s*[ap]m|\b\d{1,2}\s*[ap]m`)
)

// studentKeywords identifies events aimed at secondary school students rather
// than the general public.
var studentKeywords = []string{
	"year 11", "year 12", "year 13",
	"years 11", "years 12", "years 13",
	"secondary student", "secondary school",
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	seen := make(map[string]bool)
	var lectures []model.Lecture

	for _, listURL := range lectureSeriesPages {
		body, err := scraper.Fetch(ctx, listURL)
		if err != nil {
			// Log but don't abort — one listing page failing shouldn't drop the other.
			fmt.Printf("waikato: fetch listing %s: %v\n", listURL, err)
			continue
		}
		matches := findEventRe.FindAllSubmatch(body, -1)
		for _, m := range matches {
			eventURL := string(m[1])
			if seen[eventURL] {
				continue // deduplicate across listing pages
			}
			seen[eventURL] = true

			l, include, err := scrapeEvent(ctx, eventURL)
			if err != nil {
				fmt.Printf("waikato: skip %s: %v\n", eventURL, err)
				continue
			}
			if include {
				lectures = append(lectures, l)
			}
		}
	}
	return lectures, nil
}

// scrapeEvent fetches an individual event detail page and parses it.
// Returns (lecture, true, nil) to include, (_, false, nil) to silently skip,
// or (_, false, err) on a fetch/parse failure worth logging.
func scrapeEvent(ctx context.Context, eventURL string) (model.Lecture, bool, error) {
	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return model.Lecture{}, false, err
	}
	html := string(body)

	// Title.
	titleMatch := h1Re.FindStringSubmatch(html)
	if titleMatch == nil {
		return model.Lecture{}, false, fmt.Errorf("no h1 title found")
	}
	title := cleanText(titleMatch[1])
	if title == "" {
		return model.Lecture{}, false, fmt.Errorf("empty title")
	}

	// Info list: items are in order date, time, location, organizer, email, cost.
	infoMatch := infoListRe.FindStringSubmatch(html)
	if infoMatch == nil {
		return model.Lecture{}, false, fmt.Errorf("no event__info-list found")
	}
	liMatches := liRe.FindAllStringSubmatch(infoMatch[1], -1)
	var items []string
	for _, li := range liMatches {
		items = append(items, cleanText(li[1]))
	}
	if len(items) < 2 {
		return model.Lecture{}, false, fmt.Errorf("too few info items (%d)", len(items))
	}

	// Items appear in a consistent set but ordering can vary slightly (time is
	// omitted when not specified). Detect each item by its content.
	dateStr, timeStr, location, free := classifyInfoItems(items)
	if dateStr == "" {
		return model.Lecture{}, false, fmt.Errorf("no date found in info items: %v", items)
	}

	t, err := parseDatetime(dateStr, timeStr)
	if err != nil {
		return model.Lecture{}, false, fmt.Errorf("parse datetime %q / %q: %w", dateStr, timeStr, err)
	}

	// Append city to location when not already present.
	if city := inferCity(location, html); city != "" && !strings.Contains(strings.ToLower(location), strings.ToLower(city)) {
		location = location + ", " + city
	}

	// Description from the first content paragraph.
	desc := ""
	if descMatch := descParaRe.FindStringSubmatch(html); descMatch != nil {
		desc = cleanText(descMatch[1])
	}

	// Skip events targeted at secondary school students.
	combined := strings.ToLower(title + " " + desc)
	for _, kw := range studentKeywords {
		if strings.Contains(combined, kw) {
			return model.Lecture{}, false, nil
		}
	}

	// Registration link: prefer Eventbrite when present, otherwise link to the
	// canonical Waikato event page.
	link := eventURL
	if ebMatch := eventbriteRe.FindStringSubmatch(html); ebMatch != nil {
		link = ebMatch[1]
	}

	return model.Lecture{
		ID:          scraper.MakeID(eventURL), // stable: keyed to the Waikato page URL
		Title:       scraper.CleanTitle(title),
		Link:        link,
		TimeStart:   t,
		Description: desc,
		Summary:     scraper.TruncateSummary(desc, 200),
		Location:    location,
		Free:        free,
		HostSlug:    "waikato",
	}, true, nil
}

// classifyInfoItems inspects each <li> text from the event__info-list and
// categorises it into date, time, location, and cost. The list order is mostly
// consistent but the time item is sometimes absent, so we classify by content.
func classifyInfoItems(items []string) (dateStr, timeStr, location string, free bool) {
	for _, s := range items {
		lower := strings.ToLower(s)
		switch {
		case isDateItem(s):
			dateStr = s
		case timeItemRe.MatchString(s):
			timeStr = s
		case strings.Contains(lower, "free"):
			free = true
		case strings.HasPrefix(s, "$"):
			// paid event — free stays false
		case strings.Contains(lower, "@"):
			// email — skip
		case s == "University of Waikato":
			// organizer label — skip unless we have no location yet
			if location == "" {
				location = s
			}
		default:
			// First unclassified item that isn't the organiser or an email is the venue.
			if location == "" && s != "" {
				location = s
			}
		}
	}
	return
}

// isDateItem returns true when s looks like a date ("12 May 2026", "Tuesday 12 May 2026").
func isDateItem(s string) bool {
	for month := range scraper.FullMonthMap {
		if strings.Contains(strings.ToLower(s), month) {
			return true
		}
	}
	return false
}

// parseDatetime parses "Tuesday 12 May 2026" + "5.30pm - 6.30pm" into a time.Time.
// The weekday prefix is optional. The time string uses "." as the colon separator.
func parseDatetime(dateStr, timeStr string) (time.Time, error) {
	parts := strings.Fields(dateStr)
	// Strip leading weekday (e.g. "Tuesday") if present.
	if len(parts) >= 4 {
		parts = parts[1:]
	}
	if len(parts) < 3 {
		return time.Time{}, fmt.Errorf("expected at least 3 date parts, got %d", len(parts))
	}

	day, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("bad day %q: %w", parts[0], err)
	}
	month, ok := scraper.FullMonthMap[strings.ToLower(parts[1])]
	if !ok {
		return time.Time{}, fmt.Errorf("unknown month %q", parts[1])
	}
	year, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("bad year %q: %w", parts[2], err)
	}

	hour, min := 0, 0
	if timeStr != "" {
		// Take the start time from "5.30pm - 6.30pm".
		start := strings.SplitN(timeStr, " - ", 2)[0]
		// Waikato uses "." as separator: "5.30pm" → "5:30pm".
		start = strings.ReplaceAll(start, ".", ":")
		if h, m, ok := scraper.ParseTime12h(start); ok {
			hour, min = h, m
		}
	}

	return time.Date(year, month, day, hour, min, 0, 0, scraper.NZLocation), nil
}

// inferCity returns "Hamilton" or "Tauranga" from the location string or page HTML.
// Returns "" when the city can't be determined.
func inferCity(location, html string) string {
	loc := strings.ToLower(location)
	if strings.Contains(loc, "hamilton") || strings.Contains(loc, "waikato") {
		return "Hamilton"
	}
	if strings.Contains(loc, "tauranga") {
		return "Tauranga"
	}
	// Fall back to the page context (tags and breadcrumb text).
	lower := strings.ToLower(html)
	if strings.Contains(lower, "tauranga public lecture") {
		return "Tauranga"
	}
	if strings.Contains(lower, "hamilton public lecture") || strings.Contains(lower, "hamilton campus") {
		return "Hamilton"
	}
	return ""
}

// cleanText strips HTML tags and normalises whitespace.
func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "&rsquo;", "\u2019")
	s = strings.ReplaceAll(s, "&lsquo;", "\u2018")
	s = strings.ReplaceAll(s, "&ldquo;", "\u201C")
	s = strings.ReplaceAll(s, "&rdquo;", "\u201D")
	s = strings.ReplaceAll(s, "&ndash;", "–")
	s = strings.ReplaceAll(s, "&mdash;", "—")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
