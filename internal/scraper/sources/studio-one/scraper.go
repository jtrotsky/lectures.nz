// Package studioone scrapes public events from Studio One Toi Tū, Grey Lynn, Auckland.
//
// The events page at https://www.studioone.org.nz/events/ is WordPress-rendered HTML.
// Each event is a <div class='event-item'> block containing:
//   - <h3 class='event-heading'> — title
//   - <ul class='event-meta'> — list items with "When" (date) and time
//   - <div class='event-description'> — summary text
//   - <li>Cost …</li> — pricing
//   - outer <a href='…'> — booking/info link
package studioone

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
	listingURL = "https://www.studioone.org.nz/events/"
	location   = "Studio One Toi Tū, 1 Summer Street, Grey Lynn, Auckland"
)

// Scraper implements scraper.Scraper for Studio One Toi Tū.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "studio-one",
		Name:        "Studio One Toi Tū",
		Website:     listingURL,
		Description: "Studio One Toi Tū is Auckland's community arts centre in Grey Lynn, hosting talks, workshops, exhibitions, and cultural events.",
	}
}

var (
	// hrefRe extracts the first href from an <a> tag.
	hrefRe = regexp.MustCompile(`(?i)<a\s[^>]*href=['"]([^'"]+)['"]`)
	// h3Re extracts text from the event-heading h3.
	h3Re = regexp.MustCompile(`(?i)<h3[^>]*class=['"]event-heading['"][^>]*>([\s\S]*?)</h3>`)
	// tagStripRe removes HTML tags.
	tagStripRe = regexp.MustCompile(`<[^>]+>`)
	// descRe extracts the first <p> inside event-description.
	descRe = regexp.MustCompile(`(?i)<div[^>]*class=['"]event-description['"][^>]*>([\s\S]*?)</div>`)
	// pRe extracts text from first <p>.
	pRe = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
	// whenLiRe finds the <li> that follows the "When" label.
	whenLiRe = regexp.MustCompile(`(?i)<span[^>]*class=['"]event-meta-label['"][^>]*>\s*When\s*</span>([\s\S]*?)</li>`)
	// nextLiRe extracts the text of the very next <li> (time field).
	nextLiRe = regexp.MustCompile(`(?i)</li>\s*<li[^>]*>([\s\S]*?)</li>`)
	// costLiRe checks for a "Free" cost entry.
	costFreeRe = regexp.MustCompile(`(?i)Cost\s*</span>\s*Free`)
	// dateLine extracts day-number and month from "FRI 27 FEB" style text.
	dateLineRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`)
	// timeLineRe extracts start time from "8 – 9.30AM" or "10AM" style text.
	timeLineRe = regexp.MustCompile(`(?i)(\d{1,2})(?:[\.\:](\d{2}))?\s*(am|pm)`)
	// brRe normalises <br> tags to newlines.
	brRe = regexp.MustCompile(`(?i)<br\s*/?>`)
)

var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

func innerText(html string) string {
	s := brRe.ReplaceAllString(html, "\n")
	s = tagStripRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// parseDateLine parses the first date line from "FRI 27 FEB" style text.
func parseDateLine(raw string, loc *time.Location) (time.Time, bool) {
	// May have multiple lines (recurring dates) — take first.
	line := strings.Split(innerText(raw), "\n")[0]
	dm := dateLineRe.FindStringSubmatch(line)
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
	return time.Date(year, month, day, 0, 0, 0, 0, loc), true
}

// parseTimeLine parses start hour/min from "8 – 9.30AM" style text.
func parseTimeLine(raw string) (hour, min int, ok bool) {
	text := innerText(raw)
	// Only look at text before the "–" or "-" to get start time.
	if idx := strings.IndexAny(text, "–-"); idx != -1 {
		text = text[:idx]
	}
	m := timeLineRe.FindStringSubmatch(text)
	if m == nil {
		return 0, 0, false
	}
	h, _ := strconv.Atoi(m[1])
	mn := 0
	if m[2] != "" {
		mn, _ = strconv.Atoi(m[2])
	}
	period := strings.ToLower(m[3])
	if period == "pm" && h != 12 {
		h += 12
	} else if period == "am" && h == 12 {
		h = 0
	}
	return h, mn, true
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("studio-one: fetch: %w", err)
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	// Split by event-item div.
	const itemMarker = `class='event-item'`
	chunks := strings.Split(string(body), itemMarker)

	var lectures []model.Lecture

	for _, chunk := range chunks[1:] {
		// Title.
		h3M := h3Re.FindStringSubmatch(chunk)
		if h3M == nil {
			continue
		}
		title := innerText(h3M[1])
		if title == "" {
			continue
		}

		// Date: find the "When" li.
		whenM := whenLiRe.FindStringSubmatch(chunk)
		if whenM == nil {
			continue
		}
		dateText := whenM[1]

		t, ok := parseDateLine(dateText, loc)
		if !ok {
			continue
		}

		// Time: the li immediately after the "When" li contains the time.
		// Find position of "When" in chunk, then look for next <li>.
		whenIdx := whenLiRe.FindStringIndex(chunk)
		if whenIdx != nil {
			after := chunk[whenIdx[1]:]
			nextM := nextLiRe.FindStringSubmatch(after)
			if nextM != nil {
				if h, mn, ok2 := parseTimeLine(nextM[1]); ok2 {
					t = time.Date(t.Year(), t.Month(), t.Day(), h, mn, 0, 0, loc)
				}
			}
		}

		// Link: first <a href> in the chunk.
		link := listingURL
		if lm := hrefRe.FindStringSubmatch(chunk); lm != nil {
			href := lm[1]
			if strings.HasPrefix(href, "http") {
				link = href
			} else if strings.HasPrefix(href, "/") {
				link = "https://www.studioone.org.nz" + href
			}
		}

		// Summary: first <p> inside event-description.
		summary := ""
		if dm := descRe.FindStringSubmatch(chunk); dm != nil {
			if pm := pRe.FindStringSubmatch(dm[1]); pm != nil {
				summary = innerText(pm[1])
			}
		}

		// Cost.
		free := costFreeRe.MatchString(chunk)

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(title + t.Format("2006-01-02")),
			Title:     title,
			Link:      link,
			TimeStart: t,
			Summary:   summary,
			Free:      free,
			Location:  location,
			HostSlug:  "studio-one",
		})
	}

	return lectures, nil
}
