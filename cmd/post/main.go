// Command post publishes new lectures from data/ to a Bluesky account.
//
// It reads lectures-enriched.json (falling back to lectures.json), skips any
// lecture already recorded in data/posted.json, scores unposted upcoming
// lectures by quality, and posts the top-scoring one (default: 1 per run).
//
// Required env vars:
//
//	BSKY_HANDLE       e.g. lectures.nz
//	BSKY_APP_PASSWORD app-password from Bluesky Settings → Privacy → App Passwords
//
// Optional:
//
//	DRY_RUN=1   print posts without publishing
//	LIMIT=N     post at most N lectures (default: 1 per run)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

const (
	bskyHost         = "https://bsky.social"
	postedPath       = "data/posted.json"
	defaultLimit     = 1
	defaultDaysAhead = 14
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("post: %v", err)
	}
}

// postedRecord is stored in data/posted.json, keyed by lecture ID.
type postedRecord struct {
	URI      string    `json:"uri"`
	PostedAt time.Time `json:"posted_at"`
}

func run() error {
	handle := os.Getenv("BSKY_HANDLE")
	appPassword := os.Getenv("BSKY_APP_PASSWORD")
	dryRun := os.Getenv("DRY_RUN") == "1"
	limit := defaultLimit
	if v := os.Getenv("LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	daysAhead := defaultDaysAhead
	if v := os.Getenv("DAYS_AHEAD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			daysAhead = n
		}
	}

	if !dryRun && (handle == "" || appPassword == "") {
		return fmt.Errorf("BSKY_HANDLE and BSKY_APP_PASSWORD must be set (or use DRY_RUN=1)")
	}

	// Load lectures.
	lecturesPath := "data/lectures.json"
	if _, err := os.Stat("data/lectures-enriched.json"); err == nil {
		lecturesPath = "data/lectures-enriched.json"
	}
	lectures, err := loadLectures(lecturesPath)
	if err != nil {
		return fmt.Errorf("load lectures: %w", err)
	}

	// Load posted log.
	posted := loadPosted()

	// Find unposted upcoming lectures within the relevance window,
	// ranked by quality score — best first.
	now := time.Now()
	cutoff := now.AddDate(0, 0, daysAhead)
	var candidates []model.Lecture
	for _, l := range lectures {
		if l.TimeStart.Before(now) || l.TimeStart.After(cutoff) {
			continue
		}
		if _, done := posted[l.ID]; done {
			continue
		}
		candidates = append(candidates, l)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return score(candidates[i], now) > score(candidates[j], now)
	})
	var queue []model.Lecture
	for _, l := range candidates {
		queue = append(queue, l)
		if len(queue) >= limit {
			break
		}
	}
	if len(candidates) > 0 {
		log.Printf("Scored %d candidates; top score=%d (%q)", len(candidates), score(candidates[0], now), candidates[0].Title)
	}

	if len(queue) == 0 {
		log.Println("Nothing new to post.")
		return nil
	}

	// Authenticate.
	var accessJWT, did string
	if !dryRun {
		accessJWT, did, err = createSession(handle, appPassword)
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		log.Printf("Authenticated as %s (%s)", handle, did)
	}

	// Post each lecture.
	for _, l := range queue {
		// Resolve source Bluesky handle → DID for mention facet (no auth needed).
		var m *mention
		if bskyHandle, ok := sourceHandles[l.HostSlug]; ok {
			did, err := resolveHandle(bskyHandle)
			if err != nil {
				log.Printf("WARN: could not resolve @%s: %v", bskyHandle, err)
			} else {
				m = &mention{Handle: bskyHandle, DID: did}
			}
		}

		text, facets, embed := buildPost(l, m)
		if dryRun {
			fmt.Printf("\n--- DRY RUN ---\n%s\n(%d chars)\n", text, len([]rune(text)))
			continue
		}

		uri, err := createPost(accessJWT, did, text, facets, embed)
		if err != nil {
			log.Printf("WARN: failed to post %q: %v", l.Title, err)
			continue
		}
		posted[l.ID] = postedRecord{URI: uri, PostedAt: time.Now()}
		log.Printf("Posted: %s → %s", l.Title, uri)

		// Be polite — don't hammer the API.
		time.Sleep(500 * time.Millisecond)
	}

	if !dryRun {
		savePosted(posted)
	}
	return nil
}

