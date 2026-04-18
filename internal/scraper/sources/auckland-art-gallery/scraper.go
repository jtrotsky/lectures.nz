// Package aucklandartgallery scrapes public events from Auckland Art Gallery Toi o Tāmaki.
//
// The site is a React SPA. Event URLs follow the pattern /whats-on/event/{slug} and are
// linked from the /visit/whats-on listing page (previously /whats-on, now a 301 redirect).
// Each event detail page embeds its data as:
//
//	window.__INITIAL_STATE__ = {...};
//
// The listing page pre-loads a fixed set of event slugs in its initial HTML (both as
// plain hrefs and as Unicode-escaped JSON strings like \u002Fwhats-on\u002Fevent\u002F{slug}).
// We extract all slugs from the listing page, fetch each event detail page, and parse
// the embedded JSON for title, date, location, cost, and description. Events are filtered
// to future dates and lecture-like titles.
//
// Note: the authenticated API (api.aucklandunlimited.com/v2/aag/events) returns the full
// event catalogue, but requires credentials. The static HTML approach is limited to ~30
// pre-loaded slugs per page load.
package aucklandartgallery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	// listingURL redirected from /whats-on in early 2026.
	listingURL = "https://www.aucklandartgallery.com/visit/whats-on"
	baseURL    = "https://www.aucklandartgallery.com"
	maxEvents  = 30
)

type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "auckland-art-gallery",
		Name:        "Auckland Art Gallery Toi o Tāmaki",
		Website:     "https://www.aucklandartgallery.com/whats-on",
		Description: "New Zealand's largest art collection, hosting regular public talks, tours, artist lectures, and symposia.",
	}
}

// eventSlugRe matches /whats-on/event/{slug} in plain hrefs.
var eventSlugRe = regexp.MustCompile(`/whats-on/event/([a-z0-9\-]+)`)

// eventSlugUnicodeRe matches Unicode-escaped paths (\u002Fwhats-on\u002Fevent\u002F{slug})
// embedded in the Next.js page JSON.
var eventSlugUnicodeRe = regexp.MustCompile(`\\u002Fwhats-on\\u002Fevent\\u002F([a-z0-9\-]+)`)

// lectureKeywords — event titles we consider lecture-like.
var lectureKeywords = []string{
	"talk", "lecture", "tour", "artist", "curator", "symposium",
	"panel", "seminar", "workshop", "discussion", "conversation",
	"forum", "presentation", "in conversation",
}

func isLectureLike(title string) bool {
	lower := strings.ToLower(title)
	for _, kw := range lectureKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("auckland-art-gallery: fetch listing: %w", err)
	}

	seen := make(map[string]bool)
	var slugs []string
	addSlug := func(slug string) {
		if !seen[slug] && len(slugs) < maxEvents {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
	}
	for _, m := range eventSlugRe.FindAllSubmatch(body, -1) {
		addSlug(string(m[1]))
	}
	for _, m := range eventSlugUnicodeRe.FindAllSubmatch(body, -1) {
		addSlug(string(m[1]))
	}

	if len(slugs) == 0 {
		// Site may have restructured — return empty rather than error.
		return nil, nil
	}

	nzLoc := scraper.NZLocation
	now := time.Now()

	var lectures []model.Lecture
	for _, slug := range slugs {
		eventURL := baseURL + "/whats-on/event/" + slug
		lec, err := scrapeEventPage(ctx, eventURL, nzLoc)
		if err != nil || lec == nil {
			continue
		}
		if lec.TimeStart.Before(now) {
			continue
		}
		if !isLectureLike(lec.Title) {
			continue
		}
		lectures = append(lectures, *lec)
	}

	return lectures, nil
}

func scrapeEventPage(ctx context.Context, eventURL string, loc *time.Location) (*model.Lecture, error) {
	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return nil, err
	}

	raw := extractInitialState(body)
	if raw == nil {
		return nil, nil
	}

	var state map[string]interface{}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, nil
	}

	event := findEventData(state)
	if event == nil {
		return nil, nil
	}

	title := stringField(event, "name", "title")
	if title == "" {
		return nil, nil
	}

	startMs := int64Field(event, "startDate", "start_date")
	if startMs == 0 {
		return nil, nil
	}
	t := time.Unix(startMs/1000, 0).In(loc)

	location := stringField(event, "location")
	if location == "" {
		location = "Auckland Art Gallery Toi o Tāmaki, Cnr Kitchener & Wellesley Streets, Auckland"
	} else {
		location += ", Auckland Art Gallery Toi o Tāmaki, Auckland"
	}

	cost := stringField(event, "cost", "price")
	free := cost == "" || strings.EqualFold(cost, "free")

	summary := strings.TrimSpace(stripHTMLTags(stringField(event, "description", "summary")))
	if len(summary) > 300 {
		summary = summary[:300] + "…"
	}

	return &model.Lecture{
		ID:        scraper.MakeID(eventURL),
		Title:     scraper.CleanTitle(title),
		Link:      eventURL,
		TimeStart: t,
		Description: summary,
		Summary:     scraper.TruncateSummary(summary, 200),
		Location:    location,
		Free:        free,
		Cost:        cost,
		HostSlug:    "auckland-art-gallery",
	}, nil
}

// extractInitialState finds and returns the JSON blob from window.__INITIAL_STATE__ = {...};
func extractInitialState(body []byte) []byte {
	const marker = "window.__INITIAL_STATE__"
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		return nil
	}
	start := bytes.IndexByte(body[idx:], '{')
	if start < 0 {
		return nil
	}
	start += idx

	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(body); i++ {
		c := body[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inStr {
			escaped = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return body[start : i+1]
			}
		}
	}
	return nil
}

// findEventData searches the __INITIAL_STATE__ map for an object with both "name" and "startDate".
func findEventData(m map[string]interface{}) map[string]interface{} {
	if _, ok := m["startDate"]; ok {
		if _, ok2 := m["name"]; ok2 {
			return m
		}
	}
	for _, v := range m {
		if child, ok := v.(map[string]interface{}); ok {
			if found := findEventData(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func stringField(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func int64Field(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if n, ok := v.(float64); ok && n != 0 {
				return int64(n)
			}
		}
	}
	return 0
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTags(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}
