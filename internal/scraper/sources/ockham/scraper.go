// Package ockham scrapes the Ockham Lecture series from Objectspace gallery.
//
// The Ockham Lecture series is hosted at Objectspace (objectspace.org.nz) and
// is sponsored by Ockham Residential. Events are listed on the Objectspace
// events page alongside other gallery events. This scraper filters to events
// whose title begins with "Ockham Lecture" and registers them under the ockham
// host so the series appears as its own entry in the site.
//
// All other Objectspace events are handled by the objectspace scraper.
package ockham

import (
	"context"
	"strings"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/objectspace"
)

// Scraper implements scraper.Scraper for the Ockham Lecture series.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "ockham",
		Name:        "Ockham Lecture Series",
		Website:     "https://objectspace.org.nz/events",
		Description: "The Ockham Lecture series is an annual programme of lectures and panel discussions at Objectspace Auckland, supported by Ockham Residential.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	all, err := objectspace.ScrapeAll(ctx)
	if err != nil {
		return nil, err
	}

	var lectures []model.Lecture
	for _, l := range all {
		if strings.HasPrefix(strings.ToLower(l.Title), "ockham lecture") {
			l.HostSlug = "ockham"
			lectures = append(lectures, l)
		}
	}
	return lectures, nil
}
