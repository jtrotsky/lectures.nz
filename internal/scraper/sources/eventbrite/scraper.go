// Package eventbrite scrapes public lecture events from curated NZ Eventbrite organizers.
//
// Many NZ academic departments and institutions publish events on Eventbrite rather than
// (or in addition to) their own portals. This scraper pulls from a curated list of
// known NZ educational organizers, plus a broad keyword search for NZ lecture events.
//
// Authentication:
//
//	Set EVENTBRITE_TOKEN to a personal OAuth token from https://www.eventbrite.com/account-settings/apps
//	If the token is absent, the scraper returns zero events without error.
//
// API reference: https://www.eventbrite.com/platform/api
package eventbrite

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

var tagRe = regexp.MustCompile(`<[^>]+>`)

const apiBase = "https://www.eventbriteapi.com/v3"

// organizer is a curated NZ educational Eventbrite organizer.
type organizer struct {
	id   string
	name string // human-readable, used in logs only
}

// knownOrganizers is the curated list of NZ educational organizers on Eventbrite.
// To add a new organizer find their Eventbrite profile URL:
//
//	https://www.eventbrite.co.nz/o/<slug>-<id>
//
// and add an entry here.
var knownOrganizers = []organizer{
	{"17255647886", "Faculty of Engineering and Design, University of Auckland"},
	{"52980282993", "Toi Rāuwharangi College of Creative Arts, Massey University"},
	{"16849540898", "University of Auckland, Business School"},
	{"8858370551", "Lincoln University"},
}

// Scraper implements scraper.Scraper for Eventbrite NZ.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "eventbrite",
		Name:        "Eventbrite NZ",
		Website:     "https://www.eventbrite.co.nz",
		Description: "Public lectures and educational events from NZ institutions listed on Eventbrite.",
	}
}

// apiEventName holds the name/description text returned by Eventbrite.
// HTML is populated for the description field only.
type apiEventName struct {
	Text string `json:"text"`
	HTML string `json:"html"`
}

// apiStructuredContent is the response from /v3/events/{id}/structured_content/.
type apiStructuredContent struct {
	Modules []apiModule `json:"modules"`
}

type apiModule struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// apiTextBody is the data payload for a module of type "text".
type apiTextBody struct {
	Body struct {
		Text string `json:"text"` // HTML
	} `json:"body"`
}

// apiSpeakersData is the data payload for a module of type "speakers".
// Some events use this first-class module; others embed speaker info in text.
type apiSpeakersData struct {
	Speakers struct {
		Speakers []struct {
			Profile struct {
				Name     string `json:"name"`
				Headline string `json:"headline"` // role, e.g. "Keynote Speaker"
			} `json:"profile"`
		} `json:"speakers"`
	} `json:"speakers"`
}

// apiTime holds a local datetime string and timezone from Eventbrite.
type apiTime struct {
	Timezone string `json:"timezone"`
	Local    string `json:"local"` // "2026-04-23T18:30:00"
}

// apiAddress holds venue address fields.
type apiAddress struct {
	Address1 string `json:"address_1"`
	City     string `json:"city"`
	Country  string `json:"country"`
}

// apiVenue holds venue information.
type apiVenue struct {
	Name    string     `json:"name"`
	Address apiAddress `json:"address"`
}

// apiEvent is a single event returned by the Eventbrite API.
type apiEvent struct {
	ID          string       `json:"id"`
	Name        apiEventName `json:"name"`
	Description apiEventName `json:"description"`
	URL         string       `json:"url"`
	Start       apiTime      `json:"start"`
	End         apiTime      `json:"end"`
	IsFree      bool         `json:"is_free"`
	OnlineEvent bool         `json:"online_event"`
	Venue       *apiVenue    `json:"venue"`
}

// apiPage is a paginated list of events from the Eventbrite API.
type apiPage struct {
	Events     []apiEvent `json:"events"`
	Pagination struct {
		HasMoreItems bool `json:"has_more_items"`
		PageNumber   int  `json:"page_number"`
	} `json:"pagination"`
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	token := os.Getenv("EVENTBRITE_TOKEN")
	if token == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var lectures []model.Lecture

	// Pass 1: curated organizer list.
	for _, org := range knownOrganizers {
		lecs, err := fetchOrganizerEvents(ctx, token, org)
		if err != nil {
			// Log but continue — one failing organizer shouldn't break the rest.
			fmt.Printf("eventbrite: organiser %s (%s): %v\n", org.id, org.name, err)
			continue
		}
		for _, l := range lecs {
			if !seen[l.ID] {
				seen[l.ID] = true
				l.Organiser = org.name
				lectures = append(lectures, l)
			}
		}
	}

	return lectures, nil
}

