// Command digest sends per-city weekly lecture digests via Buttondown.
//
// Reads data/lectures-enriched.json (falling back to data/lectures.json),
// filters to lectures in the next 14 days (NZ time), groups by city, and
// sends one email per city targeted to that city's subscriber tag.
//
// Required:
//
//	BUTTONDOWN_API_KEY  Buttondown API key
//
// Optional:
//
//	DRY_RUN=1  print emails without sending
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

const (
	buttondownAPI  = "https://api.buttondown.email/v1"
	daysAhead      = 14
	minLectures    = 4 // skip digest for a city if fewer than this many upcoming lectures
	dataEnriched   = "data/lectures-enriched.json"
	dataFallback   = "data/lectures.json"
)

// cityConfig defines the cities we send digests for.
var cityConfig = []struct {
	Name string
	Tag  string
}{
	{"Auckland", "auckland"},
	{"Wellington", "wellington"},
	{"Christchurch", "christchurch"},
	{"Dunedin", "dunedin"},
	{"Hamilton", "hamilton"},
}

// hostCity maps host slugs → city. Keep in sync with cmd/build/main.go.
var hostCity = map[string]string{
	// Auckland
	"auckland": "Auckland", "aut": "Auckland", "artspace": "Auckland",
	"auckland-art-gallery": "Auckland", "auckland-museum": "Auckland",
	"gus-fisher": "Auckland", "meetup": "Auckland", "motat": "Auckland",
	"ockham": "Auckland", "studio-one": "Auckland",
	// Wellington
	"motu": "Wellington", "national-library": "Wellington", "nziia": "Wellington",
	"nz-initiative": "Wellington", "public-record": "Wellington", "rbnz": "Wellington",
	"royal-society": "Wellington", "te-papa": "Wellington", "victoria": "Wellington",
	// Dunedin
	"artgallery-nz": "Dunedin", "otago": "Dunedin",
	// Christchurch
	"canterbury": "Christchurch",
	// Hamilton
	"waikato": "Hamilton",
}

var knownCities = []string{"Auckland", "Wellington", "Christchurch", "Dunedin", "Hamilton"}

func lectureCity(l model.Lecture) string {
	loc := strings.ToLower(l.Location)
	for _, c := range knownCities {
		if strings.Contains(loc, strings.ToLower(c)) {
			return c
		}
	}
	return hostCity[l.HostSlug]
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("digest: %v", err)
	}
}

func run() error {
	apiKey := os.Getenv("BUTTONDOWN_API_KEY")
	dryRun := os.Getenv("DRY_RUN") == "1"
	draft := os.Getenv("DRAFT") == "1"
	if apiKey == "" && !dryRun {
		return fmt.Errorf("BUTTONDOWN_API_KEY not set (use DRY_RUN=1 to test output)")
	}

	lectures, err := loadLectures()
	if err != nil {
		return err
	}

	nzLoc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		return fmt.Errorf("load NZ timezone: %w", err)
	}
	now := time.Now().In(nzLoc)
	cutoff := now.AddDate(0, 0, daysAhead)

	byCityName := map[string][]model.Lecture{}
	for _, l := range lectures {
		if l.Excluded || excludedEventType(l.EventType) {
			continue
		}
		t := l.TimeStart.In(nzLoc)
		if t.Before(now) || t.After(cutoff) {
			continue
		}
		city := lectureCity(l)
		if city == "" {
			continue
		}
		byCityName[city] = append(byCityName[city], l)
	}

	for city := range byCityName {
		sort.Slice(byCityName[city], func(i, j int) bool {
			return byCityName[city][i].TimeStart.Before(byCityName[city][j].TimeStart)
		})
	}

	sent := 0
	for _, c := range cityConfig {
		ls := byCityName[c.Name]
		if len(ls) < minLectures {
			log.Printf("%s: only %d lectures in next %d days, skipping", c.Name, len(ls), daysAhead)
			continue
		}
		subject := buildSubject(c.Name, now, cutoff)
		body := buildBody(c.Name, ls, now, cutoff)
		if dryRun {
			log.Printf("=== DRY RUN: %s (%d lectures) ===\nSubject: %s\n\n%s\n", c.Name, len(ls), subject, body)
			continue
		}
		status := "sent"
		if draft {
			status = "draft"
		}
		if err := sendEmail(apiKey, subject, body, c.Tag, status); err != nil {
			log.Printf("ERROR %s: %v", c.Name, err)
			continue
		}
		log.Printf("sent: %s (%d lectures)", c.Name, len(ls))
		sent++
	}

	if !dryRun {
		mode := "sent"
		if draft {
			mode = "saved as draft"
		}
		log.Printf("done: %d/%d digests %s", sent, len(cityConfig), mode)
	}
	return nil
}

