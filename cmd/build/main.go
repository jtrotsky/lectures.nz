// Command build reads data/lectures.json and data/hosts.json and generates
// the static site in public/.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/calendar"
	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/topics"
)

// templateDir holds the path to the templates directory, set in run().
var templateDir string

// templateData is passed to every page template.
type templateData struct {
	Hosts        []model.Host
	Topics       []topics.Topic
	LecturesJSON template.JS
}

// indexData extends templateData for the index page.
type indexData struct {
	templateData
	Groups []dateGroup
}

// hostPageData extends templateData for host pages.
type hostPageData struct {
	templateData
	Host model.Host
}

// lecturePageData extends templateData for lecture detail pages.
type lecturePageData struct {
	templateData
	Lecture      model.Lecture
	Host         model.Host
	MoreLectures []model.Lecture
}

// dateGroup groups lectures under a formatted date heading.
type dateGroup struct {
	DateKey   string // e.g. "2026-04-10" for sorting/filtering
	DateLabel string // e.g. "Thursday 10 April"
	Lectures  []model.Lecture
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run() error {
	lectures, err := loadLectures("data/lectures.json")
	if err != nil {
		return fmt.Errorf("load lectures: %w", err)
	}
	hosts, err := loadHosts("data/hosts.json")
	if err != nil {
		return fmt.Errorf("load hosts: %w", err)
	}

	templateDir = "templates"

	// Prepare output directory.
	if err := os.MkdirAll("public", 0755); err != nil {
		return fmt.Errorf("mkdir public: %w", err)
	}

	// Copy static assets.
	if err := copyDir("static", "public/static"); err != nil {
		return fmt.Errorf("copy static: %w", err)
	}

	// Build shared template data.
	lecturesJSONBytes, err := json.Marshal(lectures)
	if err != nil {
		return fmt.Errorf("marshal lectures json: %w", err)
	}
	base := templateData{
		Hosts:        hosts,
		Topics:       topics.All(),
		LecturesJSON: template.JS(lecturesJSONBytes),
	}

	// Index page.
	groups := groupByDate(lectures)
	if err := renderTemplate("index.html", "public/index.html", indexData{
		templateData: base,
		Groups:       groups,
	}); err != nil {
		return err
	}
	log.Printf("built public/index.html (%d lectures, %d groups)", len(lectures), len(groups))

	// Global calendar.
	if err := writeCalendar("public/calendar.ics", lectures); err != nil {
		return err
	}

	// RSS feed.
	if err := writeRSS("public/rss.xml", lectures); err != nil {
		return err
	}
	log.Printf("built public/rss.xml")

	// Host pages + lecture pages.
	for _, h := range hosts {
		hostDir := filepath.Join("public", h.Slug)
		if err := os.MkdirAll(hostDir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", hostDir, err)
		}

		// Host page.
		if err := renderTemplate("host.html", filepath.Join(hostDir, "index.html"), hostPageData{
			templateData: base,
			Host:         h,
		}); err != nil {
			return err
		}
		log.Printf("built %s/index.html", h.Slug)

		// Host calendar.
		hostLectures := lecturesForHost(lectures, h.Slug)
		if err := writeCalendar(filepath.Join(hostDir, "calendar.ics"), hostLectures); err != nil {
			return err
		}

		// Lecture detail pages.
		for _, l := range hostLectures {
			lecDir := filepath.Join(hostDir, l.ID)
			if err := os.MkdirAll(lecDir, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", lecDir, err)
			}

			// Other lectures from same host (up to 3, excluding current).
			more := otherLectures(hostLectures, l.ID, 3)

			if err := renderTemplate("lecture.html", filepath.Join(lecDir, "index.html"), lecturePageData{
				templateData: base,
				Lecture:      l,
				Host:         h,
				MoreLectures: more,
			}); err != nil {
				return err
			}

			// Per-lecture calendar.
			if err := writeCalendar(filepath.Join(lecDir, "calendar.ics"), []model.Lecture{l}); err != nil {
				return err
			}
		}
	}

	log.Println("Build complete → public/")
	return nil
}

// ----- Data loading ---------------------------------------------------

func loadLectures(path string) ([]model.Lecture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lectures []model.Lecture
	if err := json.Unmarshal(data, &lectures); err != nil {
		return nil, err
	}
	return lectures, nil
}

func loadHosts(path string) ([]model.Host, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hosts []model.Host
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

// ----- Template handling ----------------------------------------------

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"gt":         func(a, b int) bool { return a > b },
		"gcalURL":    gcalURL,
		"outlookURL": outlookURL,
		"yahooURL":   yahooURL,
	}
}

// gcalURL returns a Google Calendar "add event" URL for a lecture.
func gcalURL(l model.Lecture) string {
	const fmt = "20060102T150405Z"
	start := l.TimeStart.UTC().Format(fmt)
	var end string
	if l.TimeEnd != nil {
		end = l.TimeEnd.UTC().Format(fmt)
	} else {
		end = l.TimeStart.UTC().Add(time.Hour).Format(fmt)
	}
	p := url.Values{}
	p.Set("action", "TEMPLATE")
	p.Set("text", l.Title)
	p.Set("dates", start+"/"+end)
	if l.Location != "" {
		p.Set("location", l.Location)
	}
	details := l.Summary
	if l.Link != "" {
		if details != "" {
			details += "\n\n"
		}
		details += l.Link
	}
	if details != "" {
		p.Set("details", details)
	}
	return "https://calendar.google.com/calendar/render?" + p.Encode()
}

