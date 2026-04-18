// Package meetup scrapes public events from curated Meetup.com groups.
//
// Meetup.com embeds event data in the page as a Next.js Apollo client cache
// under window.__NEXT_DATA__.props.pageProps.__APOLLO_STATE__. Each event
// appears as an "Event:{id}" key in the flat Apollo cache object.
//
// The JSON-LD approach no longer works as Meetup removed Event schema from
// their structured data (as of early 2026).
//
// To add a new group, append an entry to knownGroups.
package meetup

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

// apolloEvent mirrors the fields we need from each Apollo cache Event entry.
type apolloEvent struct {
	Typename    string `json:"__typename"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	EventURL    string `json:"eventUrl"`
	Description string `json:"description"`
	DateTime    string `json:"dateTime"`
	EndTime     string `json:"endTime"`
	IsOnline    bool   `json:"isOnline"`
	EventType   string `json:"eventType"`
	Status      string `json:"status"`
	FeeSettings *struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"feeSettings"`
	Venue *struct {
		Ref string `json:"__ref"`
	} `json:"venue"`
}

// apolloVenue mirrors the venue fields we need.
type apolloVenue struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
}

// nextData is the minimal shape of window.__NEXT_DATA__ we care about.
type nextData struct {
	Props struct {
		PageProps struct {
			ApolloState map[string]json.RawMessage `json:"__APOLLO_STATE__"`
		} `json:"pageProps"`
	} `json:"props"`
}

var nextDataRe = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(\{.*?\})</script>`)

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

	events, venues, err := extractApolloEvents(body)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	var lectures []model.Lecture
	for _, e := range events {
		if e.Status != "ACTIVE" {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.DateTime)
		if err != nil {
			continue
		}
		if t.Before(time.Now()) {
			continue
		}

		loc := buildLocation(e, venues)
		rawDesc := stripMarkdown(e.Description)
		free := e.FeeSettings == nil || e.FeeSettings.Amount == 0

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(e.EventURL),
			Title:       scraper.CleanTitle(e.Title),
			Link:        e.EventURL,
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

// extractApolloEvents pulls events and venues from the Next.js Apollo cache embedded in the page.
func extractApolloEvents(body []byte) ([]apolloEvent, map[string]apolloVenue, error) {
	// The __NEXT_DATA__ script tag contains the full Apollo state.
	// It can be very large; use a simple string search rather than regex to avoid
	// backtracking issues on large pages.
	const openTag = `<script id="__NEXT_DATA__" type="application/json">`
	const closeTag = `</script>`

	start := strings.Index(string(body), openTag)
	if start < 0 {
		return nil, nil, fmt.Errorf("__NEXT_DATA__ not found")
	}
	jsonStart := start + len(openTag)
	end := strings.Index(string(body)[jsonStart:], closeTag)
	if end < 0 {
		return nil, nil, fmt.Errorf("__NEXT_DATA__ closing tag not found")
	}

	var nd nextData
	if err := json.Unmarshal(body[jsonStart:jsonStart+end], &nd); err != nil {
		return nil, nil, fmt.Errorf("unmarshal __NEXT_DATA__: %w", err)
	}

	apolloState := nd.Props.PageProps.ApolloState
	if apolloState == nil {
		return nil, nil, fmt.Errorf("__APOLLO_STATE__ not found")
	}

	var events []apolloEvent
	venues := make(map[string]apolloVenue)

	for key, raw := range apolloState {
		if strings.HasPrefix(key, "Event:") {
			var e apolloEvent
			if err := json.Unmarshal(raw, &e); err == nil && e.Title != "" {
				events = append(events, e)
			}
		} else if strings.HasPrefix(key, "Venue:") {
			var v apolloVenue
			if err := json.Unmarshal(raw, &v); err == nil {
				venues[key] = v
			}
		}
	}

	return events, venues, nil
}

func buildLocation(e apolloEvent, venues map[string]apolloVenue) string {
	if e.IsOnline || e.EventType == "ONLINE" {
		return "Online"
	}
	if e.Venue != nil && e.Venue.Ref != "" {
		if v, ok := venues[e.Venue.Ref]; ok {
			parts := []string{}
			if v.Name != "" {
				parts = append(parts, v.Name)
			}
			if v.Address != "" {
				parts = append(parts, v.Address)
			}
			if v.City != "" {
				parts = append(parts, v.City)
			}
			if len(parts) > 0 {
				return strings.Join(parts, ", ")
			}
		}
	}
	return "Auckland"
}

// stripMarkdown removes Meetup's Markdown-like formatting from description text.
func stripMarkdown(s string) string {
	// Remove bold/italic markers.
	s = strings.NewReplacer("**", "", "__", "", "\\(", "(", "\\)", ")", "\\-", "-", "\\,", ",").Replace(s)
	// Collapse whitespace.
	return strings.Join(strings.Fields(s), " ")
}
