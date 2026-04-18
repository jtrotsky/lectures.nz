// Command analytics prints a Cloudflare Web Analytics summary to stdout.
//
// Required env vars:
//
//	CF_ACCOUNT_ID   Cloudflare account ID
//	CF_API_TOKEN    Cloudflare API token with Account Analytics: Read permission
//
// Optional:
//
//	CF_DAYS=N   days to look back (default: 30)
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
)

const cfGraphQL = "https://api.cloudflare.com/client/v4/graphql"

// defaultSiteTag is the lectures.nz CF Web Analytics site tag.
// Override with CF_SITE_TAG env var.
const defaultSiteTag = "e5bd352daca74ac1bf35ee577666b44d"

// group is one row from a rumPageloadEventsAdaptiveGroups result.
type group struct {
	Count      int `json:"count"`
	Dimensions struct {
		RequestPath string `json:"requestPath"`
		RefererHost string `json:"refererHost"`
		CountryName string `json:"countryName"`
		DeviceType  string `json:"deviceType"`
	} `json:"dimensions"`
}

type cfResponse struct {
	Data struct {
		Viewer struct {
			Accounts []cfAccount `json:"accounts"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type cfAccount struct {
	TopPaths  []group `json:"topPaths"`
	Referrers []group `json:"referrers"`
	Countries []group `json:"countries"`
	Devices   []group `json:"devices"`
}

const gqlQuery = `
query($accountTag: string, $siteTag: string, $start: Date, $end: Date) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      topPaths: rumPageloadEventsAdaptiveGroups(
        filter: {AND: [{siteTag: $siteTag}, {date_geq: $start}, {date_leq: $end}]}
        limit: 100
        orderBy: [count_DESC]
      ) {
        count
        dimensions { requestPath }
      }
      referrers: rumPageloadEventsAdaptiveGroups(
        filter: {AND: [{siteTag: $siteTag}, {date_geq: $start}, {date_leq: $end}]}
        limit: 20
        orderBy: [count_DESC]
      ) {
        count
        dimensions { refererHost }
      }
      countries: rumPageloadEventsAdaptiveGroups(
        filter: {AND: [{siteTag: $siteTag}, {date_geq: $start}, {date_leq: $end}]}
        limit: 15
        orderBy: [count_DESC]
      ) {
        count
        dimensions { countryName }
      }
      devices: rumPageloadEventsAdaptiveGroups(
        filter: {AND: [{siteTag: $siteTag}, {date_geq: $start}, {date_leq: $end}]}
        limit: 5
        orderBy: [count_DESC]
      ) {
        count
        dimensions { deviceType }
      }
    }
  }
}`

func main() {
	accountID := os.Getenv("CF_ACCOUNT_ID")
	apiToken := os.Getenv("CF_API_TOKEN")
	if accountID == "" || apiToken == "" {
		log.Fatal("CF_ACCOUNT_ID and CF_API_TOKEN must be set")
	}
	siteTag := os.Getenv("CF_SITE_TAG")
	if siteTag == "" {
		siteTag = defaultSiteTag
	}

	days := 30
	if d := os.Getenv("CF_DAYS"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}

	now := time.Now().UTC()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -days).Format("2006-01-02")

	fmt.Printf("\nlectures.nz — last %d days (%s → %s)\n", days, startDate, endDate)
	fmt.Println(strings.Repeat("─", 60))

	accounts, err := fetchStats(apiToken, accountID, siteTag, startDate, endDate)
	if err != nil {
		log.Fatalf("fetch: %v", err)
	}
	if len(accounts) == 0 {
		log.Fatal("no account data returned — check CF_ACCOUNT_ID")
	}

	acc := accounts[0]
	printPageviews(acc.TopPaths)
	printSection("Top pages", acc.TopPaths, func(g group) string { return g.Dimensions.RequestPath }, 15)
	printSection("Referrers", filterEmpty(acc.Referrers, func(g group) string { return g.Dimensions.RefererHost }), func(g group) string { return g.Dimensions.RefererHost }, 10)
	printSection("Countries", acc.Countries, func(g group) string { return g.Dimensions.CountryName }, 10)
	printSection("Devices", acc.Devices, func(g group) string { return g.Dimensions.DeviceType }, 5)
	fmt.Println()
}

func fetchStats(token, accountID, siteTag, start, end string) ([]cfAccount, error) {
	payload := map[string]any{
		"query": gqlQuery,
		"variables": map[string]string{
			"accountTag": accountID,
			"siteTag":    siteTag,
			"start":      start,
			"end":        end,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", cfGraphQL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var r cfResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, raw)
	}
	if len(r.Errors) > 0 {
		msgs := make([]string, len(r.Errors))
		for i, e := range r.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	return r.Data.Viewer.Accounts, nil
}

func printPageviews(paths []group) {
	total := 0
	for _, g := range paths {
		total += g.Count
	}
	fmt.Printf("\nPageviews (top 100 paths)  %s\n", formatInt(total))
}

func printSection(title string, groups []group, label func(group) string, max int) {
	if len(groups) == 0 {
		return
	}

	// Sort by count desc (may already be sorted, but referrers may have been filtered).
	sorted := make([]group, len(groups))
	copy(sorted, groups)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

	total := 0
	for _, g := range sorted {
		total += g.Count
	}

	fmt.Printf("\n%s\n", title)
	limit := max
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for _, g := range sorted[:limit] {
		name := label(g)
		if name == "" {
			name = "(direct)"
		}
		pct := 0
		if total > 0 {
			pct = g.Count * 100 / total
		}
		fmt.Printf("  %-40s %5s  %3d%%\n", truncate(name, 40), formatInt(g.Count), pct)
	}
}

func filterEmpty(groups []group, label func(group) string) []group {
	out := make([]group, 0, len(groups))
	for _, g := range groups {
		if label(g) != "" {
			out = append(out, g)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func formatInt(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	start := len(s) % 3
	if start == 0 {
		start = 3
	}
	b.WriteString(s[:start])
	for i := start; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
