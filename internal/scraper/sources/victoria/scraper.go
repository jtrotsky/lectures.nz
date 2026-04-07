// Package victoria scrapes public lecture events from Victoria University of Wellington
// (Te Herenga Waka).
//
// TODO: Victoria University's events are at https://www.wgtn.ac.nz/events
// The site uses a Drupal-based CMS. Events likely appear as <article> elements
// with classes like "views-row" or similar. Try:
//   https://www.wgtn.ac.nz/events?category=public-lectures
//   https://www.wgtn.ac.nz/events/rss (RSS feed if available)
//
// Look for JSON-LD structured data (<script type="application/ld+json">) on event pages,
// which is common with Drupal and provides clean machine-readable event data.
//
// For now this scraper returns realistic seed data.
package victoria

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

// Scraper implements scraper.Scraper for Victoria University of Wellington.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "victoria",
		Name:        "Victoria University of Wellington",
		Website:     "https://www.wgtn.ac.nz/events",
		Description: "Te Herenga Waka — Victoria University of Wellington hosts regular public lectures, seminars, and events across its faculties and research institutes.",
		Bluesky:     "@victoriauniversity.bsky.social",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.wgtn.ac.nz/events?category=public-lectures")
	// if err != nil { return nil, err }
	// return parseVictoriaHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	lectures := []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.wgtn.ac.nz/events/2026/04/democracy-disinformation"),
			Title:     "Democracy Under Pressure: Disinformation in the Digital Age",
			Link:      "https://www.wgtn.ac.nz/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+4, 18, 0, 0, 0, loc),
			Summary:   "How should democracies respond to coordinated disinformation campaigns? This public lecture draws on case studies from New Zealand and around the world.",
			Free:      true,
			Location:  "Hunter Council Chamber, Hunter Building, Kelburn Campus, Wellington",
			Speakers: []model.Speaker{
				{Name: "Professor Kate Hannah", Bio: "Director, The Disinformation Project, Te Pūnaha Matatini"},
			},
			HostSlug: "victoria",
		},
		{
			ID:        scraper.MakeID("https://www.wgtn.ac.nz/events/2026/04/antarctica-research"),
			Title:     "Life at the Frozen Edge: New Zealand's Role in Antarctic Research",
			Link:      "https://www.wgtn.ac.nz/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+9, 17, 30, 0, 0, loc),
			Summary:   "Antarctica New Zealand and Victoria University researchers discuss the latest findings from the ice, and New Zealand's unique responsibilities under the Antarctic Treaty.",
			Free:      true,
			Location:  "Rutherford House Lecture Theatre, 23 Lambton Quay, Wellington",
			HostSlug:  "victoria",
		},
		{
			ID:        scraper.MakeID("https://www.wgtn.ac.nz/events/2026/04/maori-economics"),
			Title:     "Te Ōhanga Māori: The Māori Economy in the 21st Century",
			Link:      "https://www.wgtn.ac.nz/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+14, 12, 0, 0, 0, loc),
			Summary:   "A lunchtime lecture examining the growth and diversity of the Māori economy, from kaitiakitanga-based enterprise to global investment.",
			Free:      true,
			Location:  "Pipitea Campus, 1 Bunny Street, Wellington",
			Speakers: []model.Speaker{
				{Name: "Dr Ganesh Nana", Bio: "Economist and researcher, former Chief Economic Advisor to Treasury"},
			},
			HostSlug: "victoria",
		},
	}

	return lectures, nil
}
