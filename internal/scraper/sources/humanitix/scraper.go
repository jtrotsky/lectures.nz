// Package humanitix scrapes public lecture events from curated NZ Humanitix organisers.
//
// Humanitix is a NZ/AU not-for-profit ticketing platform used by educational institutions
// and cultural organisations. This scraper pulls from a curated list of known NZ
// educational organisers.
//
// Discovery approach:
//
//	Each organiser has a profile page at events.humanitix.com/host/{slug} which
//	embeds an ItemList JSON-LD listing all upcoming events. We scrape that to
//	discover event URLs, then fetch each event page for its full Event JSON-LD.
//
// Note: The Humanitix public API (api.humanitix.com/v1) only returns events
// owned by the authenticated account, so we scrape the public pages instead.
package humanitix

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const hostBase = "https://events.humanitix.com/host"

// organiser is a curated NZ educational Humanitix organiser.
type organiser struct {
	slug string // e.g. "maxim-institute"
	name string // human-readable, used in logs
}

// knownOrganisers is the curated list of NZ educational organisers on Humanitix.
// To add a new organiser, find their profile at events.humanitix.com/host/{slug}.
var knownOrganisers = []organiser{
	{"maxim-institute", "Maxim Institute"},
	{"deloitte-techweek26", "Deloitte TechWeek26"},
	{"nz-fabian-society", "NZ Fabian Society"},
}

// Scraper implements scraper.Scraper for Humanitix NZ.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "humanitix",
		Name:        "Humanitix NZ",
		Website:     "https://humanitix.com/nz",
		Description: "Public lectures and educational events from NZ institutions listed on Humanitix.",
	}
}

// jsonLDEvent is the schema.org Event structure embedded in event pages.
type jsonLDEvent struct {
	Type               string      `json:"@type"`
	Name               string      `json:"name"`
	URL                string      `json:"url"`
	StartDate          string      `json:"startDate"`
	EndDate            string      `json:"endDate"`
	Description        string      `json:"description"`
	EventStatus        string      `json:"eventStatus"`
	EventAttendanceMode string     `json:"eventAttendanceMode"`
	Location           jsonLDPlace `json:"location"`
	Offers             []jsonLDOffer `json:"offers"`
}

type jsonLDPlace struct {
	Name    string         `json:"name"`
	Address jsonLDAddress  `json:"address"`
}

type jsonLDAddress struct {
	StreetAddress   string `json:"streetAddress"`
	AddressLocality string `json:"addressLocality"`
	AddressCountry  string `json:"addressCountry"`
	PostalCode      string `json:"postalCode"`
}

type jsonLDOffer struct {
	Type  string  `json:"@type"`
	Price float64 `json:"price"`
}

// jsonLDItemList is the schema.org ItemList embedded in host profile pages.
type jsonLDItemList struct {
	Type            string           `json:"@type"`
	ItemListElement []jsonLDListItem `json:"itemListElement"`
}

type jsonLDListItem struct {
	Type     string      `json:"@type"`
	Position int         `json:"position"`
	Item     jsonLDEvent `json:"item"`
}

