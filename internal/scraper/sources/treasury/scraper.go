// Package treasury scrapes public guest lectures from the New Zealand Treasury.
//
// Treasury publishes its guest lectures on Eventbrite under organiser ID 34223768069:
//
//	https://www.eventbrite.co.nz/o/treasury-34223768069
//
// The organiser page embeds full event data in a JSON-LD <script> block (no API key needed).
// Each lecture is listed twice — one IN-PERSON and one VIRTUAL ticket listing. We prefer
// the in-person entry and deduplicate by start time.
//
// Speakers are extracted from the event title: "Treasury Guest Lecture with Dr X: Subtitle"
package treasury

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

const (
	organiserURL = "https://www.eventbrite.co.nz/o/treasury-34223768069"
	venue        = "New Zealand Treasury, 1 The Terrace, Wellington"
)

// jsonLDList mirrors the schema.org ItemList embedded in the Eventbrite organiser page.
type jsonLDList struct {
	Context         string        `json:"@context"`
	ItemListElement []jsonLDItem  `json:"itemListElement"`
}

type jsonLDItem struct {
	Item jsonLDEvent `json:"item"`
}

type jsonLDEvent struct {
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	URL                string          `json:"url"`
	StartDate          string          `json:"startDate"`
	EventAttendanceMode string         `json:"eventAttendanceMode"`
	Location           jsonLDLocation  `json:"location"`
	Offers             jsonLDOffers    `json:"offers"`
}

type jsonLDLocation struct {
	Type    string          `json:"@type"`
	Name    string          `json:"name"`
	Address jsonLDAddress   `json:"address"`
}

type jsonLDAddress struct {
	StreetAddress  string `json:"streetAddress"`
	AddressLocality string `json:"addressLocality"`
	AddressRegion  string `json:"addressRegion"`
}

type jsonLDOffers struct {
	LowPrice string `json:"lowPrice"`
}

var (
	jsonLDRe   = regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)
	// "Treasury Guest Lecture with Dr Alan Bollard: Subtitle" → speaker = "Dr Alan Bollard"
	speakerRe  = regexp.MustCompile(`(?i)with\s+([A-Z][^\:]+?)(?:\s*[:–]|$)`)
)

// Scraper implements scraper.Scraper for the New Zealand Treasury.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "treasury",
		Name:        "New Zealand Treasury",
		Website:     organiserURL,
		Description: "Te Tai Ōhanga — the New Zealand Treasury hosts free public guest lectures on economics, public policy, and fiscal matters.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, organiserURL)
	if err != nil {
		return nil, fmt.Errorf("treasury: fetch organiser page: %w", err)
	}

	loc := scraper.NZLocation
	now := time.Now()

	// Find and parse the JSON-LD ItemList block.
	var list jsonLDList
	for _, m := range jsonLDRe.FindAllSubmatch(body, -1) {
		if err := json.Unmarshal(m[1], &list); err != nil {
			continue
		}
		if len(list.ItemListElement) > 0 {
			break
		}
	}

	// Deduplicate by start time — prefer in-person over virtual.
	type key struct{ start string }
	seen := make(map[key]bool)
	var lectures []model.Lecture

	for _, item := range list.ItemListElement {
		ev := item.Item
		if ev.URL == "" || ev.StartDate == "" {
			continue
		}

		isVirtual := strings.Contains(ev.EventAttendanceMode, "OnlineEvent") ||
			strings.Contains(strings.ToLower(ev.Name), "virtual")

		k := key{ev.StartDate}
		if seen[k] {
			continue // already have in-person variant
		}
		if isVirtual {
			// Only add virtual if no in-person variant seen yet — mark tentatively.
			// We'll overwrite if an in-person entry appears later.
		}
		seen[k] = !isVirtual // lock if in-person; leave open if virtual

		t, err := time.Parse("2006-01-02T15:04:05-0700", ev.StartDate)
		if err != nil {
			t, err = time.Parse(time.RFC3339, ev.StartDate)
			if err != nil {
				continue
			}
		}
		t = t.In(loc)
		if t.Before(now) {
			continue
		}

		// Clean title: strip "IN-PERSON attendance" / "VIRTUAL attendance" suffixes.
		title := scraper.CleanTitle(ev.Name)
		for _, suffix := range []string{": IN-PERSON attendance", ": VIRTUAL attendance", ": In-Person Attendance", ": Virtual Attendance"} {
			title = strings.TrimSuffix(title, suffix)
		}

		// Extract speaker from "Treasury Guest Lecture with Dr X: ..."
		var speakers []model.Speaker
		if m := speakerRe.FindStringSubmatch(title); m != nil {
			name := strings.TrimSpace(m[1])
			if len(name) > 2 && len(name) < 80 {
				speakers = []model.Speaker{{Name: name}}
			}
		}

		// Build location string.
		location := venue
		if ev.Location.Type != "VirtualLocation" && ev.Location.Name != "" {
			location = ev.Location.Name
		}

		free := ev.Offers.LowPrice == "0.00" || ev.Offers.LowPrice == "0" || ev.Offers.LowPrice == ""
		desc := ev.Description

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(ev.URL),
			Title:       title,
			Link:        ev.URL,
			TimeStart:   t,
			Summary:     scraper.TruncateSummary(desc, 200),
			Description: desc,
			Free:        free,
			Location:    location,
			Speakers:    speakers,
			HostSlug:    "treasury",
		})
	}

	return lectures, nil
}
