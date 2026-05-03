// Package objectspace scrapes public events from Objectspace gallery, Auckland.
//
// The events page at https://objectspace.org.nz/events is server-side rendered.
// Events appear as <article class="grid-col feed-item ... -event"> cards with:
//
//	<h2 class="title"><a href="/events/SLUG/"><span>TITLE</span></a></h2>
//	<h3 class="subtitle"><time>DATE</time></h3>  (e.g. "11 May", no year)
//
// Detail pages contain event metadata in <li class="listitem"> blocks:
//
//	Time:  "5.30pm doors / 6pm talk starts"
//	Venue: "Objectspace, 13 Rose Road, Ponsonby"
//	RSVP:  Eventbrite or other ticketing link
//
// Description comes from the <div class="contentcol content-styles"> section.
// Objectspace hosts the Ockham Lecture series, book launches, artist talks, and
// public workshops throughout the year.
package objectspace

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

const (
	baseURL   = "https://objectspace.org.nz"
	eventsURL = baseURL + "/events"
)

// Scraper implements scraper.Scraper for Objectspace gallery.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "objectspace",
		Name:        "Objectspace",
		Website:     eventsURL,
		Description: "Objectspace is an Auckland gallery dedicated to design and craft. It hosts the Ockham Lecture series, book launches, artist talks, and public workshops.",
	}
}

var (
	eventCardRe = regexp.MustCompile(`(?s)<article[^>]*class="[^"]*feed-item[^"]*-event[^"]*"[^>]*>(.*?)</article>`)
	titleLinkRe = regexp.MustCompile(`(?s)<a href="(/events/[^"]+)"><span>(.*?)</span>`)
	cardDateRe  = regexp.MustCompile(`<time>([^<]+)</time>`)

	listitemRe = regexp.MustCompile(`(?s)<li class="listitem">\s*<h3 class="title">([^<]+)</h3>\s*(.*?)\s*</li>`)
	timeRe     = regexp.MustCompile(`(?i)(\d{1,2})(?:\.(\d{2}))?([ap]m)\b`)
	rsvpLinkRe = regexp.MustCompile(`href="(https?://[^"]+)"`)
	pTagRe     = regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
)

// excludeKeywords identifies repetitive craft activities that are not talks/lectures.
var excludeKeywords = []string{
	"craft club",
}

func isExcluded(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range excludeKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// Scrape returns all Objectspace events excluding the Ockham Lecture series
// (handled by the ockham scraper).
func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	all, err := ScrapeAll(ctx)
	if err != nil {
		return nil, err
	}
	var lectures []model.Lecture
	for _, l := range all {
		if !strings.HasPrefix(strings.ToLower(l.Title), "ockham lecture") {
			lectures = append(lectures, l)
		}
	}
	return lectures, nil
}

// ScrapeAll fetches all upcoming Objectspace events. Callers (including the
// ockham scraper) can filter the result by title as needed.
func ScrapeAll(ctx context.Context) ([]model.Lecture, error) {
	nzLoc := scraper.NZLocation
	now := time.Now().In(nzLoc)

	body, err := scraper.Fetch(ctx, eventsURL)
	if err != nil {
		return nil, fmt.Errorf("objectspace: fetch listing: %w", err)
	}

	cards := eventCardRe.FindAllSubmatch(body, -1)
	var lectures []model.Lecture

	for _, card := range cards {
		block := string(card[1])

		tlM := titleLinkRe.FindStringSubmatch(block)
		if tlM == nil {
			continue
		}
		relURL := tlM[1]
		rawTitle, _ := scraper.SplitTitleSpeaker(scraper.InnerText(tlM[2]))
		title := scraper.CleanTitle(rawTitle)

		if isExcluded(title) {
			continue
		}

		eventURL := baseURL + relURL

		dateM := cardDateRe.FindStringSubmatch(block)
		if dateM == nil {
			continue
		}
		t, err := parseEventDate(strings.TrimSpace(dateM[1]), now)
		if err != nil {
			continue
		}
		if t.Before(now) {
			continue
		}

		detail := fetchDetail(ctx, eventURL)

		if detail.hour >= 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), detail.hour, detail.minute, 0, 0, nzLoc)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, nzLoc)
		}

		link := eventURL
		if detail.rsvpURL != "" {
			link = detail.rsvpURL
		}

		venue := detail.venue
		if venue == "" {
			venue = "Objectspace, 13 Rose Road, Ponsonby, Auckland"
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(eventURL),
			Title:       title,
			Link:        link,
			TimeStart:   t,
			Description: detail.description,
			Summary:     scraper.TruncateSummary(detail.description, 200),
			Location:    venue,
			Free:        false,
			HostSlug:    "objectspace",
		})
	}

	return lectures, nil
}

