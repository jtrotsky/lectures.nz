// Package artspace scrapes public events from Artspace Aotearoa, Auckland.
//
// The events page at https://artspace-aotearoa.nz/events/ is server-side rendered.
// Events appear as:
//
//	<a class="tile is-event" href="https://artspace-aotearoa.nz/events/{slug}">
//	  <header class="tile-header text-meta">2 July 2026</header>
//	  ...
//	  <h3 class="tile-title">Event title</h3>
//	</a>
//
// Detail pages contain a fuller description in the first <p> after the <h1>.
// Events include artist talks, lectures, curator tours, panel discussions, and workshops.
// All Artspace events are free or koha.
package artspace

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const listingURL = "https://artspace-aotearoa.nz/events/"

// Scraper implements scraper.Scraper for Artspace Aotearoa.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "artspace",
		Name:        "Artspace Aotearoa",
		Website:     listingURL,
		Description: "Artspace Aotearoa in Tāmaki Makaurau hosts artist talks, lectures, curator tours, and public programmes alongside its contemporary art exhibitions.",
	}
}

var (
	// Each event: href comes before class in the tag, attributes on separate lines.
	// <a\n\thref="URL"\n\tclass="tile is-event">...</a>
	eventBlockRe = regexp.MustCompile(`(?s)<a\s[^>]*href="(https://artspace-aotearoa\.nz/events/[^"]+)"[^>]*class="tile is-event"[^>]*>(.*?)</a>`)
	// Date in <header class="tile-header text-meta">2 July 2026</header>
	dateRe = regexp.MustCompile(`(?s)class="tile-header text-meta"[^>]*>\s*([^<]+?)\s*<`)
	// Title in <h3 class="tile-title"...>Title</h3>
	titleRe = regexp.MustCompile(`(?s)class="tile-title"[^>]*>\s*([^<]+?)\s*<`)
)

// workshopKeywords identifies hands-on workshops that aren't talks/lectures.
var workshopKeywords = []string{"workshop", "craft club", "pinhole camera"}

func isWorkshop(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range workshopKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	nzLoc := scraper.NZLocation
	now := time.Now().In(nzLoc)

	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("artspace: fetch listing: %w", err)
	}

	matches := eventBlockRe.FindAllSubmatch(body, -1)

	var lectures []model.Lecture
	for _, m := range matches {
		eventURL := string(m[1])
		block := string(m[2])

		dateM := dateRe.FindStringSubmatch(block)
		if dateM == nil {
			continue
		}
		dateStr := strings.TrimSpace(dateM[1])
		// Handle date ranges like "14 April – 16 April" — use first date.
		if idx := strings.IndexAny(dateStr, "–-"); idx > 0 {
			dateStr = strings.TrimSpace(dateStr[:idx])
		}

		// Parse "2 July 2026" or "6 June 2026".
		t, err := time.ParseInLocation("2 January 2006", dateStr, nzLoc)
		if err != nil {
			// Try without year (shouldn't happen but guard anyway).
			continue
		}
		titleM := titleRe.FindStringSubmatch(block)
		if titleM == nil {
			continue
		}
		title := scraper.InnerText(titleM[1])

		if isWorkshop(title) {
			continue
		}

		detail := fetchDetail(ctx, eventURL)

		hour, minute := 18, 0
		if detail.hour >= 0 {
			hour, minute = detail.hour, detail.minute
		}
		t = time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, nzLoc)

		if t.Before(now) {
			continue
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(eventURL),
			Title:       scraper.CleanTitle(title),
			Link:        eventURL,
			TimeStart:   t,
			Description: detail.description,
			Summary:     scraper.TruncateSummary(detail.description, 200),
			Location:    "Artspace Aotearoa, 292 Karangahape Road, Auckland",
			Free:        true,
			HostSlug:    "artspace",
		})
	}

	return lectures, nil
}

var (
	summaryRe = regexp.MustCompile(`(?s)<h1[^>]*>.*?</h1>\s*(?:<[^/][^>]*>)*\s*<p[^>]*>([\s\S]*?)</p>`)
	// Matches "2:00pm – 3:00pm", "6:00pm – 7:00pm", or bare "6:00pm" in a <span class="value"> element.
	timeValueRe = regexp.MustCompile(`<span class="value">(\d{1,2}:\d{2}(?:am|pm))(?:\s*[–-]|</span>)`)
)

type detailInfo struct {
	description string
	// hour and minute from the detail page; -1 if not found.
	hour, minute int
}

func fetchDetail(ctx context.Context, eventURL string) detailInfo {
	info := detailInfo{hour: -1, minute: -1}
	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return info
	}

	if m := summaryRe.FindSubmatch(body); m != nil {
		info.description = scraper.InnerText(string(m[1]))
	}

	if m := timeValueRe.FindSubmatch(body); m != nil {
		t, err := time.Parse("3:04pm", string(m[1]))
		if err == nil {
			info.hour = t.Hour()
			info.minute = t.Minute()
		}
	}

	return info
}
