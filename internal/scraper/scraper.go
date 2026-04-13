package scraper

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

// Scraper is the interface all source scrapers must implement.
type Scraper interface {
	Host() model.Host
	Scrape(ctx context.Context) ([]model.Lecture, error)
}

// DefaultClient is the shared HTTP client for all scrapers.
var DefaultClient = &http.Client{
	Timeout: 30 * time.Second,
}

// UserAgent is sent with all outbound requests.
const UserAgent = "Mozilla/5.0 (compatible; lectures.nz/1.0; +https://lectures.nz)"

// Fetch performs an HTTP GET with retry logic (3 attempts, exponential backoff).
func Fetch(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-NZ,en;q=0.9")

		resp, err := DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("attempt %d: server error %d", attempt+1, resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("client error %d for %s", resp.StatusCode, url)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d reading body: %w", attempt+1, err)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all attempts failed for %s: %w", url, lastErr)
}

// FetchDetail fetches an event detail page and returns the main description
// text extracted from JSON-LD, OpenGraph, or the first substantial paragraph.
// Returns an empty string (not an error) when nothing useful is found.
func FetchDetail(ctx context.Context, url string) (string, error) {
	body, err := Fetch(ctx, url)
	if err != nil {
		return "", err
	}
	text := ExtractDescription(body)
	if LooksLikeGarbage(text) {
		return "", nil
	}
	return text, nil
}

// MakeID generates a stable MD5-based ID from a URL.
func MakeID(url string) string {
	h := md5.Sum([]byte(url))
	return fmt.Sprintf("%x", h[:8])
}