// parseEventDate parses a date string like "11 May" (no year), using the
// current year and advancing to next year if the date has already passed.
func parseEventDate(s string, now time.Time) (time.Time, error) {
	nzLoc := scraper.NZLocation
	for _, year := range []int{now.Year(), now.Year() + 1} {
		full := fmt.Sprintf("%s %d", s, year)
		if t, err := time.ParseInLocation("2 January 2006", full, nzLoc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("objectspace: cannot parse date %q", s)
}

type detailData struct {
	description string
	venue       string
	hour        int // -1 means not found
	minute      int
	rsvpURL     string
}

func fetchDetail(ctx context.Context, eventURL string) detailData {
	data := detailData{hour: -1}

	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return data
	}

	// Parse Time, Venue, RSVP from article-meta listitem blocks.
	for _, m := range listitemRe.FindAllStringSubmatch(string(body), -1) {
		key := strings.TrimSpace(m[1])
		rawVal := strings.TrimSpace(m[2])

		switch key {
		case "Time":
			// Extract the last time found: "5.30pm doors / 6pm talk starts" → 18:00
			times := timeRe.FindAllStringSubmatch(rawVal, -1)
			if len(times) > 0 {
				last := times[len(times)-1]
				data.hour, data.minute = parseTime12(last[1], last[2], last[3])
			}
		case "Venue":
			data.venue = scraper.InnerText(rawVal)
		case "RSVP":
			if lm := rsvpLinkRe.FindStringSubmatch(rawVal); lm != nil {
				data.rsvpURL = lm[1]
			}
		}
	}

	// Description: paragraphs in the content div (before the sidebar).
	contentStart := bytes.Index(body, []byte("contentcol content-styles"))
	asideStart := bytes.Index(body, []byte(`class="grid-col asidecol"`))
	if contentStart >= 0 && asideStart > contentStart {
		section := body[contentStart:asideStart]
		var parts []string
		for _, pm := range pTagRe.FindAllSubmatch(section, -1) {
			text := strings.TrimSpace(tagRe.ReplaceAllString(
				strings.NewReplacer("&ndash;", "–", "&ldquo;", "“", "&rdquo;", "”",
					"&lsquo;", "‘", "&rsquo;", "’", "&mdash;", "—", "&nbsp;", " ",
					"&amp;", "&").Replace(string(pm[1])),
				" "))
			text = strings.Join(strings.Fields(text), " ")
			if len(text) > 15 {
				parts = append(parts, text)
			}
		}
		data.description = strings.Join(parts, "\n\n")
	}

	// Fallback to meta description.
	if len(data.description) < 40 {
		data.description = scraper.ExtractDescription(body)
	}

	return data
}

// parseTime12 converts a 12-hour time match (hour, optMinute, ampm) to 24-hour.
func parseTime12(h, m, ampm string) (hour, minute int) {
	hr, _ := strconv.Atoi(h)
	min, _ := strconv.Atoi(m)
	ampm = strings.ToLower(ampm)
	if ampm == "pm" && hr != 12 {
		hr += 12
	} else if ampm == "am" && hr == 12 {
		hr = 0
	}
	return hr, min
}
