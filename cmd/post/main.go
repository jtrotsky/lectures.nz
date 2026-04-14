// Command post publishes new lectures from data/ to a Bluesky account.
//
// It reads lectures-enriched.json (falling back to lectures.json), skips any
// lecture already recorded in data/posted.json, and creates one Bluesky post
// per new lecture.
//
// Required env vars:
//
//	BSKY_HANDLE       e.g. lectures.nz.bsky.social
//	BSKY_APP_PASSWORD app-password from Bluesky Settings → Privacy → App Passwords
//
// Optional:
//
//	DRY_RUN=1   print posts without publishing
//	LIMIT=N     post at most N lectures (default: 5 per run to avoid spam)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

const (
	bskyHost        = "https://bsky.social"
	postedPath      = "data/posted.json"
	defaultLimit    = 3
	defaultDaysAhead = 14
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("post: %v", err)
	}
}

// postedRecord is stored in data/posted.json, keyed by lecture ID.
type postedRecord struct {
	URI       string    `json:"uri"`
	PostedAt  time.Time `json:"posted_at"`
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

	// Find unposted upcoming lectures within the relevance window.
	now := time.Now()
	cutoff := now.AddDate(0, 0, daysAhead)
	var queue []model.Lecture
	for _, l := range lectures {
		if l.TimeStart.Before(now) || l.TimeStart.After(cutoff) {
			continue
		}
		if _, done := posted[l.ID]; done {
			continue
		}
		queue = append(queue, l)
		if len(queue) >= limit {
			break
		}
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
		text, facets, embed := buildPost(l)
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

// buildPost formats a lecture as a Bluesky post (≤ 300 grapheme clusters).
// Returns the post text, rich-text facets (hashtags), and a link card embed.
// The URL is carried in the embed card, not the post text.
func buildPost(l model.Lecture) (string, []map[string]any, map[string]any) {
	nzLoc, _ := time.LoadLocation("Pacific/Auckland")
	if nzLoc == nil {
		nzLoc = time.UTC
	}

	city := shortLocation(l.Location)
	dateLine := l.TimeStart.In(nzLoc).Format("Mon 2 Jan · 3:04pm")
	if city != "" {
		dateLine += " · " + city
	}

	tags := cityHashtags(city)
	if l.Free {
		tags = append(tags, "#FreeEvent")
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

	text := strings.Join(parts, "\n")

	// Trim to 300 grapheme clusters — drop summary first, then tags.
	runes := []rune(text)
	if len(runes) > 300 {
		var short []string
		short = append(short, l.Title)
		short = append(short, dateLine)
		if tagLine != "" {
			short = append(short, tagLine)
		}
		text = strings.Join(short, "\n")
		runes = []rune(text)
		if len(runes) > 300 {
			text = string(runes[:297]) + "..."
		}
	}

	// Build hashtag facets.
	facets := hashtagFacets(text, tags)

	// Build link card embed.
	embed := linkCardEmbed(l.Link, l.Title, l.Summary)

	return text, facets, embed
}

// cityHashtags returns a hashtag for well-known NZ cities extracted from loc.
func cityHashtags(city string) []string {
	known := map[string]string{
		"auckland":        "#Auckland",
		"wellington":      "#Wellington",
		"christchurch":    "#Christchurch",
		"dunedin":         "#Dunedin",
		"hamilton":        "#Hamilton",
		"tauranga":        "#Tauranga",
		"palmerston north": "#PalmerstonNorth",
		"napier":          "#Napier",
		"nelson":          "#Nelson",
	}
	if tag, ok := known[strings.ToLower(strings.TrimSpace(city))]; ok {
		return []string{tag}
	}
	return nil
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

// shortLocation returns the first meaningful part of an address (city or venue).
func shortLocation(loc string) string {
	parts := strings.Split(loc, ",")
	if len(parts) >= 2 {
		// e.g. "University of Auckland, 22 Princes St, Auckland" → "Auckland"
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return strings.TrimSpace(loc)
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