// outlookURL returns an Outlook Web calendar "add event" URL.
func outlookURL(l model.Lecture) string {
	const fmt = "2006-01-02T15:04:05"
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}
	start := l.TimeStart.In(nzLoc).Format(fmt)
	var end string
	if l.TimeEnd != nil {
		end = l.TimeEnd.In(nzLoc).Format(fmt)
	} else {
		end = l.TimeStart.In(nzLoc).Add(time.Hour).Format(fmt)
	}
	p := url.Values{}
	p.Set("path", "/calendar/action/compose")
	p.Set("rru", "addevent")
	p.Set("subject", l.Title)
	p.Set("startdt", start)
	p.Set("enddt", end)
	if l.Location != "" {
		p.Set("location", l.Location)
	}
	if l.Summary != "" {
		p.Set("body", l.Summary)
	}
	return "https://outlook.live.com/calendar/0/deeplink/compose?" + p.Encode()
}

// yahooURL returns a Yahoo Calendar "add event" URL.
func yahooURL(l model.Lecture) string {
	const fmt = "20060102T150405Z"
	start := l.TimeStart.UTC().Format(fmt)
	var end string
	if l.TimeEnd != nil {
		end = l.TimeEnd.UTC().Format(fmt)
	} else {
		end = l.TimeStart.UTC().Add(time.Hour).Format(fmt)
	}
	p := url.Values{}
	p.Set("v", "60")
	p.Set("title", l.Title)
	p.Set("st", start)
	p.Set("et", end)
	if l.Location != "" {
		p.Set("in_loc", l.Location)
	}
	if l.Summary != "" {
		p.Set("desc", l.Summary)
	}
	return "https://calendar.yahoo.com/?" + p.Encode()
}

// renderTemplate parses base.html + the named page template as a fresh set
// each time, so {{define}} blocks don't bleed between pages.
func renderTemplate(page, outPath string, data any) error {
	base := filepath.Join(templateDir, "base.html")
	pageFile := filepath.Join(templateDir, page)

	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFiles(base, pageFile)
	if err != nil {
		return fmt.Errorf("parse templates (%s): %w", page, err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		return fmt.Errorf("execute %s: %w", page, err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}

// ----- Grouping -------------------------------------------------------

func groupByDate(lectures []model.Lecture) []dateGroup {
	// Already sorted by time from collect.
	groups := []dateGroup{}
	groupIndex := map[string]int{}

	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	today := time.Now().In(nzLoc)

	for _, l := range lectures {
		lTime := l.TimeStart.In(nzLoc)
		key := lTime.Format("2006-01-02")

		idx, exists := groupIndex[key]
		if !exists {
			var label string
			if sameDay(lTime, today) {
				label = "Today — " + lTime.Format("Monday 2 January")
			} else if sameDay(lTime, today.AddDate(0, 0, 1)) {
				label = "Tomorrow — " + lTime.Format("Monday 2 January")
			} else {
				label = lTime.Format("Monday 2 January")
			}
			groups = append(groups, dateGroup{DateKey: key, DateLabel: label})
			idx = len(groups) - 1
			groupIndex[key] = idx
		}
		groups[idx].Lectures = append(groups[idx].Lectures, l)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].DateKey < groups[j].DateKey
	})
	return groups
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func lecturesForHost(all []model.Lecture, slug string) []model.Lecture {
	var out []model.Lecture
	for _, l := range all {
		if l.HostSlug == slug {
			out = append(out, l)
		}
	}
	return out
}

func otherLectures(all []model.Lecture, excludeID string, limit int) []model.Lecture {
	var out []model.Lecture
	for _, l := range all {
		if l.ID != excludeID {
			out = append(out, l)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// ----- Calendar -------------------------------------------------------

func writeCalendar(path string, lectures []model.Lecture) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := calendar.Write(f, lectures); err != nil {
		return fmt.Errorf("write calendar %s: %w", path, err)
	}
	return nil
}

// ----- RSS feed -------------------------------------------------------

func writeRSS(path string, lectures []model.Lecture) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(f, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>lectures.nz</title>
    <link>https://lectures.nz</link>
    <atom:link href="https://lectures.nz/rss.xml" rel="self" type="application/rss+xml"/>
    <description>Upcoming public lectures in New Zealand</description>
    <language>en-nz</language>
`)

	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	for _, l := range lectures {
		title := xmlEscape(l.Title)
		link := fmt.Sprintf("https://lectures.nz/%s/%s/", l.HostSlug, l.ID)
		desc := xmlEscape(l.Summary)
		pubDate := l.TimeStart.In(nzLoc).Format(time.RFC1123Z)
		fmt.Fprintf(f, `    <item>
      <title>%s</title>
      <link>%s</link>
      <guid>%s</guid>
      <pubDate>%s</pubDate>
      <description>%s</description>
    </item>
`, title, link, link, pubDate, desc)
	}

	fmt.Fprintf(f, `  </channel>
</rss>`)
	return nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// ----- Static file copy -----------------------------------------------

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