// mention holds a resolved Bluesky account for source tagging.
type mention struct {
	Handle string // e.g. "aucklanduni.bsky.social"
	DID    string // e.g. "did:plc:..."
}

// sourceHandles maps host slugs to their official Bluesky handles.
// Add more here as accounts are confirmed.
var sourceHandles = map[string]string{
	"auckland":        "aucklanduni.bsky.social",
	"otago":           "universityofotago.bsky.social",
	"auckland-museum": "aucklandmuseum.bsky.social",
	"royal-society":   "royalsocietynz.bsky.social",
	"nziia":           "nziia.bsky.social",
}

// buildPost formats a lecture as a Bluesky post (≤ 300 grapheme clusters).
// Returns the post text, rich-text facets (hashtags + optional mention), and a link card embed.
// The URL is carried in the embed card, not the post text.
func buildPost(l model.Lecture, m *mention) (string, []map[string]any, map[string]any) {
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	venue := hostDisplayName[l.HostSlug]
	if venue == "" {
		venue = cityFromLocation(l.Location)
	}
	dateLine := l.TimeStart.In(nzLoc).Format("Mon 2 Jan · 3:04pm")
	if venue != "" {
		dateLine += " · " + venue
	}

	city := cityFromLocation(l.Location)
	tags := cityHashtags(city)
	if l.Free {
		tags = append(tags, "#FreeEvent")
	}
	if tag, ok := eventTypeHashtag[l.EventType]; ok {
		tags = append(tags, tag)
	}
	tagLine := strings.Join(tags, " ")

	var parts []string
	parts = append(parts, l.Title)
	parts = append(parts, dateLine)
	if l.Summary != "" {
		parts = append(parts, l.Summary)
	}
	if tagLine != "" {
		parts = append(parts, tagLine)
	}
	if m != nil {
		parts = append(parts, "@"+m.Handle)
	}

	text := strings.Join(parts, "\n")

	// Trim to 300 grapheme clusters — drop summary first, then tags, keep mention.
	runes := []rune(text)
	if len(runes) > 300 {
		var short []string
		short = append(short, l.Title)
		short = append(short, dateLine)
		if tagLine != "" {
			short = append(short, tagLine)
		}
		if m != nil {
			short = append(short, "@"+m.Handle)
		}
		text = strings.Join(short, "\n")
		runes = []rune(text)
		if len(runes) > 300 {
			text = string(runes[:297]) + "..."
		}
	}

	// Build facets: hashtags + mention.
	facets := hashtagFacets(text, tags)
	if m != nil {
		if f := mentionFacet(text, "@"+m.Handle, m.DID); f != nil {
			facets = append(facets, f)
		}
	}

	// Link to the lectures.nz detail page (has add-to-calendar, full description).
	listingURL := "https://lectures.nz/" + l.HostSlug + "/" + l.ID

	// Build link card embed.
	embed := linkCardEmbed(listingURL, l.Title, l.Summary)

	return text, facets, embed
}

// eventTypeHashtag maps known Ollama-assigned event types to their Bluesky hashtag.
// Only types in this map are hashtagged — unknown or excluded types are silently dropped.
var eventTypeHashtag = map[string]string{
	"lecture":       "#Lecture",
	"seminar":       "#Seminar",
	"panel":         "#Panel",
	"workshop":      "#Workshop",
	"talk":          "#Talk",
	"symposium":     "#Symposium",
	"fireside chat": "#FiresideChat",
	"chat":          "#Chat",
	"debate":        "#Debate",
	"forum":         "#Forum",
	"roundtable":    "#Roundtable",
	"reading":       "#Reading",
}

