// Package aucklandmuseum scrapes public events from Auckland War Memorial Museum.
//
// Two listing pages are scraped:
//
//  1. https://www.aucklandmuseum.com/visit/whats-on
//     Server-rendered ASP.NET. Cards use the marker "columns one-third" with <h3> titles.
//
//  2. https://www.aucklandmuseum.com/visit/whats-on/evenings
//     Same server, different layout. Cards use the marker "four columns alpha" with <h2> titles.
//
// Both share the same date/place subtitle format: "TUE 14 APR, 6PM - 7.30PM".
//
// All Auckland Museum events default to Cost="Ticketed" (paid admission or ticketed evening).
// If the card description explicitly mentions "free" the event is marked Free=true instead.
package aucklandmuseum

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
	listingURL   = "https://www.aucklandmuseum.com/visit/whats-on"
	eveningsURL  = "https://www.aucklandmuseum.com/visit/whats-on/evenings"
	baseURL      = "https://www.aucklandmuseum.com"
	venueDefault = "Auckland War Memorial Museum, The Domain, Parnell, Auckland"
)

// Scraper implements scraper.Scraper for Auckland War Memorial Museum.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "auckland-museum",
		Name:        "Auckland War Memorial Museum",
		Website:     listingURL,
		Description: "Tāmaki Paenga Hira Auckland War Memorial Museum hosts public talks, evening events, and cultural programmes exploring Aotearoa's natural and human history.",
	}
}

