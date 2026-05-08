// Package canterburymuseum scrapes public events from Canterbury Museum.
//
// The what's-on page at https://www.canterburymuseum.com/visit/whats-on/
// embeds all event data in a __NEXT_DATA__ JSON blob — no JS execution needed.
//
// Events are in data.events[]. Each has:
//   - title, uri, eventDate (ISO 8601), summary (HTML), eventInformation[]
//
// eventInformation is an array of icon+text pairs. The first text entry is either:
//   - a specific date/time like "9 May 11.00 am – 2.00 pm"     → discrete event
//   - a date range like "13 February to 14 June"                → exhibition (skipped)
//   - relative text like "Until 21 July" / "Every day…"        → exhibition (skipped)
//
// Only discrete events with a parseable specific time are included.
package canterburymuseum

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	listingURL  = "https://www.canterburymuseum.com/visit/whats-on/"
	baseURL     = "https://www.canterburymuseum.com"
	defaultCity = "Christchurch"
)

// Scraper implements scraper.Scraper for Canterbury Museum.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "canterbury-museum",
		Name:        "Canterbury Museum",
		City:        defaultCity,
		Website:     listingURL,
		Description: "Canterbury Museum (Te Papa o Ōtautahi) hosts public programmes, talks, and events celebrating the natural and cultural heritage of Canterbury and the Pacific.",
	}
}

// nextData mirrors the shape of the __NEXT_DATA__ JSON embedded in the page.
type nextData struct {
	Props struct {
		PageProps struct {
			Data struct {
				Events []eventEntry `json:"events"`
			} `json:"data"`
		} `json:"pageProps"`
	} `json:"props"`
}

type eventEntry struct {
	ID      string `json:"id"`
	URI     string `json:"uri"`
	Title   string `json:"title"`
	Summary string `json:"summary"` // may contain HTML tags
	EventInformation []struct {
		Text string `json:"text"`
	} `json:"eventInformation"`
}

var (
	// nextDataRe extracts the JSON blob from the <script id="__NEXT_DATA__"> tag.
	nextDataRe = regexp.MustCompile(`(?i)<script[^>]+id="__NEXT_DATA__"[^>]*>([\s\S]*?)</script>`)

	// specificDateRe matches "9 May 11.00 am" or "9 May 11.00 am – 2.00 pm"
	// Requires a time component (am/pm) to distinguish from date-range text.
	specificDateRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2})[\.\:](\d{2})\s*(am|pm)`)

	// dateRangeRe detects exhibition-style ranges ("to", "until", "every", "daily").
	dateRangeRe = regexp.MustCompile(`(?i)\bto\b|\buntil\b|\bevery\b|\bdaily\b|\bongoing\b`)
)

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("canterbury-museum: fetch: %w", err)
	}

	m := nextDataRe.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("canterbury-museum: __NEXT_DATA__ not found")
	}

	var nd nextData
	if err := json.Unmarshal(m[1], &nd); err != nil {
		return nil, fmt.Errorf("canterbury-museum: parse __NEXT_DATA__: %w", err)
	}

	loc := scraper.NZLocation
	now := time.Now().In(loc)
	var lectures []model.Lecture

	for _, e := range nd.Props.PageProps.Data.Events {
		if len(e.EventInformation) == 0 {
			continue
		}
		dateText := strings.TrimSpace(e.EventInformation[0].Text)

		// Skip exhibitions and open-ended events.
		if dateRangeRe.MatchString(dateText) {
			continue
		}

		t, ok := parseSpecificDate(dateText, loc, now)
		if !ok {
			continue
		}
		if t.Before(now) {
			continue
		}

		link := baseURL + "/" + e.URI

		// Collect location and cost from remaining eventInformation entries.
		location := defaultCity
		free := false
		cost := ""
		for _, info := range e.EventInformation[1:] {
			txt := strings.TrimSpace(info.Text)
			lower := strings.ToLower(txt)
			if lower == "free" || strings.HasPrefix(lower, "free") {
				free = true
			} else if strings.Contains(lower, "$") || lower == "ticketed" || strings.HasPrefix(lower, "tickets") {
				cost = txt
			} else if txt != "" && location == defaultCity {
				location = txt + ", Christchurch"
			}
		}

		summary := scraper.InnerText(e.Summary)

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(link),
			Title:       scraper.CleanTitle(e.Title),
			Link:        link,
			TimeStart:   t,
			Description: summary,
			Summary:     scraper.TruncateSummary(summary, 200),
			Location:    location,
			Free:        free,
			Cost:        cost,
			HostSlug:    "canterbury-museum",
		})
	}

	return lectures, nil
}

// parseSpecificDate parses strings like "9 May 11.00 am" or "9 May 11.00 am – 2.00 pm".
// Returns the parsed time and true on success.
func parseSpecificDate(s string, loc *time.Location, now time.Time) (time.Time, bool) {
	m := specificDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}

	day, _ := strconv.Atoi(m[1])
	monthName := strings.ToLower(m[2][:3])
	month, ok := scraper.AbbrevMonthMap[monthName]
	if !ok {
		return time.Time{}, false
	}
	hour, _ := strconv.Atoi(m[3])
	min, _ := strconv.Atoi(m[4])
	period := strings.ToLower(m[5])

	if period == "pm" && hour != 12 {
		hour += 12
	} else if period == "am" && hour == 12 {
		hour = 0
	}

	year := now.Year()
	t := time.Date(year, month, day, hour, min, 0, 0, loc)
	if t.Before(now) {
		t = time.Date(year+1, month, day, hour, min, 0, 0, loc)
	}
	return t, true
}
