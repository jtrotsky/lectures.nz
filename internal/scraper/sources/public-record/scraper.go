// Package publicrecord scrapes public events from Public Record, Auckland.
//
// The site is a Shopify blog at https://publicrecord.nz/blogs/events-workshops.
// Each event is a blog article card with:
//   - <h3 class="card__heading h2"><a href="/blogs/events-workshops/{slug}">Title</a></h3>
//   - <div class="article-card__info"> containing the date text (e.g. "1st May 2026")
//
// Individual article pages are fetched for the full description.
// The listing supports pagination via ?page=N.
package publicrecord

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
	listingURL = "https://publicrecord.nz/blogs/events-workshops"
	baseURL    = "https://publicrecord.nz"
	evtLocation = "Public Record, 17 Pitt Street, Auckland"
	maxPages   = 5
)

// Scraper implements scraper.Scraper for Public Record.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "public-record",
		Name:        "Public Record",
		Website:     listingURL,
		Description: "Public Record is an Auckland venue hosting live events, workshops, talks, and cultural programmes in the city centre.",
	}
}

var (
	// articleRe splits on each blog article card.
	articleRe = regexp.MustCompile(`(?i)class="blog-articles__article`)
	// articleLinkRe extracts the /blogs/events-workshops/{slug} href.
	articleLinkRe = regexp.MustCompile(`(?i)href="(/blogs/events-workshops/[^"?#]+)"`)
	// articleTitleRe extracts the article title from the card heading anchor.
	articleTitleRe = regexp.MustCompile(`(?i)class="card__heading[^"]*"[^>]*>[\s\S]*?<a[^>]*>([^<]+)</a>`)
	// articleInfoRe extracts the article-card__info div content.
	articleInfoRe = regexp.MustCompile(`(?i)class="article-card__info"[^>]*>([\s\S]*?)</div>`)
	// tagStripRe removes HTML tags.
	tagStripRe = regexp.MustCompile(`<[^>]+>`)
	// nextPageRe detects whether a next page link exists.
	nextPageRe = regexp.MustCompile(`(?i)pagination__item--next`)
	// ordinalDateRe matches "1st May 2026", "11th Jul 2026", "2nd Jan 2025".
	ordinalDateRe = regexp.MustCompile(`(?i)(\d{1,2})(?:st|nd|rd|th)?\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\s+(\d{4})`)
	// articleSummaryRe extracts the first paragraph of the article body.
	articleSummaryRe = regexp.MustCompile(`(?i)<div[^>]*class="[^"]*rte[^"]*"[^>]*>([\s\S]*?)</div>`)
	pRe              = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
)

var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

func innerText(html string) string {
	s := tagStripRe.ReplaceAllString(html, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

// parseOrdinalDate parses "1st May 2026" → time.Time.
func parseOrdinalDate(s string, loc *time.Location) (time.Time, bool) {
	m := ordinalDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[1])
	month, ok := monthMap[strings.ToLower(m[2])]
	if !ok {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[3])
	if year == 0 {
		return time.Time{}, false
	}
	return time.Date(year, month, day, 10, 0, 0, 0, loc), true // default 10am
}

// fetchSummary fetches an article page and returns its first paragraph of body text.
func fetchSummary(ctx context.Context, url string) string {
	body, err := scraper.Fetch(ctx, url)
	if err != nil {
		return ""
	}
	dm := articleSummaryRe.FindSubmatch(body)
	if dm == nil {
		return ""
	}
	pm := pRe.FindSubmatch(dm[1])
	if pm == nil {
		return ""
	}
	return innerText(string(pm[1]))
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	seen := make(map[string]bool)
	var lectures []model.Lecture

	for page := 1; page <= maxPages; page++ {
		pageURL := listingURL
		if page > 1 {
			pageURL = fmt.Sprintf("%s?page=%d", listingURL, page)
		}

		body, err := scraper.Fetch(ctx, pageURL)
		if err != nil {
			return nil, fmt.Errorf("public-record: fetch page %d: %w", page, err)
		}

		html := string(body)

		// Split by article card marker.
		locs := articleRe.FindAllStringIndex(html, -1)
		for i, loc2 := range locs {
			end := len(html)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			chunk := html[loc2[0]:end]

			linkM := articleLinkRe.FindStringSubmatch(chunk)
			if linkM == nil {
				continue
			}
			href := linkM[1]
			if seen[href] {
				continue
			}
			seen[href] = true

			titleM := articleTitleRe.FindStringSubmatch(chunk)
			if titleM == nil {
				continue
			}
			title := strings.TrimSpace(titleM[1])
			if title == "" {
				continue
			}

			infoM := articleInfoRe.FindStringSubmatch(chunk)
			if infoM == nil {
				continue
			}
			dateText := innerText(infoM[1])

			t, ok := parseOrdinalDate(dateText, loc)
			if !ok {
				continue
			}

			link := baseURL + href
			summary := fetchSummary(ctx, link)

			lectures = append(lectures, model.Lecture{
				ID:        scraper.MakeID(link),
				Title:     scraper.CleanTitle(title),
				Link:      link,
				TimeStart: t,
				Summary:   scraper.TruncateSummary(summary, 200),
				Location:  evtLocation,
				HostSlug:  "public-record",
			})
		}

		// Stop if there's no next-page link.
		if !nextPageRe.MatchString(html) {
			break
		}
	}

	return lectures, nil
}
