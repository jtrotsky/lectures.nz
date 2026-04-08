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
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("collect: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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
