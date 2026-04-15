// Package nationallibrary scrapes public events from the National Library of New Zealand.
//
// The events listing is at https://natlib.govt.nz/events. However, the site is protected
// by Imperva/Incapsula and returns a bot-challenge page when accessed from server environments.
// The scraper attempts a real fetch but returns empty on bot-block rather than fake seed data.
//
// To fix: if natlib.govt.nz removes Incapsula protection, implement HTML parsing of their
// events listing. Events are rendered as <article> elements with date and title fields.
package nationallibrary

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	eventsURL = "https://natlib.govt.nz/events"
	baseURL   = "https://natlib.govt.nz"
)

var (
	articleRe = regexp.MustCompile(`(?s)<article[^>]*>(.*?)</article>`)
	titleRe   = regexp.MustCompile(`<h[23][^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
	dateRe    = regexp.MustCompile(`<time[^>]*datetime="([^"]+)"`)
	summaryRe = regexp.MustCompile(`<p[^>]*class="[^"]*summary[^"]*"[^>]*>([\s\S]*?)</p>`)
)

type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "national-library",
		Name:        "National Library of New Zealand",
		Website:     eventsURL,
		Description: "Te Puna Mātauranga o Aotearoa — the National Library hosts free public talks, exhibitions, and events celebrating New Zealand literature, history, and culture.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, eventsURL)
	if err != nil {
		return nil, nil // network error — skip gracefully
	}

	// Incapsula bot-block returns a small page with no real content.
	if bytes.Contains(body, []byte("Incapsula")) || bytes.Contains(body, []byte("NOINDEX, NOFOLLOW")) {
		return nil, nil // bot-blocked — skip gracefully
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now()

	var lectures []model.Lecture
	for _, m := range articleRe.FindAllSubmatch(body, -1) {
		article := m[1]

		tm := titleRe.FindSubmatch(article)
		if tm == nil {
			continue
		}
		href := string(tm[1])
		title := strings.TrimSpace(string(tm[2]))
		if title == "" {
			continue
		}

		link := href
		if !strings.HasPrefix(link, "http") {
			link = baseURL + link
		}

		var t time.Time
		if dm := dateRe.FindSubmatch(article); dm != nil {
			t, _ = time.Parse(time.RFC3339, string(dm[1]))
			if t.IsZero() {
				t, _ = time.ParseInLocation("2006-01-02", string(dm[1]), loc)
			}
		}
		if t.IsZero() || t.Before(now) {
			continue
		}

		var summary string
		if sm := summaryRe.FindSubmatch(article); sm != nil {
			summary = scraper.TruncateSummary(stripTags(string(sm[1])), 200)
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(link),
			Title:       scraper.CleanTitle(title),
			Link:        link,
			TimeStart:   t,
			Summary:     summary,
			Description: summary,
			Free:        true,
			Location:    "National Library of New Zealand, Molesworth Street, Wellington",
			HostSlug:    "national-library",
		})
	}

	return lectures, nil
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.Join(strings.Fields(s), " ")
}
