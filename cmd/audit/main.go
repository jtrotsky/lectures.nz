// Command audit prints a coverage report of a lectures JSON file by source.
//
// Usage:
//
//	go run ./cmd/audit                          # reads data/lectures-enriched.json
//	go run ./cmd/audit data/lectures.json       # reads specified file
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type lecture struct {
	HostSlug string `json:"host_slug"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Speakers []any  `json:"speakers"`
	Image    string `json:"image"`
}

var noiseKeywords = []string{
	"concert",
	"festival",
	"open day",
	"school holiday",
	"performance",
	"live ",
	"farming",
	"printopia",
	// workshops that are clearly not lectures (craft/hobby); artist/research workshops are fine
	"cyanotype workshop",
	"kintsugi workshop",
	// tours that are sightseeing rather than educational talks
	"guided tour",
	"behind the scenes tour",
}

func main() {
	path := "data/lectures-enriched.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: read %s: %v\n", path, err)
		os.Exit(1)
	}

	var lectures []lecture
	if err := json.Unmarshal(data, &lectures); err != nil {
		fmt.Fprintf(os.Stderr, "audit: parse %s: %v\n", path, err)
		os.Exit(1)
	}

	// Group by host.
	type stats struct {
		total    int
		summary  int
		speakers int
		image    int
	}
	byHost := make(map[string]*stats)
	for _, lec := range lectures {
		s := byHost[lec.HostSlug]
		if s == nil {
			s = &stats{}
			byHost[lec.HostSlug] = s
		}
		s.total++
		if strings.TrimSpace(lec.Summary) != "" {
			s.summary++
		}
		if len(lec.Speakers) > 0 {
			s.speakers++
		}
		if strings.TrimSpace(lec.Image) != "" {
			s.image++
		}
	}

	// Sort hosts by event count descending.
	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return byHost[hosts[i]].total > byHost[hosts[j]].total
	})

	fmt.Printf("\n%-28s %6s  %7s  %8s  %5s\n", "Source", "Events", "Summary", "Speakers", "Image")
	fmt.Println(strings.Repeat("-", 62))
	for _, h := range hosts {
		s := byHost[h]
		fmt.Printf("%-28s %6d  %7d  %8d  %5d\n", h, s.total, s.summary, s.speakers, s.image)
	}
	fmt.Println(strings.Repeat("-", 62))

	var totTotal, totSummary, totSpeakers, totImage int
	for _, s := range byHost {
		totTotal += s.total
		totSummary += s.summary
		totSpeakers += s.speakers
		totImage += s.image
	}
	fmt.Printf("%-28s %6d  %7d  %8d  %5d\n\n", "TOTAL", totTotal, totSummary, totSpeakers, totImage)

	// Flag likely non-lectures.
	fmt.Println("Possible non-lecture events (check these):")
	for _, lec := range lectures {
		text := strings.ToLower(lec.Title + " " + lec.Summary)
		for _, kw := range noiseKeywords {
			if strings.Contains(text, kw) {
				title := lec.Title
				if len(title) > 70 {
					title = title[:70]
				}
				fmt.Printf("  [%s] %s\n", lec.HostSlug, title)
				break
			}
		}
	}
}
