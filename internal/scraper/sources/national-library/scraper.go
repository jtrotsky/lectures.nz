// Package nationallibrary scrapes public events from the National Library of New Zealand.
//
// TODO: Events are at https://natlib.govt.nz/events
// Look for JSON-LD or a structured events listing.
//
// For now returns seed data.
package nationallibrary

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "national-library",
		Name:        "National Library of New Zealand",
		Website:     "https://natlib.govt.nz/events",
		Description: "Te Puna Mātauranga o Aotearoa — the National Library hosts free public talks, exhibitions, and events celebrating New Zealand literature, history, and culture.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://natlib.govt.nz/events")
	// if err != nil { return nil, err }
	// return parseNatLibHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	return []model.Lecture{
		{
			ID:        scraper.MakeID("https://natlib.govt.nz/events/2026/04/writers-week-lecture"),
			Title:     "Writers & Readers: New Zealand Literary Identities",
			Link:      "https://natlib.govt.nz/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+8, 17, 30, 0, 0, loc),
			Summary:   "A conversation with three New Zealand authors about writing from the margins, finding a readership, and what it means to tell New Zealand stories in a globalised publishing market.",
			Free:      true,
			Location:  "National Library of New Zealand, Molesworth Street, Wellington",
			HostSlug:  "national-library",
		},
		{
			ID:        scraper.MakeID("https://natlib.govt.nz/events/2026/04/archives-talk"),
			Title:     "History Recovered: New Discoveries in the Alexander Turnbull Collection",
			Link:      "https://natlib.govt.nz/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+17, 12, 0, 0, 0, loc),
			Summary:   "Archivists and historians reveal recently catalogued items from the Alexander Turnbull Library collection, including previously unseen photographs and correspondence.",
			Free:      true,
			Location:  "National Library of New Zealand, Molesworth Street, Wellington",
			HostSlug:  "national-library",
		},
	}, nil
}
