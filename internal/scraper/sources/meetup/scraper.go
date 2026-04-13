// Package meetup scrapes public events from curated Meetup.com groups.
//
// Meetup.com embeds structured event data as JSON-LD (schema.org/Event) in the
// group events listing page, so no API key or JS execution is required.
//
// To add a new group, append an entry to knownGroups.
package meetup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const meetupBase = "https://www.meetup.com"

// group is a curated Meetup.com group to scrape.
type group struct {
	slug string // URL slug, e.g. "prompt-poets-society-ai-vibe-coding-auckland"
	name string // human-readable, used in logs
}

var knownGroups = []group{
	{"prompt-poets-society-ai-vibe-coding-auckland", "Prompt Poets Society – AI & Vibe Coding Auckland"},
}

// Scraper implements scraper.Scraper for Meetup.com.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "meetup",
		Name:        "Meetup",
		Website:     "https://www.meetup.com",
		Description: "Public talks, workshops, and learning events from curated Auckland Meetup groups.",
	}
}

// ldEvent mirrors the schema.org/Event JSON-LD shape that Meetup.com embeds.
type ldEvent struct {
	Type        string  `json:"@type"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartDate   string  `json:"startDate"`
	EndDate     string  `json:"endDate"`
	URL         string  `json:"url"`
	IsAccessible *bool  `json:"isAccessibleForFree"`
	Location    ldPlace `json:"location"`
}

type ldPlace struct {
	Type    string    `json:"@type"`
	Name    string    `json:"name"`
	Address ldAddress `json:"address"`
}

type ldAddress struct {
	StreetAddress   string `json:"streetAddress"`
	AddressLocality string `json:"addressLocality"`
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	var all []model.Lecture
	for _, g := range knownGroups {
		lecs, err := scrapeGroup(ctx, g)
		if err != nil {
			fmt.Printf("meetup: group %s: %v\n", g.slug, err)
			continue
		}
		all = append(all, lecs...)
	}
	return all, nil
}

func scrapeGroup(ctx context.Context, g group) ([]model.Lecture, error) {
	u := fmt.Sprintf("%s/%s/events/", meetupBase, g.slug)
	body, err := scraper.Fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	events, err := extractLDEvents(body)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	var lectures []model.Lecture
	for _, e := range events {
		if strings.ToLower(e.Type) != "event" {
			continue
		}
		t, err := parseDateTime(e.StartDate, nzLoc)
		if err != nil {
			continue
		}
		if t.Before(time.Now()) {
			continue
		}

		loc := buildLocation(e.Location)
		rawDesc := stripHTML(e.Description)
		free := e.IsAccessible == nil || *e.IsAccessible // default to free if unset

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(e.URL),
			Title:       scraper.CleanTitle(e.Name),
			Link:        e.URL,
			TimeStart:   t,
			Description: rawDesc,
			Summary:     scraper.TruncateSummary(rawDesc, 200),
			Location:    loc,
			Free:        free,
			HostSlug:    "meetup",
		})
	}
	return lectures, nil
}

// extractLDEvents pulls all schema.org/Event objects from JSON-LD script tags
// in the page HTML. Meetup.com may emit a single object or an array.
func extractLDEvents(body []byte) ([]ldEvent, error) {
	const openTag = `<script type="application/ld+json">`
	const closeTag = `</script>`

	var all []ldEvent
	remaining := body
	for {
		start := bytes.Index(remaining, []byte(openTag))
		if start < 0 {
			break
		}
		jsonStart := start + len(openTag)
		end := bytes.Index(remaining[jsonStart:], []byte(closeTag))
		if end < 0 {
			break
		}
		chunk := bytes.TrimSpace(remaining[jsonStart : jsonStart+end])
		remaining = remaining[jsonStart+end:]

		// Try array first, then single object.
		var arr []ldEvent
		if err := json.Unmarshal(chunk, &arr); err == nil {
			all = append(all, arr...)
			continue
		}
		var single ldEvent
		if err := json.Unmarshal(chunk, &single); err == nil {
			all = append(all, single)
		}
	}
	return all, nil
}

// parseDateTime parses an ISO 8601 datetime string, falling back to NZ local time.
func parseDateTime(s string, loc *time.Location) (time.Time, error) {
	// Try with timezone offset first (e.g. "2026-04-15T18:00:00+12:00").
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try without offset, interpret as NZ time.
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognised datetime: %q", s)
}

func buildLocation(p ldPlace) string {
	parts := []string{}
	if p.Name != "" {
		parts = append(parts, p.Name)
	}
	if p.Address.StreetAddress != "" {
		parts = append(parts, p.Address.StreetAddress)
	}
	if p.Address.AddressLocality != "" {
		parts = append(parts, p.Address.AddressLocality)
	}
	if len(parts) == 0 {
		return "Auckland"
	}
	return strings.Join(parts, ", ")
}

// stripHTML removes HTML tags from a string for use in plain-text summaries.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse repeated spaces.
	out := strings.Join(strings.Fields(b.String()), " ")
	return out
}