func buildSubject(city string, from, to time.Time) string {
	if from.Month() == to.Month() {
		return fmt.Sprintf("%s lectures · %d–%d %s", city, from.Day(), to.Day(), from.Format("January"))
	}
	return fmt.Sprintf("%s lectures · %d %s – %d %s", city, from.Day(), from.Format("Jan"), to.Day(), to.Format("Jan"))
}

// excludedEventType mirrors the build's excludedEventTypes filter.
var excludedEventTypes = map[string]bool{
	"market": true, "concert": true, "ceremony": true, "fitness": true,
	"orientation": true, "festival": true, "meetup": true, "conference": true,
	"course": true, "AGM": true, "class": true, "workshop": true,
}

func excludedEventType(t string) bool { return t != "" && excludedEventTypes[t] }

// dateLabel returns "Today", "Tomorrow", day name (within a week), or "Mon 2 Jan".
func dateLabel(t, now time.Time) string {
	tDate := t.Truncate(24 * time.Hour)
	nowDate := now.Truncate(24 * time.Hour)
	diff := int(tDate.Sub(nowDate).Hours() / 24)
	switch diff {
	case 0:
		return "Today"
	case 1:
		return "Tomorrow"
	}
	if diff < 7 {
		return t.Format("Monday")
	}
	return t.Format("Monday 2 January")
}

type dateGroup struct {
	Label    string
	Lectures []model.Lecture
}

func groupByDate(lectures []model.Lecture, now time.Time, loc *time.Location) []dateGroup {
	var groups []dateGroup
	idx := map[string]int{}
	for _, l := range lectures {
		t := l.TimeStart.In(loc)
		key := t.Format("2006-01-02")
		if i, ok := idx[key]; ok {
			groups[i].Lectures = append(groups[i].Lectures, l)
		} else {
			idx[key] = len(groups)
			groups = append(groups, dateGroup{
				Label:    dateLabel(t, now),
				Lectures: []model.Lecture{l},
			})
		}
	}
	return groups
}

