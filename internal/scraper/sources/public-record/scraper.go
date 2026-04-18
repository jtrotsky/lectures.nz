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
	// nextPageRe detects whether a next page link exists.
	nextPageRe = regexp.MustCompile(`(?i)pagination__item--next`)
	// freeRe detects "free" in a pricing/admission context on the article page.
	freeRe = regexp.MustCompile(`(?i)\bfree\b`)
	// ordinalDateRe matches "1st May 2026", "11th Jul 2026", "2nd Jan 2025".
	ordinalDateRe = regexp.MustCompile(`(?i)(\d{1,2})(?:st|nd|rd|th)?\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\s+(\d{4})`)
	// articleContentMarkerRe finds the article body div opening tag.
	articleContentMarkerRe = regexp.MustCompile(`(?i)<div[^>]*class="[^"]*article-template__content[^"]*"`)
	pRe                    = regexp.MustCompile(`(?i)<p[^>]*>([\s\S]*?)</p>`)
)

// parseOrdinalDate parses "1st May 2026" → time.Time.
func parseOrdinalDate(s string, loc *time.Location) (time.Time, bool) {
	m := ordinalDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[1])
	month, ok := scraper.AbbrevMonthMap[strings.ToLower(m[2])]
	if !ok {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[3])
	if year == 0 {
		return time.Time{}, false
	}
	return time.Date(year, month, day, 10, 0, 0, 0, loc), true // default 10am
}

type articleDetail struct {
	description string
	free        bool
}

// fetchDetail fetches a Public Record article page and returns the first
// paragraph of the article body plus whether the event is free.
func fetchDetail(ctx context.Context, url string) articleDetail {
	body, err := scraper.Fetch(ctx, url)
	if err != nil {
		return articleDetail{}
	}
	loc := articleContentMarkerRe.FindIndex(body)
	if loc == nil {
		return articleDetail{}
	}
	end := loc[1] + 8192
	if end > len(body) {
		end = len(body)
	}
	content := body[loc[1]:end]

	desc := ""
	all := pRe.FindAllSubmatch(content, -1)
	for _, pm := range all {
		text := scraper.InnerText(string(pm[1]))
		if text != "" {
			desc = text
			break
		}
	}

	free := freeRe.Match(content)
	return articleDetail{description: desc, free: free}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	loc := scraper.NZLocation

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
			dateText := scraper.InnerText(infoM[1])

			t, ok := parseOrdinalDate(dateText, loc)
			if !ok {
				continue
			}

			link := baseURL + href
			detail := fetchDetail(ctx, link)

			lectures = append(lectures, model.Lecture{
				ID:          scraper.MakeID(link),
				Title:       scraper.CleanTitle(title),
				Link:        link,
				TimeStart:   t,
				Description: detail.description,
				Summary:     scraper.TruncateSummary(detail.description, 200),
				Location:    evtLocation,
				Free:        detail.free,
				HostSlug:    "public-record",
			})
		}

		// Stop if there's no next-page link.
		if !nextPageRe.MatchString(html) {
			break
		}
	}

	return lectures, nil
}
