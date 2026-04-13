// Command collect fetches lectures from all NZ sources and writes data/lectures.json
// and data/hosts.json.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
	"github.com/jtrotsky/lectures.nz/internal/scraper"
	"github.com/jtrotsky/lectures.nz/internal/topics"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/auckland"
	aucklandartgallery "github.com/jtrotsky/lectures.nz/internal/scraper/sources/auckland-art-gallery"
	artgallerynz "github.com/jtrotsky/lectures.nz/internal/scraper/sources/artgallery-nz"
	aucklandmuseum "github.com/jtrotsky/lectures.nz/internal/scraper/sources/auckland-museum"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/aut"
	gusfisher "github.com/jtrotsky/lectures.nz/internal/scraper/sources/gus-fisher"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/motat"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/canterbury"
	nationallibrary "github.com/jtrotsky/lectures.nz/internal/scraper/sources/national-library"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/ockham"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/otago"
	publicrecord "github.com/jtrotsky/lectures.nz/internal/scraper/sources/public-record"
	studioone "github.com/jtrotsky/lectures.nz/internal/scraper/sources/studio-one"
	tepapa "github.com/jtrotsky/lectures.nz/internal/scraper/sources/te-papa"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/victoria"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/eventbrite"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/nziia"
	royalsociety "github.com/jtrotsky/lectures.nz/internal/scraper/sources/royal-society"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/massey"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/motu"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/rbnz"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/artspace"
	nzinitiative "github.com/jtrotsky/lectures.nz/internal/scraper/sources/nz-initiative"
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/meetup"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("collect: %v", err)
	}
}

const descCachePath = "data/descriptions-cache.json"

// loadDescCache reads the on-disk descriptions cache (URL → description text).
func loadDescCache() map[string]string {
	data, err := os.ReadFile(descCachePath)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

// saveDescCache writes the descriptions cache to disk.
func saveDescCache(m map[string]string) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Printf("WARN: marshal descriptions cache: %v", err)
		return
	}
	if err := os.WriteFile(descCachePath, data, 0644); err != nil {
		log.Printf("WARN: write descriptions cache: %v", err)
	}
}

// skipDescDomains lists URL substrings whose pages reliably return unusable
// content when fetched for description extraction.
var skipDescDomains = []string{
	"eventbrite.co.nz",
	"eventbrite.com",
	"eventfinda.co.nz",
}