func buildBody(city string, lectures []model.Lecture, from, to time.Time) string {
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	now := from.In(nzLoc)

	dateRange := fmt.Sprintf("%d–%d %s", from.Day(), to.Day(), from.Format("January"))
	if from.Month() != to.Month() {
		dateRange = fmt.Sprintf("%d %s – %d %s", from.Day(), from.Format("Jan"), to.Day(), to.Format("Jan"))
	}

	groups := groupByDate(lectures, now, nzLoc)

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#e8dfe5;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;">
<table width="100%" cellpadding="0" cellspacing="0" style="background:#e8dfe5;padding:24px 0 40px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%;">`)

	// Header
	fmt.Fprintf(&b, `
<tr><td style="padding:32px 32px 20px;">
  <a href="https://lectures.nz" style="font-family:Georgia,'Times New Roman',serif;font-size:32px;font-weight:400;color:#000;text-decoration:none;letter-spacing:-0.02em;">lectures.nz</a>
</td></tr>
<tr><td style="padding:0 32px 24px;border-bottom:1px solid rgba(0,0,0,0.12);">
  <p style="margin:0;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:11px;color:rgba(0,0,0,0.45);text-transform:uppercase;letter-spacing:0.08em;font-weight:600;">%s &nbsp;·&nbsp; %s</p>
</td></tr>`, city, dateRange)

	// Date groups
	for _, g := range groups {
		// Date heading
		fmt.Fprintf(&b, `
<tr><td style="padding:24px 32px 0;">
  <p style="margin:0;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:11px;font-weight:600;letter-spacing:0.08em;text-transform:uppercase;color:rgba(0,0,0,0.35);">%s</p>
</td></tr>`, html.EscapeString(g.Label))

		for _, l := range g.Lectures {
			t := l.TimeStart.In(nzLoc)
			timeStr := t.Format("3:04pm")

			summary := l.Summary
			if summary == "" {
				summary = l.Description
			}
			if len(summary) > 180 {
				summary = summary[:177] + "…"
			}

			// Host/organiser line
			organiser := l.Organiser
			if organiser == "" {
				organiser = l.HostSlug
			}

			fmt.Fprintf(&b, `
<tr><td style="padding:12px 32px 20px;border-bottom:1px solid rgba(0,0,0,0.08);">
  <table width="100%%" cellpadding="0" cellspacing="0">
  <tr><td>
    <p style="margin:0 0 2px;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:11px;color:rgba(0,0,0,0.4);">%s</p>
    <h2 style="margin:0 0 6px;font-family:Georgia,'Times New Roman',serif;font-size:19px;font-weight:400;line-height:1.3;color:#000;">
      <a href="%s" style="color:#380d25;text-decoration:none;">%s</a>
    </h2>`,
				html.EscapeString(timeStr),
				html.EscapeString(l.Link),
				html.EscapeString(l.DisplayTitle()),
			)

			if l.Location != "" {
				fmt.Fprintf(&b, `<p style="margin:0 0 6px;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:12px;color:rgba(0,0,0,0.45);">%s</p>`, html.EscapeString(l.Location))
			}

			if summary != "" {
				fmt.Fprintf(&b, `<p style="margin:0;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:13px;line-height:1.55;color:rgba(0,0,0,0.65);">%s</p>`, html.EscapeString(summary))
			}

			b.WriteString(`</td></tr></table></td></tr>`)
		}
	}

	// Footer
	b.WriteString(`
<tr><td style="padding:28px 32px 0;">
  <a href="https://lectures.nz" style="font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:12px;font-weight:600;letter-spacing:0.04em;text-transform:uppercase;color:#380d25;text-decoration:none;">View all lectures →</a>
</td></tr>
<tr><td style="padding:16px 32px 32px;">
  <p style="margin:0;font-family:'Helvetica Neue',Helvetica,Arial,sans-serif;font-size:11px;color:rgba(0,0,0,0.35);line-height:1.6;">
    You subscribed at lectures.nz &nbsp;·&nbsp;
    <a href="{{unsubscribe_url}}" style="color:rgba(0,0,0,0.35);text-decoration:underline;">Unsubscribe</a>
  </p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`)

	return b.String()
}

type emailRequest struct {
	Subject string       `json:"subject"`
	Body    string       `json:"body"`
	Status  string       `json:"status"`
	Filters []tagFilter  `json:"filters,omitempty"`
}

type tagFilter struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func sendEmail(apiKey, subject, body, tag, status string) error {
	payload := emailRequest{
		Subject: subject,
		Body:    body,
		Status:  status,
		Filters: []tagFilter{{Type: "tag", Value: tag}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", buttondownAPI+"/emails", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("buttondown %d: %s", resp.StatusCode, body)
	}
	return nil
}

func loadLectures() ([]model.Lecture, error) {
	path := dataEnriched
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = dataFallback
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var lectures []model.Lecture
	if err := json.NewDecoder(f).Decode(&lectures); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return lectures, nil
}
