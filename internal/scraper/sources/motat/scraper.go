// Package motat scrapes public events from MOTAT (Museum of Transport and Technology), Auckland.
//
// MOTAT uses Gatsby with Contentful CMS. Gatsby pre-builds a static JSON file for
// each page at /page-data/{path}/page-data.json that contains the full GraphQL
// query result. We fetch:
//
//	https://motat.nz/page-data/events/page-data.json
//
// and walk the result tree to find the array of event nodes, each of which has:
// title, slug, startDate (ISO 8601), leadingParagraph, eventType.
package motat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	pageDataURL = "https://motat.nz/page-data/events/page-data.json"
	baseURL     = "https://motat.nz"
	location    = "MOTAT, 805 Great North Road, Western Springs, Auckland"
)

// Scraper implements scraper.Scraper for MOTAT.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "motat",
		Name:        "MOTAT",
		Website:     baseURL + "/events/",
		Description: "The Museum of Transport and Technology hosts public lectures, panel discussions, science programmes, and special events celebrating innovation and technology.",
	}
}

// pageData mirrors the Gatsby page-data.json structure for the events page.
type pageData struct {
	Result struct {
		Data struct {
			Events struct {
				Edges []struct {
					Node eventNode `json:"node"`
				} `json:"edges"`
			} `json:"events"`
		} `json:"data"`
	} `json:"result"`
}

// eventNode holds the Contentful event fields we care about.
type eventNode struct {
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	StartDate  string `json:"startDate"`
	EndDate    string `json:"endDate"`
	CustomDate string `json:"customDateText"`
	// leadingParagraph is a Contentful rich text field rendered via childMarkdownRemark.
	LeadingPara *struct {
		ChildMarkdown *struct {
			RawMarkdownBody string `json:"rawMarkdownBody"`
		} `json:"childMarkdownRemark"`
	} `json:"leadingParagraph"`
	EventType *struct {
		Title string `json:"title"`
	} `json:"eventType"`
}

// excludedTitles are MOTAT event series that are activity weekends, not lectures.
var excludedTitles = []string{
	"sports tech", // weekend activity series, not a lecture or seminar
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, pageDataURL)
	if err != nil {
		return nil, fmt.Errorf("motat: fetch page-data: %w", err)
	}

	var pd pageData
	if err := json.Unmarshal(body, &pd); err != nil {
		return nil, fmt.Errorf("motat: unmarshal page-data: %w", err)
	}

	edges := pd.Result.Data.Events.Edges
	if len(edges) == 0 {
		return nil, fmt.Errorf("motat: no events found in page-data.json")
	}

	loc, _ := time.LoadLocation("Pacific/Auckland")
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now()

	var lectures []model.Lecture
	for _, edge := range edges {
		node := edge.Node
		if node.Title == "" || node.Slug == "" || node.StartDate == "" {
			continue
		}

		// Skip known activity series that aren't lectures.
		titleLower := strings.ToLower(node.Title)
		excluded := false
		for _, kw := range excludedTitles {
			if strings.Contains(titleLower, kw) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// startDate is "2026-04-01T10:00" — local NZ time, no timezone suffix.
		t, err := time.ParseInLocation("2006-01-02T15:04", node.StartDate, loc)
		if err != nil {
			// Fallback: date-only
			t, err = time.ParseInLocation("2006-01-02", node.StartDate[:10], loc)
			if err != nil {
				continue
			}
		}

		if t.Before(now) {
			continue
		}

		link := baseURL + "/events/" + node.Slug + "/"

		summary := ""
		if node.LeadingPara != nil && node.LeadingPara.ChildMarkdown != nil {
			summary = strings.TrimSpace(node.LeadingPara.ChildMarkdown.RawMarkdownBody)
		}

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(link),
			Title:     node.Title,
			Link:      link,
			TimeStart: t,
			Description: summary,
			Summary:     scraper.TruncateSummary(summary, 200),
			Location:    location,
			HostSlug:    "motat",
		})
	}

	return lectures, nil
}
