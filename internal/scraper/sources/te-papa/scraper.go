// Package tepapa scrapes public events from Te Papa Tongarewa, the Museum of New Zealand.
//
// TODO: Events are at https://www.tepapa.govt.nz/visit/whats-on
// The site uses a Drupal CMS. Look for:
//   - JSON-LD structured data (<script type="application/ld+json">)
//   - An events API at /api/events or similar
//   - The events listing page HTML structure
//
// For now returns seed data.
package tepapa

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "te-papa",
		Name:        "Te Papa Tongarewa",
		Website:     "https://www.tepapa.govt.nz/visit/whats-on",
		Description: "The Museum of New Zealand Te Papa Tongarewa hosts lectures, symposia, and public programmes exploring Aotearoa's natural and cultural heritage.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.tepapa.govt.nz/visit/whats-on")
	// if err != nil { return nil, err }
	// return parseTePapaHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	return []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.tepapa.govt.nz/events/2026/04/maori-collections-symposium"),
			Title:     "Taonga Māori Symposium: Collections, Repatriation, and Digital Futures",
			Link:      "https://www.tepapa.govt.nz/visit/whats-on",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+6, 10, 0, 0, 0, loc),
			Summary:   "Iwi representatives, curators, and digital archivists discuss the future of taonga Māori in national collections, repatriation processes, and digital access.",
			Free:      true,
			Location:  "Te Papa Tongarewa, 55 Cable Street, Wellington",
			HostSlug:  "te-papa",
		},
		{
			ID:        scraper.MakeID("https://www.tepapa.govt.nz/events/2026/04/ocean-lecture"),
			Title:     "Moana: New Research on New Zealand's Marine Environment",
			Link:      "https://www.tepapa.govt.nz/visit/whats-on",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+15, 18, 0, 0, 0, loc),
			Summary:   "Marine biologists and ocean scientists present the latest findings from New Zealand's vast exclusive economic zone, including deep-sea discoveries and climate impacts.",
			Free:      true,
			Location:  "Te Papa Tongarewa, 55 Cable Street, Wellington",
			HostSlug:  "te-papa",
		},
	}, nil
}
