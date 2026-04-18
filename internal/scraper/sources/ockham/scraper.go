// Package ockham scrapes events related to the Ockham New Zealand Book Awards
// and associated literary lectures.
//
// TODO: The Booksellers NZ site (https://www.booksellers.co.nz/) hosts Ockham
// award event listings. The events are likely under /events or /ockham-nz-book-awards.
// Check:
//   https://www.booksellers.co.nz/ockham
//   https://www.booksellers.co.nz/events
//
// The site appears to use a standard CMS (possibly WordPress or similar).
// Look for <article> tags with event schema markup or an RSS/JSON feed.
//
// For now this scraper returns realistic seed data.
package ockham

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

// Scraper implements scraper.Scraper for Ockham NZ literary events.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "ockham",
		Name:        "Ockham New Zealand Book Awards",
		Website:     "https://www.booksellers.co.nz/ockham",
		Description: "New Zealand's premier literary prize, celebrating the best in New Zealand literature with public events, readings, and lectures throughout the year.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.booksellers.co.nz/events")
	// if err != nil { return nil, err }
	// return parseOckhamHTML(body)

	now := time.Now()
	loc := scraper.NZLocation

	lectures := []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.booksellers.co.nz/ockham/2026/longlist-celebration"),
			Title:     "Ockham NZ Book Awards 2026 Longlist Celebration",
			Link:      "https://www.booksellers.co.nz/ockham",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+5, 18, 0, 0, 0, loc),
			Summary:   "Join us to celebrate the announcement of the 2026 Ockham New Zealand Book Awards longlist, with readings from longlisted authors across all four categories.",
			Free:      false,
			Cost:      "$25 (includes welcome drink)",
			Location:  "Auckland Writers Festival Hub, Aotea Centre, Auckland",
			HostSlug:  "ockham",
		},
		{
			ID:        scraper.MakeID("https://www.booksellers.co.nz/ockham/2026/fiction-panel"),
			Title:     "In Conversation: The State of New Zealand Fiction",
			Link:      "https://www.booksellers.co.nz/ockham",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+12, 17, 0, 0, 0, loc),
			Summary:   "A panel discussion with this year's fiction finalists exploring the themes, craft, and challenges of writing contemporary New Zealand fiction.",
			Free:      true,
			Location:  "Wellington Central Library, 65 Victoria Street, Wellington",
			Speakers: []model.Speaker{
				{Name: "Paula Morris", Bio: "Award-winning New Zealand novelist and Creative Writing academic"},
				{Name: "Pip Adam", Bio: "Author and publisher, longlisted for the Acorn Prize"},
			},
			HostSlug: "ockham",
		},
	}

	return lectures, nil
}
