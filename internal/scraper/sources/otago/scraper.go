// Package otago scrapes public lecture events from the University of Otago.
//
// TODO: The University of Otago events page is at https://www.otago.ac.nz/news/events
// Otago uses a custom CMS. Events are likely listed with:
//   - <div class="event"> or similar container elements
//   - Date in a <time> element or a span with class "date"
//   - Title in an <h2> or <h3>
//
// Also check: https://www.otago.ac.nz/news/events/otago-public-lectures
// which may specifically filter to public-facing lectures.
//
// Otago has a long tradition of Distinguished Lectures — these would be high value to include.
//
// For now this scraper returns realistic seed data.
package otago

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
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

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping.
	// body, err := scraper.Fetch(ctx, "https://www.otago.ac.nz/news/events/otago-public-lectures")
	// if err != nil { return nil, err }
	// return parseOtagoHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	lectures := []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.otago.ac.nz/events/2026/04/distinguished-lecture-brain"),
			Title:     "Distinguished Lecture: The Brain in Pain — New Frontiers in Chronic Pain Research",
			Link:      "https://www.otago.ac.nz/news/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+6, 17, 0, 0, 0, loc),
			Summary:   "The annual Otago Distinguished Lecture in Medicine explores breakthrough research into the neuroscience of chronic pain and emerging treatments.",
			Free:      true,
			Location:  "Dunedin Public Hospital, Great King Street, Dunedin",
			Speakers: []model.Speaker{
				{Name: "Professor Rae Frances Irvine", Bio: "Chair of Pain Medicine, University of Otago"},
			},
			HostSlug: "otago",
		},
		{
			ID:        scraper.MakeID("https://www.otago.ac.nz/events/2026/04/southern-ocean-lecture"),
			Title:     "Secrets of the Southern Ocean: Otago's Deep-Sea Research Programme",
			Link:      "https://www.otago.ac.nz/news/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+11, 18, 30, 0, 0, loc),
			Summary:   "Marine scientists share discoveries from recent voyages to the Southern Ocean, including new species, ecosystem dynamics, and implications for fisheries management.",
			Free:      true,
			Location:  "St David Lecture Theatre Complex, University of Otago, Dunedin",
			HostSlug:  "otago",
		},
		{
			ID:        scraper.MakeID("https://www.otago.ac.nz/events/2026/04/maori-health"),
			Title:     "Equity in Health: Progress and Challenges for Māori Wellbeing",
			Link:      "https://www.otago.ac.nz/news/events",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+16, 17, 0, 0, 0, loc),
			Summary:   "A wānanga-style public discussion on the persistent health inequities facing Māori communities and what genuine system change would require.",
			Free:      true,
			Location:  "Māori Centre, University of Otago, Dunedin",
			Speakers: []model.Speaker{
				{Name: "Professor Papaarangi Reid", Bio: "Dean of Te Ara Hauora, Faculty of Medical and Health Sciences"},
			},
			HostSlug: "otago",
		},
	}

	return lectures, nil
}
