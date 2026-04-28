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
	EventType   string          `json:"event_type,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Description string          `json:"description,omitempty"`
	Speakers    []model.Speaker `json:"speakers,omitempty"`
}

// enrichResponse is what we expect back from Ollama (parsed from the JSON the model emits).
type enrichResponse struct {
	EventType   string          `json:"event_type"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Speakers    []model.Speaker `json:"speakers"`
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
				EventType:   result.EventType,
				Summary:     result.Summary,
				Description: result.Description,
				Speakers:    result.Speakers,
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
	return lec
}

// enrich calls Ollama to enrich a single lecture.
func enrich(lec model.Lecture, host, model string, dryRun bool) (model.Lecture, error) {
	desc := lec.Description
	if desc == "" {
		desc = lec.Summary
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

	isThin := len(strings.TrimSpace(desc)) < 150
	expansionInstruction := "Preserve the existing text closely — only clean up punctuation and remove hollow openers."
	if isThin {
		expansionInstruction = "Expand this — the source text is very short, so infer reasonable context from the title and host, but do not invent specific claims."
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

Given the event below, return ONLY a valid JSON object — no markdown, no explanation.

Fields:
- "event_type": One or two words classifying the event. Choose exactly one: lecture, seminar, panel, workshop, talk, symposium, fireside chat, chat, debate, forum, roundtable, reading, concert, market, ceremony, course, fitness, orientation, conference.
- "title": The cleaned event title. Strip any speaker name appended after " | " (e.g. "Fast Forward 2026: Transcolonisation! | Chelsea Winstanley" → "Fast Forward 2026: Transcolonisation!"). Also strip trailing speaker credits like " with Jane Smith" or " — featuring Dr X" if the event name is clear without them. Do NOT rewrite, shorten, or rephrase the actual event name itself — only strip the speaker suffix. Return the original if no change needed.
- "summary": One clear sentence (max 180 chars) for the index card. Capture the core topic and speaker if named. No hollow openers like "Join us" or "Discover". Do not invent anything not in the source.
- "description": 2-4 sentences for the detail page. Preserve the source's voice, key facts, people, and institutions. Remove hollow openers. Fix punctuation. %s
- "speakers": Array of speaker objects, each with "name" (string) and "bio" (string). Extract from title, speakers, or description. Return [] if none named. The "name" field must contain ONLY the person's name and honorific/title if given (e.g. "Dr Jane Smith" or "Professor John Doe") — never append event context, parenthetical notes, or topic references to the name. The "bio" field is a short role or affiliation, max 6 words (e.g. "former NZ diplomat", "Victoria University economist", "award-winning novelist"). Do not write a full sentence. Use "" if no bio information is available.

Event:
  host: %s
  title: %s
%s%s%s  description: %s
`, expansionInstruction, effectiveHost(lec), lec.Title, speakersLine, locationLine, freeLine, desc)

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
