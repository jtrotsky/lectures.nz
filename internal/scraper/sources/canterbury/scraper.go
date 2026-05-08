// Package canterbury scrapes public events from the University of Canterbury.
//
// UC's events listing page is fully JS-rendered (AEM CMS), but the page embeds a
// Funnelback search component whose collection name is in a data attribute:
//
//	data-searchCollection="uoc2~sp-aem-events"
//
// Querying the Funnelback JSON endpoint returns structured event data including
// ucEventDate, ucEventTime, ucEventLocation, ucEventPrice, and ucCategory.
//
// Filtered out:
//   - "Hui Tairanga" enrolment info evenings (not public lectures)
//   - Events with ucCategory = "Creative arts" (concerts, music recitals)
//   - Careers fairs
//
// Enrichment handles further classification.
package canterbury

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	searchURL = "https://search.canterbury.ac.nz/s/search.json" +
		"?collection=uoc2~sp-aem-events&query=!null&num_ranks=50&sort=date&profile=events-results-page"
	baseURL = "https://www.canterbury.ac.nz"
)

// Scraper implements scraper.Scraper for University of Canterbury.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "canterbury",
		Name:        "University of Canterbury",
		City:        "Christchurch",
		Website:     "https://www.canterbury.ac.nz/news-and-events/events",
		Description: "Te Whare Wānanga o Waitaha — University of Canterbury in Christchurch hosts public lectures, research seminars, and community events throughout the year.",
	}
}

// funnelbackResponse is the top-level response from the Funnelback search API.
type funnelbackResponse struct {
	Response struct {
		ResultPacket struct {
			Results []funnelbackResult `json:"results"`
		} `json:"resultPacket"`
	} `json:"response"`
}

type funnelbackResult struct {
	Title        string              `json:"title"`
	LiveURL      string              `json:"liveUrl"`
	Summary      string              `json:"summary"`
	ListMetadata map[string][]string `json:"listMetadata"`
}

// exclude patterns for event titles — info evenings, careers events, etc.
var excludeTitlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bHui Tairanga\b`),
	regexp.MustCompile(`(?i)\bInfo Evening\b`),
	regexp.MustCompile(`(?i)\bCareers Fair\b`),
	regexp.MustCompile(`(?i)\bOpen Day\b`),
	regexp.MustCompile(`(?i)\bOrientation\b`),
}

var (
	// dateParsRe matches "Thursday 18 June 2026"
	dateParsRe = regexp.MustCompile(`(?i)(\d{1,2})\s+(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{4})`)
	// timeParsRe matches "5:30PM" or "10:00AM"
	timeParsRe = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*(AM|PM)`)
)

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("canterbury: fetch funnelback: %w", err)
	}

	var resp funnelbackResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("canterbury: parse funnelback: %w", err)
	}

	loc := scraper.NZLocation
	now := time.Now().In(loc)
	seen := map[string]bool{}
	var lectures []model.Lecture

	for _, r := range resp.Response.ResultPacket.Results {
		title := scraper.CleanTitle(stripSuffix(r.Title, " | UC"))
		if title == "" || seen[r.LiveURL] {
			continue
		}

		// Skip excluded event types by title pattern.
		if matchesAny(title, excludeTitlePatterns) {
			continue
		}

		md := r.ListMetadata

		// Skip creative arts events (concerts, recitals).
		if hasValue(md["ucCategory"], "Creative arts") {
			continue
		}

		dateStr := first(md["ucEventDate"])
		if dateStr == "" {
			continue
		}
		t, ok := parseUCDate(dateStr, first(md["ucEventTime"]), loc)
		if !ok || t.Before(now) {
			continue
		}

		seen[r.LiveURL] = true

		location := first(md["ucEventLocation"])
		location = strings.TrimSpace(location)
		if location != "" && !strings.Contains(strings.ToLower(location), "christchurch") {
			location += ", Christchurch"
		}

		free := strings.EqualFold(first(md["ucEventPrice"]), "free")
		cost := ""
		if !free {
			cost = first(md["ucEventPrice"])
		}

		// Don't use Funnelback's summary — it contains crawled navigation HTML garbage.
		// Leave Description empty so fillDescriptions fetches the real event page.

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(r.LiveURL),
			Title:     title,
			Link:      r.LiveURL,
			TimeStart: t,
			Location:  location,
			Free:        free,
			Cost:        cost,
			HostSlug:    "canterbury",
		})
	}

	return lectures, nil
}

func parseUCDate(dateStr, timeStr string, loc *time.Location) (time.Time, bool) {
	dm := dateParsRe.FindStringSubmatch(dateStr)
	if dm == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(dm[1])
	month, ok := scraper.AbbrevMonthMap[strings.ToLower(dm[2][:3])]
	if !ok {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(dm[3])

	hour, min := 18, 0 // default 6pm
	if tm := timeParsRe.FindStringSubmatch(timeStr); tm != nil {
		h, _ := strconv.Atoi(tm[1])
		m, _ := strconv.Atoi(tm[2])
		period := strings.ToUpper(tm[3])
		if period == "PM" && h != 12 {
			h += 12
		} else if period == "AM" && h == 12 {
			h = 0
		}
		hour, min = h, m
	}

	return time.Date(year, month, day, hour, min, 0, 0, loc), true
}

func stripSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func first(ss []string) string {
	if len(ss) > 0 {
		return ss[0]
	}
	return ""
}

func hasValue(ss []string, v string) bool {
	for _, s := range ss {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}
