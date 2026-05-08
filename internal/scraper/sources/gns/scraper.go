// Package gns scrapes public events from GNS Science (now Earth Sciences New Zealand).
//
// GNS's news/events page uses a Vue-powered filtered search component backed by
// a JSON API at /news/search/. The endpoint requires a Referer header or it
// returns 403. The response body is JSON with a "content" key containing an
// HTML fragment of news tiles.
//
// Filtered out:
//   - Paid professional courses (title contains "course")
//   - Competitions / giveaways (title contains "be in to win")
//   - Multi-day professional symposiums, conferences, and workshops
//   - Past recording announcements ("now available to watch")
//
// GNS is based in Lower Hutt; all events default to Wellington.
package gns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	searchURL = "https://www.gns.cri.nz/news/search/?newsType=13&sort=latest"
	baseURL   = "https://www.gns.cri.nz"
	refererURL = "https://www.gns.cri.nz/news/"
)

// Scraper implements scraper.Scraper for GNS Science.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "gns",
		Name:        "GNS Science",
		City:        "Wellington",
		Website:     "https://www.gns.cri.nz/news/",
		Description: "GNS Science (now part of Earth Sciences New Zealand) hosts public talks, webinars, and community events on earthquakes, volcanoes, climate, and geoscience.",
	}
}

// searchResponse is the JSON wrapper returned by the GNS search API.
type searchResponse struct {
	Content    string `json:"content"`
	IsResults  bool   `json:"isResults"`
}

var (
	tileLinkRe  = regexp.MustCompile(`<a href="(/news/[^"]+)"[^>]*class="news-tile__link"`)
	tileTitleRe = regexp.MustCompile(`(?s)<h2 class="news-tile__title">\s*(.*?)\s*</h2>`)
	tileDateRe  = regexp.MustCompile(`<p class="news-tile__date">\s*([^<]+?)\s*</p>`)

	// excludeTitlePatterns filters non-public or non-lecture events.
	excludeTitlePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcourse\b`),
		regexp.MustCompile(`(?i)\bsymposium\b`),
		regexp.MustCompile(`(?i)\bworkshop\b`),
		regexp.MustCompile(`(?i)\bconference\b`),
		regexp.MustCompile(`(?i)\bmeeting\b`),
		regexp.MustCompile(`(?i)\bacademy\b`),
		regexp.MustCompile(`(?i)be in to win`),
		regexp.MustCompile(`(?i)now available to watch`),
	}
)

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := fetchWithReferer(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("gns: fetch search: %w", err)
	}

	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("gns: parse search response: %w", err)
	}
	if !resp.IsResults {
		return nil, nil
	}

	loc := scraper.NZLocation
	now := time.Now().In(loc)

	// Extract tiles from the HTML fragment.
	// Tiles are <a href="/news/slug/" class="news-tile__link">…</a> blocks.
	linkMatches := tileLinkRe.FindAllStringSubmatch(resp.Content, -1)
	titleMatches := tileTitleRe.FindAllStringSubmatch(resp.Content, -1)
	dateMatches := tileDateRe.FindAllStringSubmatch(resp.Content, -1)

	var lectures []model.Lecture
	seen := map[string]bool{}

	for i, lm := range linkMatches {
		if i >= len(titleMatches) || i >= len(dateMatches) {
			break
		}

		path := lm[1]
		link := baseURL + path
		if seen[link] {
			continue
		}

		title := scraper.CleanTitle(stripTags(titleMatches[i][1]))
		if title == "" {
			continue
		}

		if matchesAny(title, excludeTitlePatterns) {
			continue
		}

		dateStr := strings.TrimSpace(dateMatches[i][1])
		t, ok := parseGNSDate(dateStr, loc)
		if !ok || t.Before(now) {
			continue
		}

		seen[link] = true

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(link),
			Title:     title,
			Link:      link,
			TimeStart: t,
			Location:  "Wellington",
			Free:      strings.Contains(strings.ToLower(title), "free"),
			HostSlug:  "gns",
		})
	}

	return lectures, nil
}

// parseGNSDate parses "13 August 2024" from the tile listing.
// No time is available on the listing; defaults to 18:00.
func parseGNSDate(s string, loc *time.Location) (time.Time, bool) {
	// Format: "13 August 2024" or "1 August 2024"
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return time.Time{}, false
	}

	var day, year int
	fmt.Sscan(parts[0], &day)
	if day == 0 {
		return time.Time{}, false
	}
	month, ok := scraper.FullMonthMap[strings.ToLower(parts[1])]
	if !ok {
		return time.Time{}, false
	}
	fmt.Sscan(parts[2], &year)
	if year == 0 {
		return time.Time{}, false
	}

	return time.Date(year, month, day, 18, 0, 0, 0, loc), true
}

func fetchWithReferer(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", scraper.UserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-NZ,en;q=0.9")
	req.Header.Set("Referer", refererURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	buf := make([]byte, 0, 1<<16)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}