var (
	// catRe extracts the category text from <a class="cat">.
	catRe = regexp.MustCompile(`(?i)class="cat"[^>]*>([^<]+)<`)
	// titleH3Re extracts title/href from <h3><a href="…"> (main whats-on page).
	titleH3Re = regexp.MustCompile(`(?i)<h3[^>]*>\s*<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	// titleH2Re extracts title/href from <h2><a href="…"> (evenings page).
	titleH2Re = regexp.MustCompile(`(?i)<h2[^>]*>\s*<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	// dateRe extracts text from subtitle-date div.
	dateRe = regexp.MustCompile(`(?i)class="subtitle subtitle-date"[^>]*>([^<]+)<`)
	// placeRe extracts text from subtitle-place div.
	placeRe = regexp.MustCompile(`(?i)class="subtitle subtitle-place"[^>]*>([^<]+)<`)
	// descRe extracts the first <p> after the extra-info div.
	descRe = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
	// pastRe detects "Past Event" status labels.
	pastRe = regexp.MustCompile(`(?i)past\s+event`)
	// tagRe strips HTML tags.
	tagRe = regexp.MustCompile(`<[^>]+>`)
	// dateParsRe matches "TUE 14 APR" in the date string.
	dateParsRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`)
	// timeParsRe matches a start time like "6PM" or "6.30PM".
	timeParsRe = regexp.MustCompile(`(?i)(\d{1,2})(?:[\.\:](\d{2}))?\s*(am|pm)`)
)

var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

func innerText(html string) string {
	s := tagRe.ReplaceAllString(html, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&ndash;", "–")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// parseEventDate parses "TUE 14 APR, 6PM - 7.30PM".
// Returns zero time if the date is not a specific future date (e.g. "ON NOW", "DAILY …").
func parseEventDate(s string, loc *time.Location) (time.Time, bool) {
	s = strings.ToUpper(s)
	// Reject recurring/undated strings.
	for _, skip := range []string{"ON NOW", "DAILY", "EVERY ", "WEEKENDS", "ONGOING", "SOLD OUT"} {
		if strings.Contains(s, skip) {
			return time.Time{}, false
		}
	}

	dm := dateParsRe.FindStringSubmatch(s)
	if dm == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(dm[1])
	month, ok := monthMap[strings.ToLower(dm[2])]
	if !ok {
		return time.Time{}, false
	}

	now := time.Now().In(loc)
	year := now.Year()
	candidate := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if candidate.Before(now) {
		bumped := time.Date(year+1, month, day, 0, 0, 0, 0, loc)
		if bumped.Sub(now) <= 90*24*time.Hour {
			year++
		}
	}

	// Take start time — the text before any " - ".
	timeStr := s
	if idx := strings.Index(s, " - "); idx != -1 {
		timeStr = s[:idx]
	}
	hour, min := 18, 0 // default 6pm for evening events
	if tm := timeParsRe.FindStringSubmatch(timeStr); tm != nil {
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

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	seen := make(map[string]bool)
	var lectures []model.Lecture

	// Main what's-on page: cards split by "columns one-third", titles in <h3>.
	if lecs, err := scrapeListingPage(ctx, listingURL, "columns one-third", titleH3Re, true, loc, seen); err != nil {
		return nil, fmt.Errorf("auckland-museum: %w", err)
	} else {
		lectures = append(lectures, lecs...)
	}

	// Evenings page: cards split by "four columns alpha", titles in <h2>.
	if lecs, err := scrapeListingPage(ctx, eveningsURL, "four columns alpha", titleH2Re, false, loc, seen); err != nil {
		// Non-fatal — log and continue with what we have.
		fmt.Printf("auckland-museum: evenings fetch error: %v\n", err)
	} else {
		lectures = append(lectures, lecs...)
	}

	return lectures, nil
}

// scrapeListingPage fetches a single listing page and parses event cards.
// requireEventCat: if true, only include cards with category "Event" (main page behaviour).
func scrapeListingPage(ctx context.Context, url, cardMarker string, titleRe *regexp.Regexp, requireEventCat bool, loc *time.Location, seen map[string]bool) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	chunks := strings.Split(string(body), cardMarker)
	var lectures []model.Lecture

	for _, chunk := range chunks[1:] {
		// Skip past events.
		if pastRe.MatchString(chunk) {
			continue
		}

		// Filter by category on the main page: include lecture/event categories,
		// exclude non-lecture types like exhibitions, tours, and kids programmes.
		if requireEventCat {
			catM := catRe.FindStringSubmatch(chunk)
			cat := ""
			if catM != nil {
				cat = strings.ToLower(strings.TrimSpace(catM[1]))
			}
			switch cat {
			case "exhibition", "tour", "kids and family", "":
				continue
			}
		}

		titleM := titleRe.FindStringSubmatch(chunk)
		if titleM == nil {
			continue
		}
		href := titleM[1]
		title := innerText(titleM[2])
		if title == "" || seen[href] {
			continue
		}

		dateM := dateRe.FindStringSubmatch(chunk)
		if dateM == nil {
			continue
		}
		t, ok := parseEventDate(strings.TrimSpace(dateM[1]), loc)
		if !ok {
			continue
		}
		if t.Before(time.Now()) {
			continue
		}

		seen[href] = true
		link := baseURL + href

		location := venueDefault
		if placeM := placeRe.FindStringSubmatch(chunk); placeM != nil {
			place := strings.TrimSpace(placeM[1])
			if place != "" && !strings.EqualFold(place, "sold out") {
				location = place + ", Auckland War Memorial Museum"
			}
		}

		summary := ""
		if dm := descRe.FindStringSubmatch(chunk); dm != nil {
			summary = innerText(dm[1])
		}

		// Default to ticketed; override only if explicitly described as free.
		free := false
		cost := "Ticketed"
		if strings.Contains(strings.ToLower(summary), "free") || strings.Contains(strings.ToLower(title), "free") {
			free = true
			cost = ""
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(link),
			Title:       scraper.CleanTitle(title),
			Link:        link,
			TimeStart:   t,
			Description: summary,
			Summary:     scraper.TruncateSummary(summary, 200),
			Location:    location,
			Free:        free,
			Cost:        cost,
			HostSlug:    "auckland-museum",
		})
	}

	return lectures, nil
}
