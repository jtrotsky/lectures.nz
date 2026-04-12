// Package rbnz scrapes upcoming speech events from the Reserve Bank of New Zealand.
//
// RBNZ pre-announces speeches under https://www.rbnz.govt.nz/news-and-events/speeches
// a day or two before delivery. Each speech is published as an event page at:
//
//	https://www.rbnz.govt.nz/news-and-events/events/{year}/{month}/{slug}
//
// The listing is powered by Coveo search. Speeches, external events, and post-MPS
// engagements are returned by querying with the template and tag IDs captured from
// the browser.
//
// # Token
//
// The Coveo API requires a signed Bearer JWT that expires after 1 hour. The RBNZ
// website (which issues fresh tokens) is behind Cloudflare and not directly accessible
// from a server-side scraper. To use this scraper:
//
//  1. Open https://www.rbnz.govt.nz/news-and-events/speeches in Chrome/Firefox
//  2. Open DevTools → Network tab → filter by "search/v2"
//  3. Copy the Authorization header value (starts with "eyJ...")
//  4. Set environment variable: RBNZ_COVEO_TOKEN=<value>
//
// If the variable is absent or the token is expired, the scraper returns zero events
// without error (graceful degradation).
//
// # Coveo query parameters
//
// Endpoint: https://reservebanknewzealandproductionu8tvivj5.org.coveo.com/rest/search/v2
// Organization ID: reservebanknewzealandproductionu8tvivj5
// Search hub: RBNZ_rbnz-104-prod_web
// Tab: Speeches listing
//
// The aq filter selects Sitecore template GUIDs for event pages and tag IDs for the
// Speeches content category — both are stable and do not rotate with the token.
package rbnz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
)

const (
	coveoBase   = "https://reservebanknewzealandproductionu8tvivj5.org.coveo.com/rest/search/v2"
	coveoOrgID  = "reservebanknewzealandproductionu8tvivj5"
	coveoHub    = "RBNZ_rbnz-104-prod_web"
	coveoTab    = "Speeches listing"
	rbnzBaseURL = "https://www.rbnz.govt.nz"
)

// coveoAQ selects speech/event pages by Sitecore template GUIDs and Speeches tag IDs.
// These are stable values that do not change when the token rotates.
const coveoAQ = "(@z95xtemplate= '32e5dae82c0f4e2c8ddb372c53b56d8f'" +
	" OR @z95xtemplate= 'a55855360fb543a88d8ea7ab98c93590'" +
	" OR @z95xtemplate= '4f8e1dcf41b34c9b9e8a979fbde64385')" +
	" & (@computedsz120xaalltagids='964ef6f4095b4673b3b871cec506f914'" +
	" OR @computedsz120xaalltagids='bc89f2c12f5640eaaaea1d0546a22675'" +
	" OR @computedsz120xaalltagids='f6bc57eb910b4708a4ea2c52df5176d4')"

// Scraper implements scraper.Scraper for RBNZ speeches/events.
type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "rbnz",
		Name:        "Reserve Bank of New Zealand",
		Website:     "https://www.rbnz.govt.nz/news-and-events/speeches",
		Description: "The Reserve Bank of New Zealand hosts public speeches, economic seminars, and engagement events delivered by the Governor and senior officials.",
	}
}

// coveoResult mirrors the fields we need from each Coveo search result.
type coveoResult struct {
	Title    string `json:"title"`
	ClickURI string `json:"clickUri"`
	Excerpt  string `json:"excerpt"`
	Raw      struct {
		ComputedTitle    string  `json:"computedtitle"`
		EventStart       float64 `json:"eventstart"` // Unix ms as number
		ContentTypeNames []string `json:"computedz95xsz120xacontenttypetagnames"`
	} `json:"raw"`
}

type coveoResponse struct {
	TotalCount int           `json:"totalCount"`
	Results    []coveoResult `json:"results"`
}

// excludeContentTypes are RBNZ content categories that aren't public talks.
var excludeContentTypes = map[string]bool{
	"Finance & Expenditure Committee hearing": true,
	"Post-MPS engagement":                    true,
	"Media conference":                       true,
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	token := os.Getenv("RBNZ_COVEO_TOKEN")
	if token == "" {
		return nil, nil // no token, skip gracefully
	}

	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	nowMS := float64(time.Now().UnixMilli())

	// Only fetch upcoming speeches (eventstart >= now).
	aq := fmt.Sprintf("%s & @eventstart>=%d", coveoAQ, int64(nowMS))

	params := url.Values{}
	params.Set("searchHub", coveoHub)
	params.Set("tab", coveoTab)
	params.Set("locale", "en")
	params.Set("firstResult", "0")
	params.Set("numberOfResults", "50")
	params.Set("sortCriteria", "@eventstart ascending")
	params.Set("aq", aq)
	params.Set("fieldsToInclude", `["computedtitle","eventstart","computedz95xsz120xacontenttypetagnames","summary"]`)
	params.Set("allowQueriesWithoutKeywords", "true")

	endpoint := coveoBase + "?organizationId=" + coveoOrgID
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("rbnz: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", rbnzBaseURL)
	req.Header.Set("Referer", rbnzBaseURL+"/")
	req.Header.Set("User-Agent", scraper.UserAgent)

	resp, err := scraper.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rbnz: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		// Token expired or invalid — log and skip gracefully.
		return nil, fmt.Errorf("rbnz: token expired or invalid (HTTP %d); refresh RBNZ_COVEO_TOKEN", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rbnz: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rbnz: read body: %w", err)
	}

	var coveoResp coveoResponse
	if err := json.Unmarshal(raw, &coveoResp); err != nil {
		return nil, fmt.Errorf("rbnz: unmarshal: %w", err)
	}

	var lectures []model.Lecture
	for _, r := range coveoResp.Results {
		// Skip content types that aren't public events.
		skip := false
		for _, ct := range r.Raw.ContentTypeNames {
			if excludeContentTypes[ct] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		if r.ClickURI == "" || r.Raw.EventStart == 0 {
			continue
		}

		eventURL := r.ClickURI
		if !strings.HasPrefix(eventURL, "http") {
			eventURL = rbnzBaseURL + eventURL
		}

		startTime := time.UnixMilli(int64(r.Raw.EventStart)).In(nzLoc)

		title := r.Raw.ComputedTitle
		if title == "" {
			// Strip the " - Reserve Bank of New Zealand - Te Pūtea Matua" suffix.
			title = strings.TrimSuffix(r.Title, " - Reserve Bank of New Zealand - Te Pūtea Matua")
		}

		summary := scraper.TruncateSummary(r.Excerpt, 200)

		lectures = append(lectures, model.Lecture{
			ID:        scraper.MakeID(eventURL),
			Title:     scraper.CleanTitle(title),
			Link:      eventURL,
			TimeStart: startTime,
			Summary:   summary,
			Location:  "Wellington",
			Free:      true,
			HostSlug:  "rbnz",
		})
	}

	return lectures, nil
}
