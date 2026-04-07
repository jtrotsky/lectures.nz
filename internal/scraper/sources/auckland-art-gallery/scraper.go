// Package aucklandartgallery scrapes public events from Auckland Art Gallery Toi o Tāmaki.
//
// TODO: Events are listed at https://www.aucklandartgallery.com/whats-on
// The site uses a standard CMS. Check for:
//   - JSON-LD structured data on event pages
//   - An events API endpoint (inspect network tab)
//   - RSS feed at /whats-on/rss or similar
//
// For now returns seed data.
package aucklandartgallery

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
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

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.aucklandartgallery.com/whats-on")
	// if err != nil { return nil, err }
	// return parseAAGHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	return []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.aucklandartgallery.com/events/2026/04/artist-talk-mataaho-collective"),
			Title:     "Artist Talk: Mataaho Collective",
			Link:      "https://www.aucklandartgallery.com/whats-on",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+5, 12, 0, 0, 0, loc),
			Summary:   "Join the Mataaho Collective for an in-conversation event exploring their practice, the significance of tukutuku patterning, and their recent international commissions.",
			Free:      true,
			Location:  "Auckland Art Gallery Toi o Tāmaki, Cnr Kitchener & Wellesley Streets, Auckland",
			HostSlug:  "auckland-art-gallery",
		},
		{
			ID:        scraper.MakeID("https://www.aucklandartgallery.com/events/2026/04/curator-tour-modernism"),
			Title:     "Curator's Tour: New Zealand Modernism",
			Link:      "https://www.aucklandartgallery.com/whats-on",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+11, 14, 0, 0, 0, loc),
			Summary:   "Senior curator guides visitors through the gallery's collection of New Zealand modernist works, discussing the movement's emergence and its tensions with international influences.",
			Free:      false,
			Cost:      "$15",
			Location:  "Auckland Art Gallery Toi o Tāmaki, Cnr Kitchener & Wellesley Streets, Auckland",
			HostSlug:  "auckland-art-gallery",
		},
	}, nil
}