// apiGet performs an authenticated GET against the Eventbrite API and decodes JSON.
// It uses Bearer auth + JSON accept headers rather than the shared Fetch helper,
// which sets browser-like headers that cause Eventbrite to return HTML.
func apiGet(ctx context.Context, token, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", scraper.UserAgent)

	resp, err := scraper.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// fetchOrganizerEvents returns all upcoming live events for a single Eventbrite organizer.
func fetchOrganizerEvents(ctx context.Context, token string, org organizer) ([]model.Lecture, error) {
	var lectures []model.Lecture
	page := 1
	for {
		params := url.Values{}
		params.Set("status", "live")
		params.Set("expand", "venue")
		params.Set("page", fmt.Sprintf("%d", page))
		u := fmt.Sprintf("%s/organizers/%s/events/?%s", apiBase, org.id, params.Encode())

		var ap apiPage
		if err := apiGet(ctx, token, u, &ap); err != nil {
			return nil, err
		}
		for _, e := range ap.Events {
			l, ok := convertEvent(e)
			if !ok {
				continue
			}
			// Enrich description and speakers from structured content.
			if sc, err := fetchStructuredContent(ctx, token, e.ID); err == nil {
				if len(sc.Description) > len(l.Description) {
					l.Description = sc.Description
					l.Summary = scraper.TruncateSummary(sc.Description, 200)
				}
				if len(l.Speakers) == 0 && len(sc.Speakers) > 0 {
					l.Speakers = sc.Speakers
				}
			}
			lectures = append(lectures, l)
		}
		if !ap.Pagination.HasMoreItems {
			break
		}
		page++
	}
	return lectures, nil
}

// eventContent holds the result of fetchStructuredContent.
type eventContent struct {
	Description string
	Speakers    []model.Speaker
}

// fetchStructuredContent calls the structured_content endpoint and extracts
// the full description (from text modules) and any speakers (from either
// a first-class speakers module or patterns inside text HTML).
func fetchStructuredContent(ctx context.Context, token, eventID string) (eventContent, error) {
	u := fmt.Sprintf("%s/events/%s/structured_content/", apiBase, eventID)
	var sc apiStructuredContent
	if err := apiGet(ctx, token, u, &sc); err != nil {
		return eventContent{}, err
	}

	var htmlParts []string
	var speakers []model.Speaker

	for _, mod := range sc.Modules {
		switch mod.Type {
		case "text":
			var td apiTextBody
			if err := json.Unmarshal(mod.Data, &td); err == nil && td.Body.Text != "" {
				htmlParts = append(htmlParts, td.Body.Text)
			}
		case "speakers":
			var sd apiSpeakersData
			if err := json.Unmarshal(mod.Data, &sd); err == nil {
				for _, sp := range sd.Speakers.Speakers {
					if name := strings.TrimSpace(sp.Profile.Name); name != "" {
						speakers = append(speakers, model.Speaker{
							Name: name,
							Bio:  strings.TrimSpace(sp.Profile.Headline),
						})
					}
				}
			}
		}
	}

	combined := strings.Join(htmlParts, "\n")

	// If no first-class speakers module, try parsing speaker patterns from HTML.
	if len(speakers) == 0 && combined != "" {
		speakers = scraper.ExtractSpeakers([]byte(combined))
	}

	// Strip HTML tags to get plain description text.
	// Truncate to ~500 chars — structured content often includes speaker bios
	// and venue directions that make the description excessively long.
	description := strings.TrimSpace(tagRe.ReplaceAllString(combined, " "))
	description = strings.Join(strings.Fields(description), " ")
	description = scraper.TruncateSummary(description, 500)

	return eventContent{Description: description, Speakers: speakers}, nil
}

// convertEvent converts an Eventbrite API event to a model.Lecture.
// Returns false if the event should be skipped (online, outside NZ, no time, etc.).
func convertEvent(e apiEvent) (model.Lecture, bool) {
	if e.OnlineEvent {
		return model.Lecture{}, false
	}
	if e.Venue == nil {
		return model.Lecture{}, false
	}
	country := strings.ToUpper(e.Venue.Address.Country)
	if country != "NZ" && country != "NEW ZEALAND" && country != "" {
		return model.Lecture{}, false
	}

	if e.Start.Local == "" {
		return model.Lecture{}, false
	}

	loc, err := time.LoadLocation(e.Start.Timezone)
	if err != nil || loc == nil {
		loc, _ = time.LoadLocation("Pacific/Auckland")
		if loc == nil {
			loc = time.UTC
		}
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", e.Start.Local, loc)
	if err != nil {
		return model.Lecture{}, false
	}

	location := buildLocation(e.Venue)

	cleanTitle, speakerSuffix := scraper.SplitTitleSpeaker(scraper.CleanTitle(e.Name.Text))

	// Use description HTML stripped of tags when it's richer than plain text.
	description := e.Description.Text
	if e.Description.HTML != "" {
		htmlStripped := strings.TrimSpace(tagRe.ReplaceAllString(e.Description.HTML, " "))
		htmlStripped = strings.Join(strings.Fields(htmlStripped), " ")
		if len(htmlStripped) > len(description) {
			description = htmlStripped
		}
	}

	// Speakers from title suffix; HTML description provides a fallback.
	var speakers []model.Speaker
	if speakerSuffix != "" {
		speakers = []model.Speaker{{Name: speakerSuffix}}
	} else if e.Description.HTML != "" {
		speakers = scraper.ExtractSpeakers([]byte(e.Description.HTML))
	}

	return model.Lecture{
		ID:          scraper.MakeID(e.URL),
		Title:       cleanTitle,
		Link:        e.URL,
		TimeStart:   t,
		Description: description,
		Summary:     scraper.TruncateSummary(description, 200),
		Location:    location,
		Free:        e.IsFree,
		HostSlug:    "eventbrite",
		Speakers:    speakers,
	}, true
}

// buildLocation constructs a human-readable location string from a venue.
func buildLocation(v *apiVenue) string {
	if v == nil {
		return ""
	}
	parts := []string{}
	if v.Name != "" {
		parts = append(parts, v.Name)
	}
	if v.Address.Address1 != "" {
		parts = append(parts, v.Address.Address1)
	}
	if v.Address.City != "" {
		parts = append(parts, v.Address.City)
	}
	if len(parts) == 0 {
		return "New Zealand"
	}
	return strings.Join(parts, ", ")
}