// cityHashtags returns a hashtag for well-known NZ cities extracted from loc.
func cityHashtags(city string) []string {
	known := map[string]string{
		"auckland":         "#Auckland",
		"wellington":       "#Wellington",
		"christchurch":     "#Christchurch",
		"dunedin":          "#Dunedin",
		"hamilton":         "#Hamilton",
		"tauranga":         "#Tauranga",
		"palmerston north": "#PalmerstonNorth",
		"napier":           "#Napier",
		"nelson":           "#Nelson",
	}
	if tag, ok := known[strings.ToLower(strings.TrimSpace(city))]; ok {
		return []string{tag}
	}
	return nil
}

// mentionFacet builds an ATproto rich-text facet for a @mention in text.
func mentionFacet(text, atHandle, did string) map[string]any {
	idx := strings.Index(text, atHandle)
	if idx < 0 {
		return nil
	}
	byteStart := len([]byte(text[:idx]))
	byteEnd := byteStart + len([]byte(atHandle))
	return map[string]any{
		"$type": "app.bsky.richtext.facet",
		"index": map[string]any{
			"byteStart": byteStart,
			"byteEnd":   byteEnd,
		},
		"features": []map[string]any{
			{
				"$type": "app.bsky.richtext.facet#mention",
				"did":   did,
			},
		},
	}
}

// resolveHandle resolves a Bluesky handle to a DID via the ATproto identity API.
// No authentication required.
func resolveHandle(handle string) (string, error) {
	url := bskyHost + "/xrpc/com.atproto.identity.resolveHandle?handle=" + handle
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		DID string `json:"did"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	return result.DID, nil
}

// hashtagFacets builds ATproto rich-text facets for each #Tag found in text.
func hashtagFacets(text string, tags []string) []map[string]any {
	var facets []map[string]any
	for _, tag := range tags {
		idx := strings.Index(text, tag)
		if idx < 0 {
			continue
		}
		byteStart := len([]byte(text[:idx]))
		byteEnd := byteStart + len([]byte(tag))
		facets = append(facets, map[string]any{
			"$type": "app.bsky.richtext.facet",
			"index": map[string]any{
				"byteStart": byteStart,
				"byteEnd":   byteEnd,
			},
			"features": []map[string]any{
				{
					"$type": "app.bsky.richtext.facet#tag",
					"tag":   strings.TrimPrefix(tag, "#"),
				},
			},
		})
	}
	return facets
}

// linkCardEmbed builds an app.bsky.embed.external record for a link preview card.
func linkCardEmbed(uri, title, description string) map[string]any {
	return map[string]any{
		"$type": "app.bsky.embed.external",
		"external": map[string]any{
			"uri":         uri,
			"title":       title,
			"description": description,
		},
	}
}

// score ranks a lecture by posting quality. Higher = more worth posting.
//
// Points are awarded for:
//   - Source tier: universities (+3), research/policy orgs (+2), museums/cultural (+1)
//   - Has named speaker(s) (+2)
//   - Has a description longer than 50 chars (+1)
//   - Free event (+1)
//   - Has a physical location — not online-only (+1)
//   - Sweet-spot timing: 3–10 days out (+1)
//   - High-quality event type (lecture or seminar) (+1)
func score(l model.Lecture, now time.Time) int {
	s := 0

	// Source tier.
	switch l.HostSlug {
	case "auckland", "aut", "otago", "victoria", "canterbury", "massey":
		s += 3
	case "royal-society", "rbnz", "nziia", "motu", "nz-initiative":
		s += 2
	case "te-papa", "auckland-museum", "auckland-art-gallery", "artgallery-nz",
		"national-library", "public-record", "studio-one", "gus-fisher", "motat",
		"ockham", "artspace":
		s += 1
	}

	// Speaker quality signal.
	if len(l.Speakers) > 0 {
		s += 2
	}

	// Description richness.
	if len(l.Description) > 50 {
		s += 1
	}

	// Free event.
	if l.Free {
		s += 1
	}

	// Physical location (not online-only).
	loc := strings.ToLower(l.Location)
	if l.Location != "" && !strings.Contains(loc, "online") && !strings.Contains(loc, "zoom") {
		s += 1
	}

	// Sweet-spot timing: 3–10 days out.
	days := l.TimeStart.Sub(now).Hours() / 24
	if days >= 3 && days <= 10 {
		s += 1
	}

	// Event type quality.
	switch l.EventType {
	case "lecture", "seminar":
		s += 1
	}

	return s
}

// hostDisplayName maps a host slug to a short display name for the post dateline.
var hostDisplayName = map[string]string{
	"auckland":             "University of Auckland",
	"aut":                  "Auckland University of Technology",
	"canterbury":           "University of Canterbury",
	"massey":               "Massey University",
	"otago":                "University of Otago",
	"victoria":             "Victoria University of Wellington",
	"te-papa":              "Te Papa",
	"auckland-museum":      "Auckland Museum",
	"auckland-art-gallery": "Auckland Art Gallery",
	"artgallery-nz":        "Art Gallery NZ",
	"gus-fisher":           "Gus Fisher Gallery",
	"motat":                "MOTAT",
	"studio-one":           "Studio One Toi Tū",
	"national-library":     "National Library",
	"public-record":        "Public Record",
	"royal-society":        "Royal Society",
	"nziia":                "NZ Institute of International Affairs",
	"rbnz":                 "Reserve Bank of NZ",
	"motu":                 "Motu Research",
	"nz-initiative":        "NZ Initiative",
	"ockham":               "Ockham Book Awards",
	"artspace":             "Artspace Aotearoa",
	"meetup":               "Meetup Auckland",
	"eventbrite":           "Eventbrite NZ",
}

// cityFromLocation extracts the city from a location string (last comma-separated part).
// Used for hashtag generation only.
func cityFromLocation(loc string) string {
	parts := strings.Split(loc, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// --- ATproto XRPC calls ---

func createSession(handle, password string) (accessJWT, did string, err error) {
	body, _ := json.Marshal(map[string]string{
		"identifier": handle,
		"password":   password,
	})
	resp, err := xrpcPost(bskyHost+"/xrpc/com.atproto.server.createSession", "", body)
	if err != nil {
		return "", "", err
	}
	var result struct {
		AccessJwt string `json:"accessJwt"`
		DID       string `json:"did"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", "", fmt.Errorf("parse session: %w", err)
	}
	return result.AccessJwt, result.DID, nil
}

