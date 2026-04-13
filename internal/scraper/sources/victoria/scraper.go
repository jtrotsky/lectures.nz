// Package victoria scrapes public lecture events from Victoria University of Wellington
// (Te Herenga Waka).
//
// The events page at https://www.wgtn.ac.nz/events uses a Vue.js component that calls
// a Squiz Funnelback search API:
//
//	https://www.wgtn.ac.nz/utils/get-events?collection=vic-events-push&...
//
// We filter to upcoming events with General public audience, not purely online.
package victoria

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const eventsBaseURL = "https://www.wgtn.ac.nz/utils/get-events"
const baseURL = "https://www.wgtn.ac.nz"

// Scraper implements scraper.Scraper for Victoria University of Wellington.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "victoria",
		Name:        "Victoria University of Wellington",
		Website:     "https://www.wgtn.ac.nz/events",
		Description: "Te Herenga Waka — Victoria University of Wellington hosts regular public lectures, seminars, and events across its faculties and research institutes.",
		Bluesky:     "@victoriauniversity.bsky.social",
	}
}

// vuwResponse mirrors the relevant parts of the Funnelback JSON response.
type vuwResponse struct {
	Response struct {
		ResultPacket struct {
			Results []struct {
				Title   string `json:"title"`
				LiveUrl string `json:"liveUrl"`
				MetaData struct {
					T              string `json:"t"`
					StartDate      string `json:"startDate"`
					EndDate        string `json:"endDate"`
					EventStartTime string `json:"eventStartTime"`
					EventSummary   string `json:"eventSummary"`
					EventLocation  string `json:"eventLocation"`
					EventAudience  string `json:"eventAudience"`
					EventsTagOnline  string `json:"eventsTagOnline"`
					EventCategory    string `json:"eventCategory"`
					Cost             string `json:"cost"`
					URL              string `json:"url"`
				} `json:"metaData"`
			} `json:"results"`
		} `json:"resultPacket"`
	} `json:"response"`
}

// parseTime12h parses a 12-hour time string like "10:00am", "12:30pm", "5:00pm".
func parseTime12h(s string) (hour, min int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, 0, false
	}
	isPM := strings.HasSuffix(s, "pm")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "pm"), "am")
	parts := strings.SplitN(s, ":", 2)
	if len(parts) < 1 {
		return 0, 0, false
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	mn := 0
	if len(parts) == 2 {
		mn, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	if isPM && h != 12 {
		h += 12
	} else if !isPM && h == 12 {
		h = 0
	}
	return h, mn, true
}

// adminKeywords identifies administrative/calendar events to exclude.
var adminKeywords = []string{
	"trimester", "resumes after", "semester", "public holiday", "university closed",
	"council meeting", "committee meeting", "halls of residence", "aegrotat",
	"withdrawal from", "graduation", "orientation",
	"campus tour", "alumni get-together", "alumni and friends", "alumni function",
	"open day", "application round", "enrolment opens", "applications close",
	"deadline to submit", "admission application", "graduate awards",
	"programme ends", "program ends",
	// Fitness/wellness activities (not lectures)
	"baduanjin",
	// Language competitions (not public talks)
	"bridge competition",
	// Language-practice social meetups (not public lectures)
	"mandarin corner",
}

// isAdminEvent returns true for administrative/calendar events that aren't public talks.
func isAdminEvent(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range adminKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// isOverseas returns true for event locations that are clearly outside New Zealand.
var overseasIndicators = []string{
	"hotel", "sukhumvit", "bangkok", "hanoi", "singapore", "sydney",
	"hong kong", "london", "new york", "tokyo", "saigon", "ho chi minh",
	"kuala lumpur", "jakarta", "taipei", "seoul", "beijing", "shanghai",
}

func isOverseas(location string) bool {
	loc := strings.ToLower(location)
	for _, indicator := range overseasIndicators {
		if strings.Contains(loc, indicator) {
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
	// Format: DMMMYYYY e.g. "8Apr2026"
	fromDate := fmt.Sprintf("%d%s%d", now.Day(), now.Format("Jan"), now.Year())

	apiURL := fmt.Sprintf(
		"%s?collection=vic-events-push&profile=_default_preview&fmo=true&query=!showall"+
			"&meta_isApproved=yes"+
			"&meta_eventsTagCategory_not_phrase=Important%%20university%%20dates"+
			"&sort=adate&num_ranks=100&start_rank=1&meta_d3=%s",
		eventsBaseURL, fromDate,
	)

	body, err := scraper.Fetch(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("victoria: fetch: %w", err)
	}

	var resp vuwResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("victoria: unmarshal: %w", err)
	}

	var lectures []model.Lecture
	seen := map[string]bool{}

	for _, r := range resp.Response.ResultPacket.Results {
		md := r.MetaData
		// Only include events open to the general public.
		if md.EventAudience != "" && !strings.Contains(md.EventAudience, "General public") {
			continue
		}
		// Skip purely online events.
		if md.EventsTagOnline == "online" {
			continue
		}
		// Skip overseas events (alumni functions in Asia etc.).
		loc := md.EventLocation
		if isOverseas(loc) {
			continue
		}
		// Exclude administrative/calendar events.
		if isAdminEvent(md.T) {
			continue
		}

		startStr := md.StartDate
		if len(startStr) != 8 {
			continue
		}
		year, _ := strconv.Atoi(startStr[:4])
		month, _ := strconv.Atoi(startStr[4:6])
		day, _ := strconv.Atoi(startStr[6:])

		h, mn, ok := parseTime12h(md.EventStartTime)
		if !ok {
			h, mn = 12, 0
		}
		t := time.Date(year, time.Month(month), day, h, mn, 0, 0, nzLoc)

		eventURL := md.URL
		if eventURL == "" {
			eventURL = r.LiveUrl
		}
		if eventURL == "" {
			continue
		}
		// The Funnelback API sometimes returns percent-encoded URLs — decode them.
		if decoded, err := url.QueryUnescape(eventURL); err == nil {
			eventURL = decoded
		}
		if !strings.HasPrefix(eventURL, "http") {
			eventURL = baseURL + eventURL
		}

		title := md.T
		if title == "" {
			title = r.Title
		}

		// Deduplicate by title+date (same event may appear under multiple faculty URLs).
		dedupeKey := strings.ToLower(title) + "|" + startStr
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true

		location := md.EventLocation
		if location == "" {
			location = "Wellington"
		}

		free := md.Cost == "free"

		// The Funnelback API returns a short eventSummary. Fetch the detail
		// page when that's thin so enrichment has enough text to classify
		// correctly (e.g. social meetup vs lecture).
		desc := md.EventSummary
		if len(strings.TrimSpace(desc)) < 120 {
			if detail, err := scraper.FetchDetail(ctx, eventURL); err == nil && detail != "" {
				desc = detail
			}
		}

		lectures = append(lectures, model.Lecture{
			ID:          scraper.MakeID(eventURL),
			Title:       scraper.CleanTitle(title),
			Link:        eventURL,
			TimeStart:   t,
			Description: desc,
			Summary:     scraper.TruncateSummary(desc, 200),
			Location:    location,
			Free:        free,
			HostSlug:    "victoria",
		})
	}

	return lectures, nil
}
