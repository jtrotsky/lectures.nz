// Package nziia scrapes public events from the NZ Institute of International Affairs.
//
// The events listing page at /events renders cards as:
//
//	<a class="events__card flex fd-column [events__card--past-event]" href="/events/{slug}">
//	  <time class="h5 ...">DD.MM.YYYY</time>
//	  <span class="... events__card__details-block__text--branch">City</span>
//	  <div class="events__card__details-block__icon events__card__details-block__icon--in-person"></div>
//	  <h3 ...>Title</h3>
//	</a>
//
// Cards with class "events__card--past-event" are skipped. The featured event
// uses a "featured-event--desktop" anchor (the mobile duplicate is ignored).
//
// Each event detail page provides:
//
//	<h1>Title</h1>
//	<h2>Speaker Name, Affiliation</h2>
//	<h3>Tuesday, 14 April 2026 5:30pm</h3>
//	<h3 class="color--dark-grey">City</h3>
//	<h4 class="... sub-page-details-section--event__location-text">Full venue</h4>
//	<div class="dn-s"><p>Summary…</p></div>
//
// Events without the in-person icon are skipped (webinars/online-only).
package nziia

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
	listingURL = "https://www.nziia.org.nz/events"
	baseURL    = "https://www.nziia.org.nz"
)

// Scraper implements scraper.Scraper for the NZ Institute of International Affairs.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "nziia",
		Name:        "NZ Institute of International Affairs",
		Website:     listingURL,
		Description: "The New Zealand Institute of International Affairs hosts public lectures and forums on foreign policy, geopolitics, and international relations across New Zealand.",
	}
}

var (
	// featuredHrefRe extracts the href from the featured desktop card anchor.
	featuredHrefRe = regexp.MustCompile(`(?i)class="featured-event--desktop[^"]*"[^>]+href="(/events/[^"]+)"`)
	// cardHrefRe extracts the href from within a regular card chunk.
	cardHrefRe = regexp.MustCompile(`href="(/events/[^"]+)"`)
	// cardDateRe extracts the DD.MM.YYYY date from a card's time element.
	cardDateRe = regexp.MustCompile(`<time[^>]*>(\d{2}\.\d{2}\.\d{4})</time>`)

	// Detail page regexes — applied to content from the event h1 onwards.
	titleRe        = regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`)
	speakerRe      = regexp.MustCompile(`<h2[^>]*>([^<]+)</h2>`)
	dateTimeRe     = regexp.MustCompile(`<h3[^>]*>([^<]+)</h3>`)
	venueRe        = regexp.MustCompile(`sub-page-details-section--event__location-text[^>]*>([^<]+)</h4>`)
	summaryBlockRe = regexp.MustCompile(`(?i)class="dn-s"[^>]*>([\s\S]*?)</div>`)
	tagRe          = regexp.MustCompile(`<[^>]+>`)
	inPersonRe     = regexp.MustCompile(`event__icon--in-person`)

	// detailDateRe parses "Tuesday, 14 April 2026 5:30pm".
	detailDateRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{4})`)
	timeParsRe   = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(am|pm)`)
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
	s = strings.ReplaceAll(s, "&#039;", "'")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// parseDetailDate parses "Tuesday, 14 April 2026 5:30pm".
func parseDetailDate(s string, loc *time.Location) (time.Time, bool) {
	dm := detailDateRe.FindStringSubmatch(s)
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

// parseCardDate parses "DD.MM.YYYY" from listing cards.
func parseCardDate(s string) (time.Time, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	day, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	year, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), true
}

const cardMarker = `class="events__card flex fd-column`

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("nziia: fetch listing: %w", err)
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now()

	html := string(body)
	seen := make(map[string]bool)
	var hrefs []string

	// Featured desktop card (skip the mobile duplicate).
	if m := featuredHrefRe.FindStringSubmatch(html); m != nil {
		if !seen[m[1]] {
			seen[m[1]] = true
			hrefs = append(hrefs, m[1])
		}
	}

	// Regular event cards.
	for _, chunk := range strings.Split(html, cardMarker) {
		// The chunk starts right after "class="events__card flex fd-column".
		// Past events have "events__card--past-event" next; upcoming have a space then `"`.
		peek := chunk
		if len(peek) > 50 {
			peek = peek[:50]
		}
		if strings.Contains(peek, "past-event") {
			continue
		}

		// Also filter by card date to avoid fetching borderline past events.
		if dm := cardDateRe.FindStringSubmatch(chunk); dm != nil {
			if t, ok := parseCardDate(dm[1]); ok && t.Before(now.Add(-24*time.Hour)) {
				continue
			}
		}

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

	// Must be in-person.
	if !inPersonRe.MatchString(html) {
		return model.Lecture{}, false
	}

	titleM := titleRe.FindStringSubmatch(html)
	if titleM == nil {
		return model.Lecture{}, false
	}
	title := strings.TrimSpace(titleM[1])

	// Slice content from the h1 onwards to avoid nav false-matches.
	idx := strings.Index(html, titleM[0])
	content := html[idx:]

	// Date/time from first h3 in content.
	dtM := dateTimeRe.FindStringSubmatch(content)
	if dtM == nil {
		return model.Lecture{}, false
	}
	t, ok := parseDetailDate(strings.TrimSpace(dtM[1]), loc)
	if !ok {
		return model.Lecture{}, false
	}
	if t.Before(time.Now().In(loc)) {
		return model.Lecture{}, false
	}

	// Speaker from first h2 in content.
	var speaker string
	if sm := speakerRe.FindStringSubmatch(content); sm != nil {
		speaker = strings.TrimSpace(sm[1])
	}

	// Full venue.
	var location string
	if vm := venueRe.FindStringSubmatch(content); vm != nil {
		location = strings.TrimSpace(vm[1])
	}

	// Summary text.
	var summary string
	if sum := summaryBlockRe.FindStringSubmatch(content); sum != nil {
		summary = innerText(sum[1])
	}

	lec := model.Lecture{
		ID:          scraper.MakeID(link),
		Title:       scraper.CleanTitle(title),
		Link:        link,
		TimeStart:   t,
		Description: summary,
		Summary:     scraper.TruncateSummary(summary, 200),
		Location:    location,
		HostSlug:    "nziia",
	}
	if speaker != "" {
		lec.Speakers = []model.Speaker{{Name: speaker}}
	}
	return lec, true
}
