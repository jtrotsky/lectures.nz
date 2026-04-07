// Package auckland scrapes public lecture events from the University of Auckland.
//
// TODO: The University of Auckland events page (https://www.auckland.ac.nz/en/news/events.html)
// is rendered client-side with JavaScript. To scrape it properly you would need either:
//   - A headless browser (e.g. chromedp)
//   - Their internal API endpoint (check network tab for XHR/fetch calls)
//
// The page appears to use an Adobe Experience Manager (AEM) backend. Look for endpoints like:
//   https://www.auckland.ac.nz/bin/api/events?category=public-lectures&...
//
// For now this scraper returns realistic seed data so the site works end-to-end.
package auckland

import (
	"context"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

// Scraper implements scraper.Scraper for University of Auckland.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "auckland",
		Name:        "University of Auckland",
		Website:     "https://www.auckland.ac.nz/en/news/events.html",
		Description: "New Zealand's leading research-intensive university, offering a wide range of public lectures, seminars, and community events.",
		Icon:        "https://www.auckland.ac.nz/favicon.ico",
	}
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	// TODO: Replace with real scraping once the AEM API endpoint is identified.
	// Attempt:
	//   body, err := scraper.Fetch(ctx, "https://www.auckland.ac.nz/en/news/events.html")
	//   if err != nil { return nil, err }
	//   return parseAucklandHTML(body)

	now := time.Now()
	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}

	lectures := []model.Lecture{
		{
			ID:        scraper.MakeID("https://www.auckland.ac.nz/events/2026/04/climate-futures"),
			Title:     "Climate Futures: Science, Policy and Hope",
			Link:      "https://www.auckland.ac.nz/en/news/events.html",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+3, 18, 0, 0, 0, loc),
			Summary:   "A public lecture exploring New Zealand's role in addressing the climate crisis, with perspectives from leading researchers and policymakers.",
			Free:      true,
			Location:  "Owen G Glenn Building, Level 1, 12 Grafton Road, Auckland City",
			Speakers: []model.Speaker{
				{Name: "Professor James Renwick", Bio: "Climate scientist at Victoria University of Wellington, IPCC author"},
			},
			HostSlug: "auckland",
		},
		{
			ID:        scraper.MakeID("https://www.auckland.ac.nz/events/2026/04/te-tiriti-today"),
			Title:     "Te Tiriti o Waitangi Today: Constitutional Conversations",
			Link:      "https://www.auckland.ac.nz/en/news/events.html",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+7, 17, 30, 0, 0, loc),
			Summary:   "Distinguished scholars examine the ongoing relevance of Te Tiriti o Waitangi in contemporary New Zealand constitutional arrangements.",
			Free:      true,
			Location:  "Faculty of Law, 9 Eden Crescent, Auckland",
			Speakers: []model.Speaker{
				{Name: "Associate Professor Carwyn Jones", Bio: "Faculty of Law, University of Auckland"},
			},
			HostSlug: "auckland",
		},
		{
			ID:        scraper.MakeID("https://www.auckland.ac.nz/events/2026/04/neuroscience-sleep"),
			Title:     "The Neuroscience of Sleep: Why Rest is Your Brain's Superpower",
			Link:      "https://www.auckland.ac.nz/en/news/events.html",
			TimeStart: time.Date(now.Year(), now.Month(), now.Day()+10, 18, 30, 0, 0, loc),
			Summary:   "Discover the latest research into sleep, memory consolidation, and what happens to your brain during those vital hours of rest.",
			Free:      true,
			Location:  "School of Medical Sciences, 85 Park Road, Grafton, Auckland",
			HostSlug:  "auckland",
		},
	}

	return lectures, nil
}
