// Package royalsociety scrapes public events from Royal Society Te Apārangi.
//
// The events listing at /events renders cards as:
//
//	<div class="important-image-container ..." data-region-type="..." data-end="Month-YYYY">
//	  <a href="/events/{slug}" class="important-image" ...>
//	    <h2 class="title">Event Title</h2>
//	    <p class="text">Short description</p>
//	  </a>
//	</div>
//
// Each event detail page provides:
//
//	<h1 class="page-heading">Title</h1>
//	<div class="intro-content typography"><p>Description</p></div>
//	<div class="venue">
//	  <p class="speak-large">Venue address</p>
//	  <p class="speak-large">6:15pm Tue 21 April, 2026 - 7:45pm Tue 21 April, 2026</p>
//	</div>
package royalsociety

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

const (
	listingURL = "https://www.royalsociety.org.nz/events"
	baseURL    = "https://www.royalsociety.org.nz"
)

// Scraper implements scraper.Scraper for Royal Society Te Apārangi.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "royal-society",
		Name:        "Royal Society Te Apārangi",
		Website:     listingURL,
		Description: "Royal Society Te Apārangi hosts public events exploring science, research, and ideas shaping New Zealand and the world.",
	}
}

var (
	// cardHrefRe extracts the event href from an important-image anchor.
	cardHrefRe = regexp.MustCompile(`(?i)<a\s+href="(/events/[^"]+)"\s+class="important-image"`)

	// Detail page regexes.
	titleRe   = regexp.MustCompile(`(?i)<h1[^>]*class="page-heading"[^>]*>([^<]+)</h1>`)
	introRe   = regexp.MustCompile(`(?i)class="intro-content[^"]*"[^>]*>\s*<p[^>]*>([\s\S]*?)</p>`)
	venueRe = regexp.MustCompile(`(?i)<p[^>]*class="speak-large"[^>]*>([\s\S]*?)</p>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)

	// dateParsRe parses "21 April, 2026" or "21 April 2026".
	dateParsRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(January|February|March|April|May|June|July|August|September|October|November|December),?\s+(\d{4})`)
	// timeParsRe parses "6:15pm" or "6pm".
	timeParsRe = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(am|pm)`)
)

var fullMonthMap = map[string]time.Month{
	"january": time.January, "february": time.February, "march": time.March,
	"april": time.April, "may": time.May, "june": time.June,
	"july": time.July, "august": time.August, "september": time.September,
	"october": time.October, "november": time.November, "december": time.December,
}

func innerText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// parseEventDate parses "6:15pm Tue 21 April, 2026".
func parseEventDate(s string, loc *time.Location) (time.Time, bool) {
	dm := dateParsRe.FindStringSubmatch(s)
	if dm == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(dm[1])
	month, ok := fullMonthMap[strings.ToLower(dm[2])]
	if !ok {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(dm[3])

	hour, min := 18, 0 // default 6pm
	if tm := timeParsRe.FindStringSubmatch(s); tm != nil {
		h, _ := strconv.Atoi(tm[1])
		mn := 0
		if tm[2] != "" {
			mn, _ = strconv.Atoi(tm[2])
		}
		period := strings.ToLower(tm[3])
		if period == "pm" && h != 12 {
			h += 12
		} else if period == "am" && h == 12 {
			h = 0
		}
		hour, min = h, mn
	}
	return time.Date(year, month, day, hour, min, 0, 0, loc), true
}

const cardMarker = `class="important-image-container`

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("royal-society: fetch listing: %w", err)
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	seen := make(map[string]bool)
	var hrefs []string

	for _, chunk := range strings.Split(string(body), cardMarker) {
		m := cardHrefRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		href := m[1]
		if !seen[href] {
			seen[href] = true
			hrefs = append(hrefs, href)
		}
	}

	var lectures []model.Lecture
	for _, href := range hrefs {
		link := baseURL + href
		lec, ok := fetchDetail(ctx, link, loc)
		if !ok {
			continue
		}
		lectures = append(lectures, lec)
	}
	return lectures, nil
}

func fetchDetail(ctx context.Context, link string, loc *time.Location) (model.Lecture, bool) {
	body, err := scraper.Fetch(ctx, link)
	if err != nil {
		return model.Lecture{}, false
	}
	html := string(body)

	titleM := titleRe.FindStringSubmatch(html)
	if titleM == nil {
		return model.Lecture{}, false
	}
	title := strings.TrimSpace(titleM[1])

	// Scope venue/datetime extraction to the div.venue block.
	venueBlock := html
	if idx := strings.Index(html, `class="venue"`); idx != -1 {
		venueBlock = html[idx:]
		// Trim to a reasonable window (the venue block is compact).
		if len(venueBlock) > 1000 {
			venueBlock = venueBlock[:1000]
		}
	}

	// Extract speak-large paragraphs: first = venue address, second = datetime.
	venueMatches := venueRe.FindAllStringSubmatch(venueBlock, -1)
	if len(venueMatches) < 2 {
		return model.Lecture{}, false
	}
	venue := strings.TrimSpace(innerText(venueMatches[0][1]))
	dateStr := strings.TrimSpace(innerText(venueMatches[1][1]))

	t, ok := parseEventDate(dateStr, loc)
	if !ok {
		return model.Lecture{}, false
	}
	if t.Before(time.Now().In(loc)) {
		return model.Lecture{}, false
	}

	var summary string
	if im := introRe.FindStringSubmatch(html); im != nil {
		summary = innerText(im[1])
	}

	return model.Lecture{
		ID:        scraper.MakeID(link),
		Title:     scraper.CleanTitle(title),
		Link:      link,
		TimeStart: t,
		Description: summary,
		Summary:     scraper.TruncateSummary(summary, 200),
		Location:    venue,
		HostSlug:    "royal-society",
	}, true
}
