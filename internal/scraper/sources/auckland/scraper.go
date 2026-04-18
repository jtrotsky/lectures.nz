// Package auckland scrapes public lecture events from the University of Auckland.
//
// The University of Auckland runs an events portal at https://unievents.auckland.ac.nz/
// which is an Ionic SPA, but it calls a public API at:
//
//	https://apis.auckland.ac.nz/events-portal-access/v1/events
//
// The API returns all events (168+) including music workshops, campus tours, etc.
// We filter to lectures, talks, seminars, and similar by keyword and category.
package auckland

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const apiURL = "https://apis.auckland.ac.nz/events-portal-access/v1/events"

// Scraper implements scraper.Scraper for University of Auckland.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "auckland",
		Name:        "University of Auckland",
		Website:     "https://unievents.auckland.ac.nz/",
		Description: "New Zealand's leading research-intensive university, offering a wide range of public lectures, seminars, and community events.",
	}
}

type apiEvent struct {
	EventID          string  `json:"eventId"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	StartDateTime    string  `json:"startDateTime"`
	EndDateTime      string  `json:"endDateTime"`
	Summary          string  `json:"summary"`
	OrganisationName string  `json:"organisationName"`
	CategoryName     string  `json:"categoryName"`
	SubcategoryName  string  `json:"subcategoryName"`
	IsFree           bool    `json:"isFree"`
	LowestPrice      float64 `json:"lowestPrice"`
	HighestPrice     float64 `json:"highestPrice"`
	Location         struct {
		Name        string `json:"name"`
		Address1    string `json:"address1"`
		Address2    string `json:"address2"`
		City        string `json:"city"`
		DisplayName string `json:"displayName"`
	} `json:"location"`
}

// lectureKeywords identifies events worth including.
var lectureKeywords = []string{
	"lecture", "inaugural lecture", "professorial lecture",
	"seminar", "symposium", "colloquium",
	"talk", "keynote", "panel",
	"forum", "workshop", "discussion",
	"curator talk", "public talk",
}

// excludeCategories are broad categories we skip entirely.
var excludeCategories = map[string]bool{
	"Music": true,
}

func isLectureEvent(e apiEvent) bool {
	if excludeCategories[e.CategoryName] {
		return false
	}
	// Exclude purely online events.
	if e.Location.Name == "Online" || (e.Location.City == "" && e.Location.Address1 == "") {
		return false
	}
	// Must be in Auckland.
	if e.Location.City != "" && e.Location.City != "Auckland" {
		return false
	}
	text := strings.ToLower(e.Name + " " + e.Summary + " " + e.SubcategoryName)
	for _, kw := range lectureKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func buildLocation(e apiEvent) string {
	loc := e.Location
	if loc.Name != "" && loc.Address1 != "" {
		return loc.Name + ", " + loc.Address1 + ", Auckland"
	}
	if loc.DisplayName != "" {
		return loc.DisplayName
	}
	if loc.Name != "" {
		return loc.Name + ", Auckland"
	}
	return "Auckland"
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("auckland: fetch: %w", err)
	}

	var events []apiEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("auckland: unmarshal: %w", err)
	}

	nzLoc := scraper.NZLocation

	var lectures []model.Lecture
	for _, e := range events {
		if !isLectureEvent(e) {
			continue
		}
		if e.StartDateTime == "" {
			continue
		}
		// API returns local NZ time without timezone, e.g. "2026-05-07T10:30:00"
		t, err := time.ParseInLocation("2006-01-02T15:04:05", e.StartDateTime, nzLoc)
		if err != nil {
			continue
		}

		cost := ""
		free := e.IsFree || e.LowestPrice == 0
		if !free && e.LowestPrice > 0 {
			cost = fmt.Sprintf("$%.0f", e.LowestPrice)
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(e.URL),
			Title:       e.Name,
			Link:        e.URL,
			TimeStart:   t,
			Description: e.Summary,
			Summary:     scraper.TruncateSummary(e.Summary, 200),
			Location:    buildLocation(e),
			Free:        free,
			Cost:        cost,
			HostSlug:    "auckland",
		})
	}

	return lectures, nil
}