func createPost(accessJWT, did, text string, facets []map[string]any, embed map[string]any) (string, error) {
	record := map[string]any{
		"$type":     "app.bsky.feed.post",
		"text":      text,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if len(facets) > 0 {
		record["facets"] = facets
	}
	if embed != nil {
		record["embed"] = embed
	}

	body, _ := json.Marshal(map[string]any{
		"repo":       did,
		"collection": "app.bsky.feed.post",
		"record":     record,
	})
	resp, err := xrpcPost(bskyHost+"/xrpc/com.atproto.repo.createRecord", accessJWT, body)
	if err != nil {
		return "", err
	}
	var result struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse post response: %w", err)
	}
	return result.URI, nil
}

func xrpcPost(url, accessJWT string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if accessJWT != "" {
		req.Header.Set("Authorization", "Bearer "+accessJWT)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// --- Posted log ---

func loadPosted() map[string]postedRecord {
	data, err := os.ReadFile(postedPath)
	if err != nil {
		return make(map[string]postedRecord)
	}
	var m map[string]postedRecord
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]postedRecord)
	}
	return m
}

func savePosted(m map[string]postedRecord) {
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(postedPath, data, 0644); err != nil {
		log.Printf("WARN: save posted log: %v", err)
	}
}

// --- Data loading ---

func loadLectures(path string) ([]model.Lecture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lectures []model.Lecture
	return lectures, json.Unmarshal(data, &lectures)
}
