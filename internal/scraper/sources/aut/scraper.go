// Package aut scrapes public events from Auckland University of Technology (AUT).
//
// AUT's events page at https://www.aut.ac.nz/events uses Squiz Matrix CMS
// and renders event listings server-side as HTML — no JavaScript execution needed.
//
// Event structure in HTML:
//
//	<div class="eventList">
//	  <span class="monText">Apr</span>
//	  <span class="dateText">22</span>
//	  dateSummary: "April 2026"
//	  Date/Time: "Wednesday 22 Apr, 5:30pm - 7:30pm"
//	  Location: "AUT City Campus<br>WG Building..."
//	  <a class="eventTitle">Title</a>
//	</div>
package aut

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const listingURL = "https://www.aut.ac.nz/events"

// Scraper implements scraper.Scraper for AUT.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "aut",
		Name:        "Auckland University of Technology",
		Website:     "https://www.aut.ac.nz/events",
		Description: "AUT hosts public lectures, symposia, and open events across its Auckland campuses.",
	}
}

var (
	eventListRe   = regexp.MustCompile(`(?s)<div class="eventList[^"]*">(.*?)(?:<div class="eventList|$)`)
	monTextRe     = regexp.MustCompile(`class="monText">([^<]+)<`)
	dateTextRe    = regexp.MustCompile(`class="dateText">([^<]+)<`)
	dateSummaryRe = regexp.MustCompile(`class="dateSummary"[^>]*>\s*([^<]+)\s*<`)
	titleLinkRe   = regexp.MustCompile(`href="([^"]+)"[^>]*class="eventTitle[^"]*"[^>]*>(.*?)</a>`)
	dateTimeRe    = regexp.MustCompile(`(?s)Date/Time:.*?col-sm-9">(.*?)</div>`)
	locationRe    = regexp.MustCompile(`(?s)Location:.*?col-sm-9">(.*?)</div>`)
	timeRe        = regexp.MustCompile(`(?i),\s*(\d{1,2}(?::\d{2})?(?:am|pm))`)
	jsonLdDescRe  = regexp.MustCompile(`"description"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

// fetchSummary fetches an event detail page and extracts the JSON-LD description.
func fetchSummary(ctx context.Context, url string) string {
	body, err := scraper.Fetch(ctx, url)
	if err != nil {
		return ""
	}
	m := jsonLdDescRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	// JSON-LD string values use JSON escaping — unquote them.
	unquoted, err := strconv.Unquote(`"` + string(m[1]) + `"`)
	if err != nil {
		return string(m[1])
	}
	return unquoted
}

// stripTags removes HTML tags and normalises whitespace.
func stripTags(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// parseAUTTime parses "Wednesday 22 Apr, 5:30pm" style strings.
// Returns hour, minute in 24h and whether parsing succeeded.
func parseAUTTime(s string) (int, int, bool) {
	m := timeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	t := strings.ToLower(strings.TrimSpace(m[1]))
	isPM := strings.HasSuffix(t, "pm")
	t = strings.TrimSuffix(strings.TrimSuffix(t, "pm"), "am")
	parts := strings.SplitN(t, ":", 2)
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

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("aut: fetch: %w", err)
	}

	// Find the eventList section.
	const marker = `id="eventList"`
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		return nil, fmt.Errorf("aut: eventList marker not found")
	}
	// Take a generous chunk after the marker to find all events.
	section := string(body[idx:])
	if end := strings.Index(section, `id="eventFilter"`); end > 0 {
		section = section[:end]
	}

	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}
	now := time.Now().In(nzLoc)

	matches := eventListRe.FindAllStringSubmatch(section, -1)
	var lectures []model.Lecture

	for _, m := range matches {
		block := m[1]

		// Extract month (e.g. "Apr"), day (e.g. "22"), year from dateSummary (e.g. "April 2026").
		monM := monTextRe.FindStringSubmatch(block)
		dayM := dateTextRe.FindStringSubmatch(block)
		summaryM := dateSummaryRe.FindStringSubmatch(block)
		if monM == nil || dayM == nil || summaryM == nil {
			continue
		}

		// Parse year from "April 2026".
		yearStr := regexp.MustCompile(`\d{4}`).FindString(summaryM[1])
		if yearStr == "" {
			yearStr = strconv.Itoa(now.Year())
		}
		year, _ := strconv.Atoi(yearStr)

		dayNum, _ := strconv.Atoi(strings.TrimSpace(dayM[1]))
		monthStr := strings.TrimSpace(monM[1])
		monthTime, err := time.Parse("Jan", monthStr)
		if err != nil {
			// Try full month name
			monthTime, err = time.Parse("January", monthStr)
			if err != nil {
				continue
			}
		}

		// Extract title and URL.
		titleM := titleLinkRe.FindStringSubmatch(block)
		if titleM == nil {
			continue
		}
		eventURL := titleM[1]
		title := stripTags(titleM[2])

		// Extract start time.
		dtM := dateTimeRe.FindStringSubmatch(block)
		h, mn, ok := 0, 0, false
		if dtM != nil {
			h, mn, ok = parseAUTTime(stripTags(dtM[1]))
		}
		if !ok {
			h, mn = 12, 0
		}

		t := time.Date(year, monthTime.Month(), dayNum, h, mn, 0, 0, nzLoc)

		// Extract location.
		locM := locationRe.FindStringSubmatch(block)
		location := "Auckland"
		if locM != nil {
			raw := locM[1]
			// Replace <br> with ", " then strip remaining tags.
			raw = regexp.MustCompile(`(?i)<br\s*/?>(\s*<br\s*/?>)*`).ReplaceAllString(raw, ", ")
			location = stripTags(raw)
			// Remove trailing NZ boilerplate
			location = strings.TrimSuffix(location, ", Auckland , New Zealand")
			location = strings.TrimSuffix(location, ", New Zealand")
			location = strings.TrimSpace(location)
			if location == "" {
				location = "Auckland"
			}
			location += ", Auckland"
		}

		rawDesc := fetchSummary(ctx, eventURL)
		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(eventURL),
			Title:       scraper.CleanTitle(title),
			Link:        eventURL,
			TimeStart:   t,
			Description: rawDesc,
			Summary:     scraper.TruncateSummary(rawDesc, 200),
			Location:    location,
			HostSlug:    "aut",
		})
	}

	return lectures, nil
}
