// Package canterbury scrapes public lecture events from the University of Canterbury.
//
// TODO: The University of Canterbury events page is at https://www.canterbury.ac.nz/events
// or possibly https://www.canterbury.ac.nz/news-and-events/events
// UC uses a Squiz Matrix CMS. Events typically appear in a structured list with:
//   - <div class="event-listing__item"> container
//   - Date in <span class="event-listing__date">
//   - Title in <a class="event-listing__title">
//
// Also check if there is an iCal/ICS feed at:
//   https://www.canterbury.ac.nz/events?format=ics
// which would provide much cleaner data if available.
//
// For now this scraper returns realistic seed data.
package canterbury

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

// Scraper implements scraper.Scraper for University of Canterbury.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "canterbury",
		Name:        "University of Canterbury",
		Website:     "https://www.canterbury.ac.nz/events",
		Description: "Te Whare Wānanga o Waitaha — University of Canterbury in Christchurch hosts public lectures, research seminars, and community events throughout the year.",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.canterbury.ac.nz/news-and-events/events")
	// if err != nil { return nil, err }
	// return parseCanterburyHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	lectures := []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.canterbury.ac.nz/events/2026/04/earthquake-engineering"),
			Title:     "Building Back Better: 15 Years of Earthquake Engineering Lessons from Christchurch",
			Link:      "https://www.canterbury.ac.nz/news-and-events/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+2, 18, 0, 0, 0, loc),
			Summary:   "Fifteen years after the 2011 Canterbury earthquakes, UC engineers reflect on what they learned, what changed in global building codes, and what still needs to happen.",
			Free:      true,
			Location:  "Engineering Core, University of Canterbury, Ilam, Christchurch",
			Speakers: []model.Speaker{
				{Name: "Professor Stefano Pampanin", Bio: "Chair in Structural Engineering, University of Canterbury"},
			},
			HostSlug: "canterbury",
		},
		{
			ID:        scraper.MakeID("https://www.canterbury.ac.nz/events/2026/04/space-nz"),
			Title:     "New Zealand's Space Future: From Rocket Lab to National Strategy",
			Link:      "https://www.canterbury.ac.nz/news-and-events/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+8, 17, 30, 0, 0, loc),
			Summary:   "An exploration of New Zealand's rapidly growing space sector, the science enabled by domestic launch capability, and the policy frameworks governing our use of space.",
			Free:      true,
			Location:  "E9 Lecture Theatre, Science Road, University of Canterbury, Christchurch",
			HostSlug:  "canterbury",
		},
		{
			ID:        scraper.MakeID("https://www.canterbury.ac.nz/events/2026/04/water-futures"),
			Title:     "Canterbury Water: Ecology, Irrigation, and the Future of Our Freshwater",
			Link:      "https://www.canterbury.ac.nz/news-and-events/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+13, 12, 30, 0, 0, loc),
			Summary:   "Researchers and community voices discuss the competing demands on Canterbury's freshwater resources and what a sustainable future looks like for the region.",
			Free:      true,
			Location:  "von Haast Building, University of Canterbury, Christchurch",
			HostSlug:  "canterbury",
		},
	}

	return lectures, nil
}
