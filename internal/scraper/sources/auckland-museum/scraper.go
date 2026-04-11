// Package aucklandmuseum scrapes public events from Auckland War Memorial Museum.
//
// The site is server-rendered ASP.NET. The what's-on page renders event cards as:
//
//	<div class="box generic columns one-third">
//	  <div class="box-thumbnail"><a class="cat">Event</a>...</div>
//	  <span>
//	    <h3><a href="/visit/...">Title</a></h3>
//	    <div class="date-location">
//	      <div class="subtitle subtitle-date">TUE 14 APR, 6PM - 7.30PM</div>
//	      <div class="subtitle subtitle-place">Room, Floor</div>
//	    </div>
//	    <p>Description</p>
//	  </span>
//	</div>
//
// We include only "Event" category cards with a parseable specific date
// (skipping "ON NOW", "DAILY …" and other non-dated formats).
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
	// titleRe extracts the event title and relative href from <h3><a href="…">.
	titleRe = regexp.MustCompile(`(?i)<h3[^>]*>\s*<a\s+href="([^"]+)"[^>]*>([^<]+)</a>`)
	// dateRe extracts text from subtitle-date div.
	dateRe = regexp.MustCompile(`(?i)class="subtitle subtitle-date"[^>]*>([^<]+)<`)
	// placeRe extracts text from subtitle-place div.
	placeRe = regexp.MustCompile(`(?i)class="subtitle subtitle-place"[^>]*>([^<]+)<`)
	// descRe extracts the first <p> inside the span.
	descRe = regexp.MustCompile(`(?i)<span[^>]*>[\s\S]*?<p[^>]*>([\s\S]*?)</p>`)
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
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// parseEventDate parses "TUE 14 APR, 6PM - 7.30PM".
// Returns zero time if the date is not a specific future date (e.g. "ON NOW", "DAILY …").
func parseEventDate(s string, loc *time.Location) (time.Time, bool) {
	s = strings.ToUpper(s)
	// Reject recurring/undated strings.
	for _, skip := range []string{"ON NOW", "DAILY", "EVERY ", "WEEKENDS", "ONGOING"} {
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
	if candidate.Before(now.AddDate(0, 0, -1)) {
		year++
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

const cardMarker = `columns one-third`

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("auckland-museum: fetch: %w", err)
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	chunks := strings.Split(string(body), cardMarker)

	var lectures []model.Lecture
	seen := make(map[string]bool)

	for _, chunk := range chunks[1:] {
		// Only include "Event" category cards.
		catM := catRe.FindStringSubmatch(chunk)
		if catM == nil {
			continue
		}
		cat := strings.TrimSpace(catM[1])
		if !strings.EqualFold(cat, "event") {
			continue
		}

		titleM := titleRe.FindStringSubmatch(chunk)
		if titleM == nil {
			continue
		}
		href := titleM[1]
		title := strings.TrimSpace(titleM[2])
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

		link := baseURL + href
		seen[href] = true

		location := venueDefault
		if placeM := placeRe.FindStringSubmatch(chunk); placeM != nil {
			place := strings.TrimSpace(placeM[1])
			if place != "" {
				location = place + ", Auckland War Memorial Museum"
			}
		}

		summary := ""
		if dm := descRe.FindStringSubmatch(chunk); dm != nil {
			summary = innerText(dm[1])
		}

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(link),
			Title:     scraper.CleanTitle(title),
			Link:      link,
			TimeStart: t,
			Summary:   scraper.TruncateSummary(summary, 200),
			Location:  location,
			HostSlug:  "auckland-museum",
		})
	}

	return lectures, nil
}
