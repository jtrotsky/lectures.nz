// Package massey scrapes public events from Massey University.
//
// The events page at https://www.massey.ac.nz/about/events/ is client-side
// rendered but backed by an Elasticsearch endpoint:
//
//	POST https://www.massey.ac.nz/search/_api/events/_search
//
// We fetch all upcoming events (event_end >= now), then filter in Go to those
// where event_audience contains "public", excluding enrolment/open day events.
// Online events are included — Massey's "This Thinking Life" webinar series
// has genuine lecture content.
//
// Field notes from the index:
//   - event_start / event_end: RFC3339 strings (e.g. "2026-06-17T06:00:00+00:00")
//   - event_audience / event_category: arrays of strings
//   - location_display_value: array, first element is the venue string
package massey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const searchURL = "https://www.massey.ac.nz/search/_api/events/_search"

// Scraper implements scraper.Scraper for Massey University.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "massey",
		Name:        "Massey University",
		Website:     "https://www.massey.ac.nz/about/events/",
		Description: "Massey University hosts public lectures, seminars, and events across its Auckland, Palmerston North, and Wellington campuses.",
	}
}

// esResponse mirrors the relevant parts of the Elasticsearch response.
// Array-typed fields may arrive as a plain string or a JSON array.
type esResponse struct {
	Hits struct {
		Hits []struct {
			Source struct {
				Title                string          `json:"title"`
				URL                  string          `json:"url"`
				EventStart           string          `json:"event_start"`
				EventEnd             string          `json:"event_end"`
				EventStatus          json.RawMessage `json:"event_status"`
				EventCategory        json.RawMessage `json:"event_category"`
				EventAudience        json.RawMessage `json:"event_audience"`
				LocationDisplayValue json.RawMessage `json:"location_display_value"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// toStringSlice converts a JSON field that may be a string or []string.
func toStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var arr []string
	json.Unmarshal(raw, &arr) //nolint:errcheck
	return arr
}

// excludeKeywords identifies promotional/admin events that aren't public talks.
var excludeKeywords = []string{
	"information evening",
	"open day",
	"explore massey",
	"webinar: explore",
	"study options",
}

func isExcluded(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range excludeKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	now := time.Now().In(nzLoc)
	nowMS := now.UnixMilli()

	queryBody := fmt.Sprintf(`{
		"size": 100,
		"from": 0,
		"_source": {
			"includes": ["title","url","event_start","event_end","event_status",
			             "event_category","event_audience","location_display_value"]
		},
		"sort": {"event_start": "asc"},
		"query": {
			"bool": {
				"filter": [
					{"bool": {"filter": [{"range": {"event_end": {"gte": %d}}}]}}
				]
			}
		}
	}`, nowMS)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewBufferString(queryBody))
	if err != nil {
		return nil, fmt.Errorf("massey: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.massey.ac.nz")
	req.Header.Set("Referer", "https://www.massey.ac.nz/about/events/")
	req.Header.Set("User-Agent", scraper.UserAgent)
	req.Header.Set("x-elastic-client-meta", "ent=1.24.2-es-connector,js=browser,t=1.24.2-es-connector,ft=universal")

	resp, err := scraper.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("massey: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("massey: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("massey: read body: %w", err)
	}

	var esResp esResponse
	if err := json.Unmarshal(raw, &esResp); err != nil {
		return nil, fmt.Errorf("massey: unmarshal: %w", err)
	}

	var lectures []model.Lecture
	for _, hit := range esResp.Hits.Hits {
		src := hit.Source

		status := toStringSlice(src.EventStatus)
		audience := toStringSlice(src.EventAudience)
		category := toStringSlice(src.EventCategory)
		locationVals := toStringSlice(src.LocationDisplayValue)

		// Skip cancelled events.
		if containsString(status, "cancelled") {
			continue
		}

		// Only public-audience events.
		if !containsString(audience, "public") {
			continue
		}

		if src.URL == "" || src.EventStart == "" {
			continue
		}

		startTime, err := time.Parse(time.RFC3339, src.EventStart)
		if err != nil {
			continue
		}
		startTime = startTime.In(nzLoc)

		// Belt-and-suspenders: skip past events (the ES range is on event_end,
		// so multi-day recurring events may still appear).
		if startTime.Before(now) {
			continue
		}

		if isExcluded(src.Title) {
			continue
		}

		eventURL := src.URL
		if !strings.HasPrefix(eventURL, "http") {
			eventURL = "https://www.massey.ac.nz" + eventURL
		}

		location := ""
		if len(locationVals) > 0 {
			location = locationVals[0]
		}
		if location == "" {
			switch {
			case containsString(category, "auckland"):
				location = "Massey University Albany Campus, Auckland"
			case containsString(category, "wellington"):
				location = "Massey University Wellington Campus"
			case containsString(category, "online"):
				location = "Online"
			default:
				location = "Massey University, Palmerston North"
			}
		}

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(eventURL),
			Title:     scraper.CleanTitle(src.Title),
			Link:      eventURL,
			TimeStart: startTime,
			Location:  location,
			HostSlug:  "massey",
		})
	}

	return lectures, nil
}
