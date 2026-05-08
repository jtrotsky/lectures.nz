// Package canterbury scrapes public lecture events from the University of Canterbury.
//
// UC's events page (https://www.canterbury.ac.nz/news-and-events/events) uses an
// Adobe Experience Manager (AEM) CMS that renders all event listings client-side via
// JavaScript. The static HTML response contains no event data — only navigation and
// a hero section. No accessible JSON API or ICS feed was found.
//
// Returns 0 events until a scraping approach is found.
package canterbury

import (
	"context"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

// Scraper implements scraper.Scraper for University of Canterbury.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "canterbury",
		Name:        "University of Canterbury",
		Website:     "https://www.canterbury.ac.nz/news-and-events/events",
		Description: "Te Whare Wānanga o Waitaha — University of Canterbury in Christchurch hosts public lectures, research seminars, and community events throughout the year.",
	}
}

func (s *Scraper) Scrape(_ context.Context) ([]model.Lecture, error) {
	// UC events page is fully JS-rendered (AEM CMS); no accessible feed found.
	return nil, nil
}
