// Package artgallerynz scrapes public events from artgallery.org.nz.
//
// The site is a Shopify store. The events page at /pages/events lists products
// with a card-per-event layout. Each card has an image link, then an h4 with
// a title span and a date span (e.g. "Fri 10 Apr, 10.30am–12pm").
//
// We split the listing page by the image-link class marker to isolate cards,
// then extract slug, title, and date string from each chunk.
package artgallerynz

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
	listingURL = "https://artgallery.org.nz/pages/events"
	baseURL    = "https://artgallery.org.nz"
)

// Scraper implements scraper.Scraper for artgallery.org.nz.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "artgallery-nz",
		Name:        "Art Gallery New Zealand",
		Website:     listingURL,
		Description: "Art Gallery New Zealand hosts public talks, workshops, and events celebrating art and culture.",
	}
}

var (
	// productLinkRe extracts /products/{slug} hrefs.
	productLinkRe = regexp.MustCompile(`href="/products/([^"?#/]+)"`)
	// titleSpanRe extracts the text from the "font-medium" title span.
	titleSpanRe = regexp.MustCompile(`(?i)class="font-medium[^"]*"[^>]*>\s*([^<]+?)\s*<`)
	// dateSpanRe extracts the text from the "block text-sm" date span.
	dateSpanRe = regexp.MustCompile(`(?i)class="block text-sm[^"]*"[^>]*>\s*([^<]+?)\s*<`)
	// datePart extracts day number and month abbreviation from the date string.
	datePart = regexp.MustCompile(`(?i)(\d{1,2})\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`)
	// timePart extracts start time, e.g. "10.30am" (before "–").
	timePart = regexp.MustCompile(`(?i)(\d{1,2})(?:\.(\d{2}))?\s*(am|pm)`)
	// jsonLdDescRe extracts the description from JSON-LD structured data.
	jsonLdDescRe = regexp.MustCompile(`"description"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// parseEventDate parses a date string like "Fri 10 Apr, 10.30am–12pm".
func parseEventDate(s string, loc *time.Location) (time.Time, bool) {
	// Take only text before a newline (multiple dates listed per event).
	s = strings.Split(s, "\n")[0]

	dm := datePart.FindStringSubmatch(s)
	if dm == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(dm[1])
	month, ok := monthMap[strings.ToLower(dm[2])]
	if !ok {
		return time.Time{}, false
	}

	// Infer year: use current year, bump to next if date already passed.
	now := time.Now().In(loc)
	year := now.Year()
	candidate := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if candidate.Before(now) {
		bumped := time.Date(year+1, month, day, 0, 0, 0, 0, loc)
		if bumped.Sub(now) <= 90*24*time.Hour {
			year++
		}
	}

	// Extract start time (take first match, before any "–").
	timeStr := s
	if idx := strings.IndexAny(s, "–-"); idx != -1 {
		timeStr = s[:idx]
	}
	hour, min := 10, 0 // default 10am
	if tm := timePart.FindStringSubmatch(timeStr); tm != nil {
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

// fetchSummary fetches a product page and extracts the description from JSON-LD.
func fetchSummary(ctx context.Context, url string) string {
	body, err := scraper.Fetch(ctx, url)
	if err != nil {
		return ""
	}
	m := jsonLdDescRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	unquoted, err := strconv.Unquote(`"` + string(m[1]) + `"`)
	if err != nil {
		return string(m[1])
	}
	return unquoted
}

// cardMarker is a unique substring that starts each event card's image link.
const cardMarker = `group/slide-image`

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("artgallery-nz: fetch listing: %w", err)
	}

	loc := scraper.NZLocation

	// Split by the image-link class marker; each chunk is one card.
	chunks := strings.Split(string(body), cardMarker)

	seen := make(map[string]bool)
	var lectures []model.Lecture

	for _, chunk := range chunks[1:] { // skip preamble
		slugM := productLinkRe.FindStringSubmatch(chunk)
		if slugM == nil {
			continue
		}
		slug := slugM[1]
		if seen[slug] {
			continue
		}
		seen[slug] = true

		titleM := titleSpanRe.FindStringSubmatch(chunk)
		if titleM == nil {
			continue
		}
		title := strings.TrimSpace(titleM[1])
		if title == "" {
			continue
		}

		dateM := dateSpanRe.FindStringSubmatch(chunk)
		if dateM == nil {
			continue
		}
		dateStr := strings.TrimSpace(dateM[1])

		t, ok := parseEventDate(dateStr, loc)
		if !ok {
			continue
		}

		link := baseURL + "/products/" + slug
		rawDesc := fetchSummary(ctx, link)
		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(link),
			Title:       scraper.CleanTitle(title),
			Link:        link,
			TimeStart:   t,
			Description: rawDesc,
			Summary:     scraper.TruncateSummary(rawDesc, 200),
			HostSlug:    "artgallery-nz",
		})
	}

	return lectures, nil
}
