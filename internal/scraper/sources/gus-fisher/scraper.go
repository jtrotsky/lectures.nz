// Package gusfisher scrapes public events from Gus Fisher Gallery, University of Auckland.
//
// The site is WordPress with the Divi page builder. Each event occupies one
// et_pb_row containing two columns — one is a spacer div, the other has:
//
//	.et_pb_text_inner  →  <h2>category</h2> <h1>title</h1> <h3>subtitle</h3>
//	.et_pb_text_inner  →  <p>Date time</p> <p>Description…</p>
//	.et_pb_button      →  registration link
//
// We split by et_pb_row, find rows containing <h1> tags, and extract each event.
package gusfisher

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
	listingURL = "https://gusfishergallery.auckland.ac.nz/upcoming-events/"
	location   = "Gus Fisher Gallery, 74 Shortland Street, Auckland CBD"
)

// Scraper implements scraper.Scraper for Gus Fisher Gallery.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "gus-fisher",
		Name:        "Gus Fisher Gallery",
		Website:     listingURL,
		Description: "Gus Fisher Gallery at the University of Auckland hosts artist talks, film screenings, panel discussions, and public programmes in contemporary art.",
	}
}

var (
	// innerRe splits out content between et_pb_text_inner divs.
	innerRe = regexp.MustCompile(`(?i)class="et_pb_text_inner"[^>]*>([\s\S]*?)</div>`)
	// h1Re extracts h1 text.
	h1Re = regexp.MustCompile(`(?i)<h1[^>]*>([\s\S]*?)</h1>`)
	// h2Re extracts h2 text.
	h2Re = regexp.MustCompile(`(?i)<h2[^>]*>([\s\S]*?)</h2>`)
	// h3Re extracts h3 text.
	h3Re = regexp.MustCompile(`(?i)<h3[^>]*>([\s\S]*?)</h3>`)
	// pAllRe extracts all <p> tags content.
	pAllRe = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
	// btnRe extracts the href from et_pb_button anchor.
	btnRe = regexp.MustCompile(`(?i)class="et_pb_button[^"]*"[^>]*href="([^"]+)"`)
	// tagRe strips HTML tags.
	tagRe = regexp.MustCompile(`<[^>]+>`)
	// dateParsRe matches "11 April" or "18 April" in date strings.
	dateParsRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(January|February|March|April|May|June|July|August|September|October|November|December)`)
	// timeParsRe matches start time like "10.30am", "2pm".
	timeParsRe = regexp.MustCompile(`(?i)(\d{1,2})(?:[\.\:](\d{2}))?\s*(am|pm)`)
)

var monthMap = map[string]time.Month{
	"january": time.January, "february": time.February, "march": time.March,
	"april": time.April, "may": time.May, "june": time.June,
	"july": time.July, "august": time.August, "september": time.September,
	"october": time.October, "november": time.November, "december": time.December,
}

func innerText(html string) string {
	s := tagRe.ReplaceAllString(html, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#8217;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// parseDateStr parses "Saturday 11 April, 10.30am-11.30am" → time.Time.
func parseDateStr(s string, loc *time.Location) (time.Time, bool) {
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

	// Start time — text before "-" or "–".
	timeStr := s
	for _, sep := range []string{"-", "–"} {
		if idx := strings.Index(s, sep); idx != -1 {
			timeStr = s[:idx]
			break
		}
	}
	hour, min := 18, 0
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

const rowMarker = `et_pb_row et_pb_row_`

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("gus-fisher: fetch: %w", err)
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	html := string(body)

	// Find all et_pb_text_inner blocks across the page.
	innerBlocks := innerRe.FindAllStringSubmatch(html, -1)

	// Walk pairs of blocks: first has h1/h2/h3 metadata, second has date+description.
	var lectures []model.Lecture

	for i := 0; i < len(innerBlocks)-1; i++ {
		block := innerBlocks[i][1]

		h1M := h1Re.FindStringSubmatch(block)
		if h1M == nil {
			continue
		}
		title := innerText(h1M[1])
		if title == "" {
			continue
		}

		// The next text_inner block should have the date in its first <p>.
		nextBlock := innerBlocks[i+1][1]
		pMatches := pAllRe.FindAllStringSubmatch(nextBlock, -1)
		if len(pMatches) == 0 {
			continue
		}
		dateStr := innerText(pMatches[0][1])

		t, ok := parseDateStr(dateStr, loc)
		if !ok {
			continue
		}

		// Description: all text in the next block after the date line.
		// Content may use <div> tags instead of <p>, so strip the date line
		// from the raw block text and take whatever remains.
		allText := innerText(nextBlock)
		summary := strings.TrimSpace(strings.TrimPrefix(allText, dateStr))

		// Registration link: search forward from current position in raw HTML.
		// Find the button link within a reasonable window.
		startIdx := strings.Index(html, innerBlocks[i][0])
		link := listingURL
		if startIdx >= 0 {
			window := html[startIdx:]
			if end := strings.Index(window[100:], rowMarker); end > 0 {
				window = window[:100+end]
			} else if len(window) > 3000 {
				window = window[:3000]
			}
			if bm := btnRe.FindStringSubmatch(window); bm != nil {
				link = bm[1]
			}
		}

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(title + t.Format("2006-01-02")),
			Title:     scraper.CleanTitle(title),
			Link:      link,
			TimeStart: t,
			Description: summary,
			Summary:     scraper.TruncateSummary(summary, 200),
			Location:    location,
			Free:        true,
			HostSlug:    "gus-fisher",
		})

		i++ // skip next block since we consumed it as date/desc
	}

	return lectures, nil
}