var (
	ldJSONRe   = regexp.MustCompile(`(?s)<script[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	hostedByRe = regexp.MustCompile(`(?i)[Hh]osted by ([^.<"]+)`)
	metaDescRe = regexp.MustCompile(`<meta name="description" content="([^"]+)"`)
)

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	seen := make(map[string]bool)
	var all []model.Lecture

	for _, org := range knownOrganisers {
		lecs, err := scrapeOrganiser(ctx, org)
		if err != nil {
			fmt.Printf("humanitix: organiser %s: %v\n", org.slug, err)
			continue
		}
		for _, l := range lecs {
			if !seen[l.ID] {
				seen[l.ID] = true
				all = append(all, l)
			}
		}
	}
	return all, nil
}

// scrapeOrganiser fetches the host profile page, extracts event URLs from the
// ItemList JSON-LD, then scrapes each event page for full event data.
func scrapeOrganiser(ctx context.Context, org organiser) ([]model.Lecture, error) {
	profileURL := fmt.Sprintf("%s/%s", hostBase, org.slug)
	body, err := scraper.Fetch(ctx, profileURL)
	if err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}

	eventURLs, err := extractEventURLs(body)
	if err != nil {
		return nil, fmt.Errorf("extract event URLs: %w", err)
	}
	if len(eventURLs) == 0 {
		fmt.Printf("humanitix: no upcoming events found for %s\n", org.slug)
		return nil, nil
	}

	var lectures []model.Lecture
	for _, u := range eventURLs {
		l, ok, err := scrapeEvent(ctx, u)
		if err != nil {
			fmt.Printf("humanitix: event %s: %v\n", u, err)
			continue
		}
		if !ok {
			continue
		}
		lectures = append(lectures, l)
	}
	return lectures, nil
}

// extractEventURLs pulls event page URLs from the ItemList JSON-LD on a host profile page.
func extractEventURLs(body []byte) ([]string, error) {
	matches := ldJSONRe.FindAllSubmatch(body, -1)
	for _, m := range matches {
		var il jsonLDItemList
		if err := json.Unmarshal(m[1], &il); err != nil {
			continue
		}
		if il.Type != "ItemList" {
			continue
		}
		var urls []string
		for _, item := range il.ItemListElement {
			if u := item.Item.URL; u != "" {
				urls = append(urls, u)
			}
		}
		return urls, nil
	}
	return nil, nil
}

// scrapeEvent fetches a single event page and returns a Lecture.
// Returns false if the event should be skipped (online, outside NZ, cancelled, etc.).
func scrapeEvent(ctx context.Context, eventURL string) (model.Lecture, bool, error) {
	body, err := scraper.Fetch(ctx, eventURL)
	if err != nil {
		return model.Lecture{}, false, fmt.Errorf("fetch: %w", err)
	}

	ev, ok := extractEventLD(body)
	if !ok {
		return model.Lecture{}, false, fmt.Errorf("no Event JSON-LD found")
	}

	// Skip cancelled or postponed events.
	if strings.Contains(ev.EventStatus, "EventCancelled") ||
		strings.Contains(ev.EventStatus, "EventPostponed") {
		return model.Lecture{}, false, nil
	}

	// Skip online-only events.
	if strings.Contains(ev.EventAttendanceMode, "OnlineEventAttendanceMode") {
		return model.Lecture{}, false, nil
	}

	// Skip events outside NZ.
	country := strings.ToUpper(ev.Location.Address.AddressCountry)
	if country != "" && country != "NZ" && country != "NEW ZEALAND" {
		return model.Lecture{}, false, nil
	}

	t, err := time.Parse(time.RFC3339, ev.StartDate)
	if err != nil {
		// Humanitix uses "+1200" offset — try without colon
		t, err = time.Parse("2006-01-02T15:04:05-0700", ev.StartDate)
		if err != nil {
			return model.Lecture{}, false, fmt.Errorf("parse start date %q: %w", ev.StartDate, err)
		}
	}

	var endTime *time.Time
	if ev.EndDate != "" {
		te, err := time.Parse(time.RFC3339, ev.EndDate)
		if err != nil {
			te, err = time.Parse("2006-01-02T15:04:05-0700", ev.EndDate)
			if err == nil {
				endTime = &te
			}
		} else {
			endTime = &te
		}
	}

	isFree := isFreeEvent(ev.Offers)
	location := buildLocation(ev.Location)
	organiserName := extractOrganiser(body)
	description := html.UnescapeString(ev.Description)
	cleanTitle, speakerSuffix := scraper.SplitTitleSpeaker(scraper.CleanTitle(ev.Name))

	var speakers []model.Speaker
	if speakerSuffix != "" {
		speakers = []model.Speaker{{Name: speakerSuffix}}
	}

	return model.Lecture{
		ID:          scraper.MakeID(ev.URL),
		Title:       cleanTitle,
		Link:        ev.URL,
		TimeStart:   t,
		TimeEnd:     endTime,
		Description: description,
		Summary:     scraper.TruncateSummary(description, 200),
		Location:    location,
		Free:        isFree,
		HostSlug:    "humanitix",
		Organiser:   organiserName,
		Speakers:    speakers,
	}, true, nil
}

// extractEventLD finds the schema.org Event JSON-LD block in a Humanitix event page.
func extractEventLD(body []byte) (jsonLDEvent, bool) {
	matches := ldJSONRe.FindAllSubmatch(body, -1)
	for _, m := range matches {
		var ev jsonLDEvent
		if err := json.Unmarshal(m[1], &ev); err != nil {
			continue
		}
		if ev.Type == "Event" && ev.Name != "" {
			return ev, true
		}
	}
	return jsonLDEvent{}, false
}

// extractOrganiser pulls the organiser name from the "hosted by X" pattern in
// the page's meta description tag.
func extractOrganiser(body []byte) string {
	metaMatch := metaDescRe.FindSubmatch(body)
	if metaMatch == nil {
		return ""
	}
	desc := html.UnescapeString(string(metaMatch[1]))
	hbMatch := hostedByRe.FindStringSubmatch(desc)
	if hbMatch == nil {
		return ""
	}
	return strings.TrimSpace(hbMatch[1])
}

// isFreeEvent returns true if all individual offers have price 0.
// Uses the non-aggregate Offer entries only.
func isFreeEvent(offers []jsonLDOffer) bool {
	hasOffer := false
	for _, o := range offers {
		if o.Type != "Offer" {
			continue
		}
		hasOffer = true
		if o.Price > 0 {
			return false
		}
	}
	return hasOffer
}

// buildLocation constructs a human-readable location string from a schema.org Place.
func buildLocation(place jsonLDPlace) string {
	parts := []string{}
	if place.Name != "" {
		parts = append(parts, html.UnescapeString(place.Name))
	}
	if place.Address.AddressLocality != "" {
		parts = append(parts, place.Address.AddressLocality)
	}
	if len(parts) == 0 {
		return "New Zealand"
	}
	return strings.Join(parts, ", ")
}
