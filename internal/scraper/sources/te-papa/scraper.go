// Package tepapa scrapes public events from Te Papa Tongarewa, the Museum of New Zealand.
//
// The site is a Next.js app that embeds all page data in a
// <script id="__NEXT_DATA__" type="application/json"> tag, so no JS execution
// is required.  Events live at:
//
//	props.pageProps.bodySubscription.initialData.allSubLandingPages[0].content[0].cards[*].linkToPage
//
// where linkToPage.__typename == "EventPageRecord".
package tepapa

import (
	"bytes"
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

const listingURL = "https://www.tepapa.govt.nz/visit/events"
const baseURL = "https://www.tepapa.govt.nz/"

type Scraper struct{}

func (s *Scraper) Host() model.Host {
	return model.Host{
		Slug:        "te-papa",
		Name:        "Te Papa Tongarewa",
		Website:     "https://www.tepapa.govt.nz/visit/events",
		Description: "The Museum of New Zealand Te Papa Tongarewa hosts lectures, symposia, and public programmes exploring Aotearoa's natural and cultural heritage.",
	}
}

// nextData mirrors the relevant parts of Next.js __NEXT_DATA__.
type nextData struct {
	Props struct {
		PageProps struct {
			BodySubscription struct {
				InitialData struct {
					AllSubLandingPages []struct {
						Content []struct {
							Cards []struct {
								LinkToPage struct {
									Typename     string `json:"__typename"`
									PrimaryTitle string `json:"primaryTitle"`
									Slug         string `json:"slug"`
									When         []struct {
										StartDate string `json:"startDate"`
										EndDate   string `json:"endDate"`
										Time      string `json:"time"`
									} `json:"when"`
									CardDescription *dastDoc `json:"cardDescription"`
								} `json:"linkToPage"`
							} `json:"cards"`
						} `json:"content"`
					} `json:"allSubLandingPages"`
				} `json:"initialData"`
			} `json:"bodySubscription"`
		} `json:"pageProps"`
	} `json:"props"`
}

// dastDoc is a DatoCMS DAST structured-text field.
type dastDoc struct {
	Value struct {
		Document struct {
			Children []dastNode `json:"children"`
		} `json:"document"`
	} `json:"value"`
}

type dastNode struct {
	Type     string     `json:"type"`
	Value    string     `json:"value"`
	Children []dastNode `json:"children"`
}

// dastText extracts plain text from a DAST tree by recursively collecting span values.
func dastText(nodes []dastNode) string {
	var parts []string
	for _, n := range nodes {
		if n.Type == "span" && n.Value != "" {
			parts = append(parts, n.Value)
		}
		if len(n.Children) > 0 {
			if t := dastText(n.Children); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, " ")
}

var timeRe = regexp.MustCompile(`(?i)(\d{1,2})(?:\.(\d{2}))?\s*([ap]m)?`)

// parseTimeStr extracts the start hour/minute from strings like:
// "10.00am to 2.00pm", "10.30–11.30am", "7pm", "11am to 11.15am"
func parseTimeStr(s string) (hour, min int, ok bool) {
	lower := strings.ToLower(s)
	m := timeRe.FindStringSubmatch(lower)
	if m == nil {
		return 0, 0, false
	}
	h, _ := strconv.Atoi(m[1])
	mn := 0
	if m[2] != "" {
		mn, _ = strconv.Atoi(m[2])
	}
	period := m[3]
	if period == "" {
		// Infer AM/PM from anywhere in the string.
		if strings.Contains(lower, "am") {
			period = "am"
		} else if strings.Contains(lower, "pm") {
			period = "pm"
		} else {
			return 0, 0, false
		}
	}
	if period == "pm" && h != 12 {
		h += 12
	} else if period == "am" && h == 12 {
		h = 0
	}
	return h, mn, true
}

func (s *Scraper) Scrape(ctx context.Context) ([]model.Lecture, error) {
	body, err := scraper.Fetch(ctx, listingURL)
	if err != nil {
		return nil, fmt.Errorf("tepapa: fetch: %w", err)
	}

	// Extract __NEXT_DATA__ JSON from the HTML.
	const startTag = `<script id="__NEXT_DATA__" type="application/json">`
	const endTag = `</script>`
	start := bytes.Index(body, []byte(startTag))
	if start < 0 {
		return nil, fmt.Errorf("tepapa: __NEXT_DATA__ script tag not found")
	}
	jsonStart := start + len(startTag)
	end := bytes.Index(body[jsonStart:], []byte(endTag))
	if end < 0 {
		return nil, fmt.Errorf("tepapa: closing </script> not found after __NEXT_DATA__")
	}
	raw := body[jsonStart : jsonStart+end]

	var nd nextData
	if err := json.Unmarshal(raw, &nd); err != nil {
		return nil, fmt.Errorf("tepapa: unmarshal __NEXT_DATA__: %w", err)
	}

	loc := scraper.NZLocation

	pages := nd.Props.PageProps.BodySubscription.InitialData.AllSubLandingPages
	if len(pages) == 0 || len(pages[0].Content) == 0 {
		return nil, fmt.Errorf("tepapa: no content sections found in __NEXT_DATA__")
	}

	var lectures []model.Lecture
	for _, content := range pages[0].Content {
		for _, card := range content.Cards {
			lp := card.LinkToPage
			if lp.Typename != "EventPageRecord" {
				continue
			}
			if lp.PrimaryTitle == "" || lp.Slug == "" {
				continue
			}

			eventURL := baseURL + lp.Slug

			// Use the first date entry that has a startDate.
			for _, when := range lp.When {
				if when.StartDate == "" {
					continue
				}
				date, err := time.Parse("2006-01-02", when.StartDate)
				if err != nil {
					continue
				}

				h, mn, ok := parseTimeStr(when.Time)
				if !ok {
					h, mn = 10, 0 // default to 10am if unparseable
				}
				t := time.Date(date.Year(), date.Month(), date.Day(), h, mn, 0, 0, loc)

				rawDesc := ""
				if lp.CardDescription != nil {
					rawDesc = dastText(lp.CardDescription.Value.Document.Children)
				}
				lectures = append(lectures, model.Lecture{
					ID:          scraper.MakeID(eventURL + when.StartDate),
					Title:       lp.PrimaryTitle,
					Link:        eventURL,
					TimeStart:   t,
					Description: rawDesc,
					Summary:     scraper.TruncateSummary(rawDesc, 200),
					Location:    "Te Papa Tongarewa, 55 Cable Street, Wellington",
					HostSlug:    "te-papa",
				})
				break // only take the first date per event card
			}
		}
	}

	return lectures, nil
}
