// Package motu scrapes public policy seminars from Motu Economic and Public Policy Research.
//
// The events listing page paginates via ?start=10 increments and renders items as:
//
//	<h3><a href="/about-us/public-policy-seminars/events/{slug}">Title</a></h3>
//	<p>Description snippet…</p>
//	<strong>Dec 1, 2025</strong>
//
// Detail pages provide a fuller description and speaker names in <strong> tags.
// Events with "Webinar" in the title are online-only and are skipped.
// All Motu Public Policy Seminars are free to the public.
package motu

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
	baseURL    = "https://motu.nz"
	listingURL = "https://motu.nz/about-us/public-policy-seminars/events"
)

// Scraper implements scraper.Scraper for Motu Economic and Public Policy Research.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "motu",
		Name:        "Motu Economic and Public Policy Research",
		Website:     listingURL,
		Description: "Motu hosts free public policy seminars on economics, climate, housing, and wellbeing, typically held in Wellington.",
	}
}

var (
	// Listing page: links and dates.
	listHrefRe  = regexp.MustCompile(`<h3[^>]*>\s*<a\s+href="(/about-us/public-policy-seminars/events/[^"]+)"`)
	listTitleRe = regexp.MustCompile(`<h3[^>]*>\s*<a[^>]*>([^<]+)</a>`)
	listDateRe  = regexp.MustCompile(`<b>([A-Za-z]+ \d{1,2}, \d{4})</b>`)

	// Detail page.
	detailTitleRe = regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`)
	// Speaker names appear as the first <strong> in <p> tags.
	speakerRe = regexp.MustCompile(`<p[^>]*><strong>([^<]+)</strong>`)
	// Summary: first <p> after </h1>.
	firstParaRe = regexp.MustCompile(`</h1>\s*(?:<[^/][^>]*>)*\s*<p[^>]*>([\s\S]*?)</p>`)

)

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	loc := scraper.NZLocation
	now := time.Now().In(loc)

	seen := make(map[string]bool)
	type item struct {
		href  string
		title string
		date  time.Time
	}
	var items []item

	for start := 0; ; start += 10 {
		pageURL := fmt.Sprintf("%s?start=%d", listingURL, start)
		body, err := scraper.Fetch(ctx, pageURL)
		if err != nil {
			return nil, fmt.Errorf("motu: fetch listing page start=%d: %w", start, err)
		}
		html := string(body)

		hrefs := listHrefRe.FindAllStringSubmatch(html, -1)
		titles := listTitleRe.FindAllStringSubmatch(html, -1)
		dates := listDateRe.FindAllStringSubmatch(html, -1)

		if len(hrefs) == 0 {
			break // no more pages
		}

		anyFuture := false
		for i, hm := range hrefs {
			href := hm[1]
			if seen[href] {
				continue
			}
			seen[href] = true

			title := ""
			if i < len(titles) {
				title = strings.TrimSpace(titles[i][1])
			}

			// Skip webinars / online-only by title.
			if strings.Contains(strings.ToLower(title), "webinar") {
				continue
			}

			var t time.Time
			if i < len(dates) {
				t, _ = time.ParseInLocation("Jan 2, 2006", strings.TrimSpace(dates[i][1]), loc)
			}
			if t.IsZero() || t.Before(now.Add(-24*time.Hour)) {
				continue
			}
			anyFuture = true
			items = append(items, item{href: href, title: title, date: t})
		}

		// Listing is newest-first; once a full page has no future events, stop.
		if !anyFuture {
			break
		}
	}

	var lectures []model.Lecture
	for _, it := range items {
		link := baseURL + it.href
		lec, ok := fetchDetail(ctx, link, it.date, loc)
		if !ok {
			continue
		}
		lectures = append(lectures, lec)
	}
	return lectures, nil
}

func fetchDetail(ctx context.Context, link string, listingDate time.Time, loc *time.Location) (model.Lecture, bool) {
	body, err := scraper.Fetch(ctx, link)
	if err != nil {
		return model.Lecture{}, false
	}
	html := string(body)

	tm := detailTitleRe.FindStringSubmatch(html)
	if tm == nil {
		return model.Lecture{}, false
	}
	title := scraper.InnerText(tm[1])

	// Use noon on the listing date as the start time (Motu rarely publishes a time).
	t := time.Date(listingDate.Year(), listingDate.Month(), listingDate.Day(), 12, 0, 0, 0, loc)

	// Summary: first paragraph after the h1.
	var summary string
	if pm := firstParaRe.FindStringSubmatch(html); pm != nil {
		summary = scraper.InnerText(pm[1])
	}

	// Speakers: first <strong> in each <p> block, limited to a few.
	var speakers []model.Speaker
	for _, sm := range speakerRe.FindAllStringSubmatch(html, 4) {
		name := strings.TrimSpace(sm[1])
		if name == "" || len(name) > 80 {
			continue
		}
		speakers = append(speakers, model.Speaker{Name: name})
		if len(speakers) >= 2 {
			break
		}
	}

	return model.Lecture{
		ID:          scraper.MakeID(link),
		Title:       scraper.CleanTitle(title),
		Link:        link,
		TimeStart:   t,
		Description: summary,
		Summary:     scraper.TruncateSummary(summary, 200),
		Location:    "Wellington",
		Free:        true,
		Speakers:    speakers,
		HostSlug:    "motu",
	}, true
}
