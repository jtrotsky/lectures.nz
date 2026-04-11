# lectures.nz — Claude context

## What this project is
A Go static site that scrapes NZ public lectures/events, builds a website, and deploys to Cloudflare Pages via GitHub Actions. See https://lectures.nz.

## Architecture
```
cmd/collect/   — scrapes all sources → data/lectures.json + data/hosts.json
cmd/build/     — reads data/ → generates public/ (static HTML)
cmd/serve/     — local dev server
cmd/calendar/  — iCal feed generator
internal/model/         — Lecture and Host structs
internal/scraper/       — shared Fetch(), MakeID(), CleanTitle(), TruncateSummary()
internal/scraper/sources/<name>/scraper.go  — one file per source (16 sources)
internal/topics/        — tag inference + IsExcluded() exclusion list
```

## Key model fields (internal/model/model.go)
`Lecture`: ID, Title, Link, TimeStart, TimeEnd, Summary, SummaryHTML, Free, Cost, Location, Image, Speakers []Speaker, Tags []string, HostSlug
`Speaker`: Name, Bio

## Scraper conventions
- Implement `scraper.Scraper` interface: `Host() model.Host` + `Scrape(ctx) ([]Lecture, error)`
- Use `scraper.Fetch(ctx, url)` for HTML pages (sets browser headers)
- For JSON APIs use a dedicated http client with `Authorization: Bearer` + `Accept: application/json` (see eventbrite scraper)
- Use `scraper.MakeID(url)`, `scraper.CleanTitle(s)`, `scraper.TruncateSummary(s, n)`
- Return nil,nil (not error) when a source is intentionally skipped (e.g. missing token)

## Deployment
- GitHub Actions: `.github/workflows/deploy.yml` — runs collect + build + deploy on push to main and daily at 6am NZST (18:00 UTC)
- Deploys to Cloudflare Pages (project: lectures-nz)
- Secrets needed: CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, EVENTBRITE_SECRET

## Enrichment
- `scripts/enrich.py` — reads data/lectures.json, calls Ollama, writes data/lectures-enriched.json
- Set OLLAMA_HOST to your Ollama instance (default: http://localhost:11434)
- Run on Windows PC (free GPU inference): `python scripts/enrich.py`

## Content curation principles
- Free events preferred; paid OK if clearly educational
- In-person only (no online-only events)
- Real lectures/talks/seminars — not concerts, teen workshops, activity days, book launches
- NZ only

## Common commands
```bash
go run ./cmd/collect    # scrape all sources → data/
go run ./cmd/build      # build static site → public/
go run ./cmd/serve      # local dev server
go test ./...           # run tests
python scripts/audit.py # coverage stats by source (no Claude needed)
```

## Sources (16 active)
auckland, aut, victoria, otago, canterbury, auckland-museum, auckland-art-gallery,
artgallery-nz, te-papa, national-library, gus-fisher, ockham, motat,
studio-one, public-record, eventbrite

## What NOT to ask Claude to do
- Run `go run ./cmd/collect` — GH Actions owns this; run it yourself with `!`
- Re-explain project structure — it's all here
- Text enrichment at scale — use `scripts/enrich.py` + Ollama instead
