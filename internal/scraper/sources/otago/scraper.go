// Package otago scrapes public lecture events from the University of Otago.
//
// Otago's events page (https://www.otago.ac.nz/news/events) is protected by
// Cloudflare's managed challenge — curl receives a JS challenge page, not event
// listings. No bypass was found without a headless browser.
//
// Returns 0 events until a scraping approach is found.
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
