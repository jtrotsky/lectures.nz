// Command enrich enriches lectures.json using a local Ollama instance and
// writes the result to data/lectures-enriched.json.
//
// For each lecture it:
//  1. Extracts speaker name(s) from title + description
//  2. Strips speaker suffixes from titles (e.g. "Event | Jane Smith")
//  3. Writes a short summary (index card) and longer description (detail page)
//
// Settings are loaded from .env then overridden by real env vars.
//
// Required:
//
//	OLLAMA_HOST   Ollama base URL (e.g. http://192.168.1.1:11434)
//
// Optional:
//
//	OLLAMA_MODEL     model to use (default: gemma4:e4b)
//	DRY_RUN=1        print prompts, skip Ollama calls
//	FORCE_REFRESH=1  re-enrich all, ignore cache
//	REFRESH_SOURCE=X re-enrich only events from this host_slug
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

const (
	inputPath  = "data/lectures.json"
	outputPath = "data/lectures-enriched.json"
	cachePath  = "data/enriched-cache.json"
)

// cacheEntry holds the enriched fields we persist between runs.
type cacheEntry struct {
	EventType     string          `json:"event_type,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Description   string          `json:"description,omitempty"`
	Speakers      []model.Speaker `json:"speakers,omitempty"`
	Excluded      bool            `json:"excluded,omitempty"`
	ExcludeReason string          `json:"exclude_reason,omitempty"`
}

// enrichResponse is what we expect back from Ollama (parsed from the JSON the model emits).
type enrichResponse struct {
	EventType     string          `json:"event_type"`
	Title         string          `json:"title"`
	Summary       string          `json:"summary"`
	Description   string          `json:"description"`
	Speakers      []model.Speaker `json:"speakers"`
	Exclude       bool            `json:"exclude"`
	ExcludeReason string          `json:"exclude_reason"`
}

// sourceStats tracks per-host counts for the summary table.
type sourceStats struct {
	cached     int
	refreshed  int
	unenriched int
}

func main() {
	loadDotEnv(".env")

	ollamaHost := os.Getenv("OLLAMA_HOST")
	ollamaModel := os.Getenv("OLLAMA_MODEL")
	if ollamaModel == "" {
		ollamaModel = "gemma4:e4b"
	}
	dryRun := os.Getenv("DRY_RUN") == "1"
	forceRefresh := os.Getenv("FORCE_REFRESH") == "1"
	refreshSource := os.Getenv("REFRESH_SOURCE")
	cacheOnly := ollamaHost == ""

	if err := run(ollamaHost, ollamaModel, dryRun, forceRefresh, refreshSource, cacheOnly); err != nil {
		log.Fatalf("enrich: %v", err)
	}
}

func run(ollamaHost, ollamaModel string, dryRun, forceRefresh bool, refreshSource string, cacheOnly bool) error {
	// Load input.
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("%s not found — run: go run ./cmd/collect", inputPath)
	}
	var lectures []model.Lecture
	if err := json.Unmarshal(raw, &lectures); err != nil {
		return fmt.Errorf("parse %s: %w", inputPath, err)
	}

	// Load cache.
	cache := map[string]cacheEntry{}
	if b, err := os.ReadFile(cachePath); err == nil {
		if err := json.Unmarshal(b, &cache); err != nil {
			log.Printf("warn: cache parse error, starting fresh: %v", err)
		}
	}

	// Print run header.
	cached := 0
	for _, l := range lectures {
		if _, ok := cache[l.ID]; ok {
			cached++
		}
	}
	todo := len(lectures) - cached
	switch {
	case cacheOnly:
		fmt.Printf("Applying cache to %d lectures (%d cached, %d unenriched)\n", len(lectures), cached, todo)
	case forceRefresh:
		fmt.Printf("FORCE_REFRESH: re-enriching all %d lectures using %s @ %s\n", len(lectures), ollamaModel, ollamaHost)
	case refreshSource != "":
		srcCount := 0
		for _, l := range lectures {
			if l.HostSlug == refreshSource {
				srcCount++
			}
		}
		fmt.Printf("REFRESH_SOURCE=%s: re-enriching %d events, %d others cached, using %s @ %s\n",
			refreshSource, srcCount, cached-srcCount, ollamaModel, ollamaHost)
	default:
		fmt.Printf("Enriching %d lectures (%d cached) using %s @ %s\n", todo, cached, ollamaModel, ollamaHost)
	}

	// Warm up the model so it's loaded into memory before the main loop.
	if !cacheOnly && !dryRun && todo > 0 {
		fmt.Printf("Warming up %s...", ollamaModel)
		if _, err := ollamaGenerate(ollamaHost, ollamaModel, "OK"); err != nil {
			fmt.Printf(" failed (%v), continuing anyway\n", err)
		} else {
			fmt.Println(" ready")
		}
	}

	stats := map[string]*sourceStats{}
	enriched := make([]model.Lecture, 0, len(lectures))

	for i, lec := range lectures {
		slug := lec.HostSlug
		if slug == "" {
			slug = "unknown"
		}
		if _, ok := stats[slug]; !ok {
			stats[slug] = &sourceStats{}
		}
		st := stats[slug]

		isSourceRefresh := refreshSource != "" && lec.HostSlug == refreshSource
		_, cached := cache[lec.ID]
		inCache := lec.ID != "" && cached

		if inCache && !forceRefresh && !isSourceRefresh {
			fmt.Printf("[%3d/%d] %s (cached)\n", i+1, len(lectures), truncate(lec.Title, 50))
			out := applyCache(lec, cache[lec.ID])
			enriched = append(enriched, out)
			st.cached++
			continue
		}

		if cacheOnly {
			enriched = append(enriched, lec)
			st.unenriched++
			continue
		}

		fmt.Printf("[%3d/%d] %s", i+1, len(lectures), truncate(lec.Title, 50))
		result, err := enrich(lec, ollamaHost, ollamaModel, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  WARN: %s: %v\n", truncate(lec.Title, 50), err)
			enriched = append(enriched, lec)
			st.unenriched++
			continue
		}
		enriched = append(enriched, result)
		fmt.Println(" ✓")
		st.refreshed++

		if lec.ID != "" {
			cache[lec.ID] = cacheEntry{
				EventType:     result.EventType,
				Summary:       result.Summary,
				Description:   result.Description,
				Speakers:      result.Speakers,
				Excluded:      result.Excluded,
				ExcludeReason: result.ExcludeReason,
			}
		}
	}

	// Save cache.
	if b, err := json.MarshalIndent(cache, "", "  "); err == nil {
		if err := os.WriteFile(cachePath, b, 0644); err != nil {
			log.Printf("warn: failed to save cache: %v", err)
		}
	}

	// Save output.
	b, err := json.MarshalIndent(enriched, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := os.WriteFile(outputPath, b, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	fmt.Printf("\nWrote %s\n", outputPath)

	// Per-source summary table.
	slugs := make([]string, 0, len(stats))
	for s := range stats {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	fmt.Printf("\n%-30s %8s %10s %12s\n", "Source", "Cached", "Refreshed", "Unenriched")
	fmt.Println(strings.Repeat("-", 62))
	for _, slug := range slugs {
		s := stats[slug]
		fmt.Printf("%-30s %8d %10d %12d\n", slug, s.cached, s.refreshed, s.unenriched)
	}
	fmt.Printf("\nReview with: diff <(jq '.[].title' %s) <(jq '.[].title' %s)\n", inputPath, outputPath)
	return nil
}

// applyCache merges cached enrichment fields onto a lecture.
// Existing collect-time speakers are preserved if the cache has none.
func applyCache(lec model.Lecture, entry cacheEntry) model.Lecture {
	if entry.EventType != "" {
		lec.EventType = entry.EventType
	}
	if entry.Summary != "" {
		lec.Summary = entry.Summary
	}
	if entry.Description != "" {
		lec.Description = entry.Description
	}
	if len(entry.Speakers) > 0 {
		lec.Speakers = entry.Speakers
	}
	lec.Excluded = entry.Excluded
	if entry.ExcludeReason != "" {
		lec.ExcludeReason = entry.ExcludeReason
	}
	return lec
}

// navBoilerplateMarkers are strings that indicate a description field contains
// website navigation/menu HTML rather than actual event content.
var navBoilerplateMarkers = []string{
	"skip to main content",
	"toggle submenu",
	"toggle hamburger menu",
	"open menu close menu",
	"see what's on or search for an event",
	"lots of interesting events happening",
}

// stripNavBoilerplate returns "" if s looks like scraped navigation HTML rather
// than event-specific text. Returns s unchanged otherwise.
func stripNavBoilerplate(s string) string {
	lower := strings.ToLower(s)
	for _, marker := range navBoilerplateMarkers {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	return s
}

// enrich calls Ollama to enrich a single lecture.
func enrich(lec model.Lecture, host, model string, dryRun bool) (model.Lecture, error) {
	// Use description unless it's nav boilerplate, then fall back to summary.
	rawDesc := stripNavBoilerplate(strings.TrimSpace(lec.Description))
	rawSummary := strings.TrimSpace(lec.Summary)

	// Build the source text block for the prompt.
	// When both are present and different, pass them separately so the model
	// can use whichever (or both) is more informative.
	var sourceText string
	switch {
	case rawDesc != "" && rawSummary != "" && rawDesc != rawSummary:
		sourceText = fmt.Sprintf("  source_summary: %s\n  source_description: %s", rawSummary, rawDesc)
	case rawDesc != "":
		sourceText = fmt.Sprintf("  source_description: %s", rawDesc)
	case rawSummary != "":
		sourceText = fmt.Sprintf("  source_description: %s", rawSummary)
	}

	usefulLen := len(rawDesc)
	if usefulLen < 10 {
		usefulLen = len(rawSummary)
	}
	isThin := usefulLen < 150

	descInstruction := "If source_summary or source_description is already specific and well-written, preserve its essence closely — don't rewrite it into something vaguer. Clean up punctuation and remove hollow openers only."
	if isThin {
		descInstruction = "The source text is thin. Write about the topic's significance and what the audience will learn — not about the speaker's actions. Never pad with 'will speak on this topic', 'will present on', 'will discuss this subject', or any phrase that just restates the title. Write in active voice about the subject matter itself."
	}

	speakerNames := ""
	if len(lec.Speakers) > 0 {
		names := make([]string, 0, len(lec.Speakers))
		for _, sp := range lec.Speakers {
			if sp.Name != "" {
				names = append(names, sp.Name)
			}
		}
		speakerNames = strings.Join(names, ", ")
	}

	speakersLine := ""
	if speakerNames != "" {
		speakersLine = fmt.Sprintf("  speakers: %s\n", speakerNames)
	}
	locationLine := ""
	if lec.Location != "" {
		locationLine = fmt.Sprintf("  location: %s\n", lec.Location)
	}
	freeLine := ""
	if lec.Free {
		freeLine = "  admission: free\n"
	}

	prompt := fmt.Sprintf(`You are a curator for lectures.nz, a New Zealand public lectures website.

lectures.nz lists public lectures, talks, seminars, panels, forums, and similar educational events.
It does NOT list: campus festivals, cultural showcases, open days, orientation events, performances, concerts, markets, fitness classes, graduation ceremonies, or events with no lecture/talk component.
When in doubt, include — but flag clear mismatches.

Given the event below, return ONLY a valid JSON object — no markdown, no explanation.

Fields:
- "event_type": One or two words classifying the event. Choose exactly one: lecture, seminar, panel, workshop, talk, symposium, fireside chat, chat, debate, forum, roundtable, reading, concert, market, ceremony, course, fitness, orientation, festival, open day, conference.
- "exclude": true if this event clearly does not belong on lectures.nz (e.g. campus festival, open day, cultural performance, market, fitness class, ceremony). false if it has any meaningful talk/lecture/seminar component, even if culturally themed.
- "exclude_reason": a short phrase (max 8 words) if exclude is true, otherwise omit.
- "title": The cleaned event title. Strip any speaker name appended after " | " (e.g. "Fast Forward 2026: Transcolonisation! | Chelsea Winstanley" → "Fast Forward 2026: Transcolonisation!"). Also strip trailing speaker credits like " with Jane Smith" or " — featuring Dr X" if the event name is clear without them. Do NOT rewrite, shorten, or rephrase the actual event name itself — only strip the speaker suffix. Return the original if no change needed.
- "summary": One clear sentence (max 180 chars) for the index card. Capture the core topic and speaker if known. No hollow openers ("Join us", "Discover", "Explore"). Do not invent anything not in the source. If source_summary is already good, you may use it directly.
- "description": 2-4 sentences for the detail page. %s Begin with the event's intellectual substance — what question it addresses, what perspective it offers, or (for a named speaker) what they'll argue or present. Never start with what type of event it is ("The lecture", "This talk", "This seminar", "This event"). Never start with "Attendees will". Remove hollow marketing openers ("Join us", "This is a unique opportunity", "Don't miss"). Preserve specific people, institutions, and facts from the source.
- "speakers": Array of {name, bio} objects. Extract from title, speakers field, or description. Return [] if none named. "name": person's full name including honorific prefix — keep "Dr", "Professor", "Sir", "Dame" if present in the source (e.g. "Dr Jane Smith", "Professor John Doe"). Never append role, topic, or parenthetical notes to the name. "bio": their specific role or affiliation as stated in the source, max 6 words (e.g. "Curator Archaeology, Auckland Museum"). Use "" if their role isn't mentioned — never use generic words like "speaker", "presenter", or "expert".

Event:
  host: %s
  title: %s
%s%s%s%s
`, descInstruction, effectiveHost(lec), lec.Title, speakersLine, locationLine, freeLine, sourceText)

	if dryRun {
		fmt.Printf("\n--- DRY RUN: %s ---\n", truncate(lec.Title, 60))
		if len(prompt) > 300 {
			fmt.Println(prompt[:300])
		} else {
			fmt.Println(prompt)
		}
		return lec, nil
	}

	raw, err := ollamaGenerate(host, model, prompt)
	if err != nil {
		return lec, err
	}

	jsonStr := extractJSON(raw)
	var resp enrichResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return lec, fmt.Errorf("parse model response: %w (raw: %s)", err, truncate(raw, 120))
	}

	out := lec
	if resp.EventType != "" {
		out.EventType = resp.EventType
	}
	out.Excluded = resp.Exclude
	if resp.ExcludeReason != "" {
		out.ExcludeReason = resp.ExcludeReason
	}
	// Only apply title if model stripped something (never allow lengthening).
	if t := strings.TrimSpace(resp.Title); t != "" && t != lec.Title && len(t) < len(lec.Title) {
		out.Title = t
	}
	if resp.Summary != "" {
		out.Summary = resp.Summary
	}
	if resp.Description != "" {
		out.Description = resp.Description
	}
	if len(resp.Speakers) > 0 {
		out.Speakers = cleanSpeakers(resp.Speakers)
	}
	return out, nil
}

// effectiveHost returns the organiser name if set, otherwise the host slug.
// This gives Ollama the real institution name rather than the aggregator platform.
func effectiveHost(lec model.Lecture) string {
	if lec.Organiser != "" {
		return lec.Organiser
	}
	return lec.HostSlug
}

// ollamaGenerate calls the Ollama /api/generate endpoint.
func ollamaGenerate(host, mdl, prompt string) (string, error) {
	payload := map[string]any{
		"model":  mdl,
		"prompt": prompt,
		"stream": false,
		"think":  false, // suppress <think> tokens on reasoning models
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Post(
		host+"/api/generate", "application/json", bytes.NewReader(b),
	)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama read body: %w", err)
	}
	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("ollama parse: %w (body: %s)", err, truncate(string(body), 80))
	}
	return strings.TrimSpace(result.Response), nil
}

var (
	thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>`)
	junkRe  = regexp.MustCompile(`^(&[a-z#0-9]+;|[—–\-\s\.,:;!?]+)$`)
)

// extractJSON pulls the first JSON object out of a model response,
// stripping <think> blocks and markdown code fences.
func extractJSON(raw string) string {
	raw = thinkRe.ReplaceAllString(raw, "")
	raw = strings.TrimSpace(raw)

	if strings.Contains(raw, "```") {
		parts := strings.Split(raw, "```")
		if len(parts) >= 3 {
			raw = parts[1]
			raw = strings.TrimPrefix(raw, "json")
			raw = strings.TrimSpace(raw)
		}
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		raw = raw[start : end+1]
	}
	return strings.TrimSpace(raw)
}

// cleanSpeakers drops speakers whose name is an HTML entity, punctuation, or empty.
func cleanSpeakers(speakers []model.Speaker) []model.Speaker {
	out := make([]model.Speaker, 0, len(speakers))
	for _, sp := range speakers {
		name := strings.TrimSpace(sp.Name)
		if name != "" && !junkRe.MatchString(name) {
			out = append(out, sp)
		}
	}
	return out
}

// loadDotEnv reads key=value pairs from path and sets missing env vars.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
