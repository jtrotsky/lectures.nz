.PHONY: collect build serve dev tidy setup post post-dry enrich enrich-dry enrich-force enrich-source enrich-check analytics audit

# Load .env if present (provides OLLAMA_HOST, CF_* etc.)
-include .env
export

# Fetch events from all NZ sources → data/
collect:
	go run ./cmd/collect

# Generate static site → public/
build:
	go run ./cmd/build

# Serve public/ on http://localhost:8080
serve:
	go run ./cmd/serve

# Run collect + build + serve in sequence
dev:
	go run ./cmd/collect && go run ./cmd/build && go run ./cmd/serve

# Fetch/update Go module dependencies (run this first if go.sum is stale)
tidy:
	go mod tidy

# First-time setup: tidy deps then run full dev pipeline
setup: tidy dev

# ---- Enrichment (Ollama) ------------------------------------------------
#
#   make enrich                        normal run   — skips cached lectures
#   make enrich-dry                    dry run      — prints prompts, no Ollama needed (good for testing)
#   make enrich-force                  force        — re-enriches everything, ignores cache
#   make enrich-source SOURCE=meetup   per-source   — re-enriches one host_slug, others stay cached
#   make enrich-check                  ping         — check Ollama is reachable

enrich:
	go run ./cmd/enrich

enrich-dry:
	DRY_RUN=1 go run ./cmd/enrich

enrich-force:
	FORCE_REFRESH=1 go run ./cmd/enrich

enrich-source:
	REFRESH_SOURCE=$(SOURCE) go run ./cmd/enrich

enrich-check:
	curl -sf $(OLLAMA_HOST)/api/tags | python3 -c "import json,sys; m=[x['name'] for x in json.load(sys.stdin)['models']]; print('Ollama reachable — models:', ', '.join(m))"

# ---- Bluesky posting ----------------------------------------------------

# Post new lectures to Bluesky (requires BSKY_HANDLE + BSKY_APP_PASSWORD)
post:
	go run ./cmd/post

# Preview posts without publishing
post-dry:
	DRY_RUN=1 go run ./cmd/post

# ---- Analytics --------------------------------------------------------------
#
#   make analytics           last 30 days
#   make analytics CF_DAYS=7 last 7 days
#
# Requires CF_ACCOUNT_ID and CF_API_TOKEN in .env (gitignored) or env.

analytics:
	go run ./cmd/analytics

# ---- Audit ------------------------------------------------------------------
#
#   make audit                           reads data/lectures-enriched.json
#   make audit FILE=data/lectures.json   reads specified file

audit:
	go run ./cmd/audit $(FILE)