func skipDescDomain(link string) bool {
	lower := strings.ToLower(link)
	for _, d := range skipDescDomains {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

// skipDescFetch lists host slugs whose detail pages reliably return unusable
// content (JS-only rendering, bot detection, login walls, etc.).
var skipDescFetch = map[string]bool{
	"massey":     true, // returns "outdated browser" page
	"eventbrite": true, // JS-rendered; description already comes from API
}

// fillDescriptions fetches full descriptions for lectures with thin Description
// fields (< 200 chars), using a URL-keyed cache to avoid repeat fetches.
func fillDescriptions(ctx context.Context, lectures []model.Lecture, cache map[string]string) {
	const minLen = 200
	fetched, hits := 0, 0
	for i, l := range lectures {
		if len(l.Description) >= minLen {
			continue
		}
		if l.Link == "" || skipDescFetch[l.HostSlug] || skipDescDomain(l.Link) {
			continue
		}
		if cached, ok := cache[l.Link]; ok {
			if len(cached) > len(lectures[i].Description) {
				lectures[i].Description = cached
			}
			hits++
			continue
		}
		body, err := scraper.Fetch(ctx, l.Link)
		if err != nil {
			log.Printf("DESC  [%s] fetch failed: %v", l.HostSlug, err)
			cache[l.Link] = l.Description // cache to avoid retrying
			continue
		}
		desc := scraper.ExtractDescription(body)
		// Only use the fetched description if it's genuinely better — not a
		// navigation dump or error page.
		if len(desc) > len(l.Description) && !scraper.LooksLikeGarbage(desc) {
			lectures[i].Description = desc
		}
		cache[l.Link] = lectures[i].Description
		fetched++
		log.Printf("DESC  [%s] fetched: %q", l.HostSlug, l.Title[:min(50, len(l.Title))])
	}
	log.Printf("Descriptions: %d fetched, %d from cache", fetched, hits)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	descCache := loadDescCache()

	scrapers := []scraper.Scraper{
		&auckland.Scraper{},
		&aut.Scraper{},
		&ockham.Scraper{},
		&victoria.Scraper{},
		&otago.Scraper{},
		&canterbury.Scraper{},
		&aucklandartgallery.Scraper{},
		&tepapa.Scraper{},
		&nationallibrary.Scraper{},
		&artgallerynz.Scraper{},
		&studioone.Scraper{},
		&publicrecord.Scraper{},
		&aucklandmuseum.Scraper{},
		&gusfisher.Scraper{},
		&motat.Scraper{},
		&eventbrite.Scraper{},
		&nziia.Scraper{},
		&royalsociety.Scraper{},
		&massey.Scraper{},
		&motu.Scraper{},
		&rbnz.Scraper{},
		&artspace.Scraper{},
		&nzinitiative.Scraper{},
		&meetup.Scraper{},
	}

	type result struct {
		host     model.Host
		lectures []model.Lecture
		err      error
	}

	results := make(chan result, len(scrapers))
	var wg sync.WaitGroup

	for _, s := range scrapers {
		wg.Add(1)
		go func(s scraper.Scraper) {
			defer wg.Done()
			h := s.Host()
			lecs, err := s.Scrape(ctx)
			results <- result{host: h, lectures: lecs, err: err}
		}(s)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results.
	hostMap := make(map[string]model.Host)
	seen := make(map[string]bool) // dedup by ID
	var allLectures []model.Lecture

	for r := range results {
		if r.err != nil {
			log.Printf("ERROR [%s]: %v", r.host.Slug, r.err)
			// Still register the host even if scraping failed.
			hostMap[r.host.Slug] = r.host
			continue
		}
		log.Printf("OK    [%s]: %d lectures", r.host.Slug, len(r.lectures))
		hostMap[r.host.Slug] = r.host

		now := time.Now()
		for _, l := range r.lectures {
			if l.TimeStart.Before(now) {
				continue // skip past events
			}
			if seen[l.ID] {
				continue // dedup
			}
			// Decode HTML entities in text fields (e.g. &amp; &ndash; &mdash;).
			l.Title = html.UnescapeString(l.Title)
			l.Summary = html.UnescapeString(l.Summary)
			l.Description = html.UnescapeString(l.Description)
			// Skip non-lecture events (open days, orientations, etc.).
			if topics.IsExcluded(l.Title) {
				log.Printf("SKIP  [%s]: %q", l.HostSlug, l.Title)
				continue
			}
			seen[l.ID] = true
			if len(l.Tags) == 0 {
				l.Tags = topics.Infer(l.Title, l.Summary)
			}
			allLectures = append(allLectures, l)
		}
	}

	// Fill thin descriptions from detail pages (cached).
	fillDescriptions(ctx, allLectures, descCache)
	saveDescCache(descCache)

	// Sort by start time.
	sort.Slice(allLectures, func(i, j int) bool {
		return allLectures[i].TimeStart.Before(allLectures[j].TimeStart)
	})

	// Build hosts list with lectures embedded.
	var hosts []model.Host
	for _, h := range hostMap {
		for _, l := range allLectures {
			if l.HostSlug == h.Slug {
				h.Lectures = append(h.Lectures, l)
			}
		}
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})

	if err := os.MkdirAll("data", 0755); err != nil {
		return fmt.Errorf("mkdir data: %w", err)
	}

	if err := writeJSON("data/lectures.json", allLectures); err != nil {
		return err
	}
	if err := writeJSON("data/hosts.json", hosts); err != nil {
		return err
	}

	log.Printf("Wrote %d lectures from %d hosts", len(allLectures), len(hosts))
	return nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	log.Printf("Wrote %s", path)
	return nil
}
