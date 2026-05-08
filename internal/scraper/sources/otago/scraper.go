// Package otago scrapes public lecture events from the University of Otago.
//
// Otago's entire otago.ac.nz domain is behind Cloudflare's managed challenge —
// all subpaths (events, search, homepage) return a JS challenge page.
// Their iCal feed (/news/events/public-lectures?format=ical) exists but is
// equally blocked. No accessible API or feed was found.
//
// Returns 0 events until Otago's Cloudflare config changes or another feed appears.
package otago

import (
	"context"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

// Scraper implements scraper.Scraper for University of Otago.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "otago",
		Name:        "University of Otago",
		Website:     "https://www.otago.ac.nz/news/events",
		Description: "New Zealand's first university, founded in 1869. Otago hosts distinguished lectures, public seminars, and community events across its Dunedin, Christchurch, and Wellington campuses.",
	}
}

func (s *Scraper) Scrape(_ context.Context) ([]model.Lecture, error) {
	// Otago events page is behind a Cloudflare JS challenge; returns no data.
	return nil, nil
}
