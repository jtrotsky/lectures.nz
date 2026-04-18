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
	"github.com/jtrotsky/lectures.nz/internal/scraper/sources/treasury"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("collect: %v", err)
	}
}

const descCachePath = "data/descriptions-cache.json"

// cachedDetail holds the description text and any speakers extracted from a
// detail page. Replaces the old map[string]string format (URL → plain string).
type cachedDetail struct {
	Description string          `json:"description"`
	Speakers    []model.Speaker `json:"speakers,omitempty"`
}

// loadDescCache reads the on-disk descriptions cache (URL → cachedDetail).
// Migrates transparently from the old format (URL → plain string).
func loadDescCache() map[string]cachedDetail {
	data, err := os.ReadFile(descCachePath)
	if err != nil {
		return make(map[string]cachedDetail)
	}
	// Try new format first.
	var m map[string]cachedDetail
	if json.Unmarshal(data, &m) == nil {
		return m
	}
	// Migrate from old format (map of plain strings).
	var old map[string]string
	if json.Unmarshal(data, &old) == nil {
		m = make(map[string]cachedDetail, len(old))
		for k, v := range old {
			m[k] = cachedDetail{Description: v}
		}
		return m
	}
	return make(map[string]cachedDetail)
}

// saveDescCache writes the descriptions cache to disk.
func saveDescCache(m map[string]cachedDetail) {
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
	"eventbrite": true, // JS-rendered; description already comes from API
}

// isEventbriteLink returns true when the URL points to an Eventbrite event page.
func isEventbriteLink(link string) bool {
	lower := strings.ToLower(link)
	return strings.Contains(lower, "eventbrite.co.nz") || strings.Contains(lower, "eventbrite.com")
}

// fillDescriptions fetches full descriptions (and speakers) for lectures with
// thin Description fields (< 200 chars), using a URL-keyed cache.
func fillDescriptions(ctx context.Context, lectures []model.Lecture, cache map[string]cachedDetail) {
	const minLen = 200
	fetched, hits := 0, 0
	for i, l := range lectures {
		// Special case: Eventbrite links on non-Eventbrite host pages (e.g. Auckland Uni
		// listing Eventbrite events). The description is skipped but speakers can be
		// extracted from the artistInfo JSON embedded in the page HTML.
		if l.Link != "" && isEventbriteLink(l.Link) && l.HostSlug != "eventbrite" && len(l.Speakers) == 0 {
			if cached, ok := cache[l.Link]; ok {
				if len(cached.Speakers) > 0 {
					lectures[i].Speakers = cached.Speakers
					hits++
				}
			} else {
				if body, err := scraper.Fetch(ctx, l.Link); err == nil {
					if sp := scraper.ExtractEventbriteSpeakers(body); len(sp) > 0 {
						lectures[i].Speakers = sp
						log.Printf("SPKR  [%s] %d speaker(s) via Eventbrite: %q", l.HostSlug, len(sp), l.Title[:min(50, len(l.Title))])
					}
					cache[l.Link] = cachedDetail{Description: l.Description, Speakers: lectures[i].Speakers}
					fetched++
				}
			}
			continue
		}

		if len(l.Description) >= minLen {
			continue
		}
		if l.Link == "" || skipDescFetch[l.HostSlug] || skipDescDomain(l.Link) {
			continue
		}
		if cached, ok := cache[l.Link]; ok {
			if len(cached.Description) > len(lectures[i].Description) {
				lectures[i].Description = cached.Description
			}
			if len(lectures[i].Speakers) == 0 && len(cached.Speakers) > 0 {
				lectures[i].Speakers = cached.Speakers
			}
			hits++
			continue
		}
		body, err := scraper.Fetch(ctx, l.Link)
		if err != nil {
			log.Printf("DESC  [%s] fetch failed: %v", l.HostSlug, err)
			cache[l.Link] = cachedDetail{Description: l.Description}
			continue
		}

		desc := scraper.ExtractDescription(body)
		if !scraper.LooksLikeGarbage(desc) {
			if len(desc) > len(l.Description) {
				lectures[i].Description = desc
			} else if scraper.HasSpeakerInfo(desc) && !scraper.HasSpeakerInfo(l.Description) {
				// Append "Presented by X" text so enrichment can see the speaker name.
				lectures[i].Description = strings.TrimRight(l.Description, " .") + ". " + desc
			}
		}

		// Extract speakers from the full page body when none are set.
		if len(lectures[i].Speakers) == 0 {
			if sp := scraper.ExtractSpeakers(body); len(sp) > 0 {
				lectures[i].Speakers = sp
				log.Printf("SPKR  [%s] %d speaker(s): %q", l.HostSlug, len(sp), l.Title[:min(50, len(l.Title))])
			}
		}

		cache[l.Link] = cachedDetail{
			Description: lectures[i].Description,
			Speakers:    lectures[i].Speakers,
		}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
		&treasury.Scraper{},
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
