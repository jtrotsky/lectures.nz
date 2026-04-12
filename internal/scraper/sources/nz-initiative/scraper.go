// Package nzinitiative scrapes public events from The New Zealand Initiative.
//
// The events page at https://www.nzinitiative.org.nz/events/ is server-side rendered.
// Events appear as:
//
//	<article class="article summary typography Event">
//	  <h2><a href="/events/{slug}/">Event Title</a></h2>
//	  <div class="publish-date">7 May, 2026</div>
//	  <div class="location">Auckland</div>
//	  <p>Description...</p>
//	</article>
//
// Members-only events (lunches, retreats) are excluded — only public talks,
// seminars, and webinars are included.
package nzinitiative

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	listingURL = "https://www.nzinitiative.org.nz/events/"
	baseURL    = "https://www.nzinitiative.org.nz"
)

// Scraper implements scraper.Scraper for The New Zealand Initiative.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "nz-initiative",
		Name:        "The New Zealand Initiative",
		Website:     listingURL,
		Description: "The New Zealand Initiative is a Wellington-based think tank hosting public lectures, seminars, and forums on economic policy, education, and governance.",
	}
}

var (
	// Each event is an <article class="article summary typography Event"> block.
	// The tag may have trailing whitespace/tabs after the class value.
	articleRe = regexp.MustCompile(`(?s)<article class="article summary typography Event"[^>]*>(.*?)</article>`)
	// Title and link from <h2><a href="/events/...">Title</a></h2>
	titleLinkRe = regexp.MustCompile(`<h2><a href="(/events/[^"]+)">([^<]+)</a></h2>`)
	// Date from <div class="publish-date">\n  7 May, 2026\n</div>
	dateRe = regexp.MustCompile(`class="publish-date"[^>]*>\s*([^<\n]+?)\s*<`)
	// Location from <div class="location">Auckland</div>
	locationRe = regexp.MustCompile(`class="location"[^>]*>\s*([^<\n]+?)\s*<`)
	// Members-only marker
	membersRe = regexp.MustCompile(`members-only|Members.*Lunch|Members.*Retreat|Members.*Dinner`)
)

func isMembers(title, block string) bool {
	t := strings.ToLower(title)
	if strings.Contains(t, "members'") || strings.Contains(t, "members'") {
		return true
	}
	return membersRe.MatchString(block)
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}
	now := time.Now().In(nzLoc)

	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("nz-initiative: fetch listing: %w", err)
	}

	articles := articleRe.FindAllSubmatch(body, -1)

	var lectures []model.Lecture
	for _, a := range articles {
		block := string(a[1])

		tlM := titleLinkRe.FindStringSubmatch(block)
		if tlM == nil {
			continue
		}
		eventPath := tlM[1]
		title := strings.TrimSpace(tlM[2])

		if isMembers(title, block) {
			continue
		}

		dateM := dateRe.FindStringSubmatch(block)
		if dateM == nil {
			continue
		}
		dateStr := strings.TrimSpace(dateM[1])
		// Format: "7 May, 2026"
		t, err := time.ParseInLocation("2 January, 2006", dateStr, nzLoc)
		if err != nil {
			continue
		}
		// Default to 18:00 if no time is available.
		t = time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, nzLoc)

		if t.Before(now) {
			continue
		}

		eventURL := baseURL + eventPath

		locM := locationRe.FindStringSubmatch(block)
		location := "Wellington"
		if locM != nil && strings.TrimSpace(locM[1]) != "" {
			location = strings.TrimSpace(locM[1])
		}

		summary := scraper.TruncateSummary(fetchSummary(ctx, eventURL), 200)

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(eventURL),
			Title:     scraper.CleanTitle(title),
			Link:      eventURL,
			TimeStart: t,
			Summary:   summary,
			Location:  location,
			HostSlug:  "nz-initiative",
		})
	}

	return lectures, nil
}

var summaryParaRe = regexp.MustCompile(`(?s)<p[^>]*>([\s\S]*?)</p>`)
var tagStripRe = regexp.MustCompile(`<[^>]+>`)

func fetchSummary(ctx context.Context, eventURL string) string {
	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return ""
	}
	// Find the main content area — first substantial <p> after the h1.
	idx := strings.Index(string(body), "</h1>")
	if idx < 0 {
		return ""
	}
	rest := string(body)[idx:]
	m := summaryParaRe.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	text := tagStripRe.ReplaceAllString(m[1], " ")
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}
