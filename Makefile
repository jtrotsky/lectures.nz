.PHONY: collect build serve dev tidy setup post post-dry enrich enrich-dry enrich-force enrich-source enrich-check

OLLAMA := http://100.74.102.54:11434

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
	OLLAMA_HOST=$(OLLAMA) python3 scripts/enrich.py

enrich-dry:
	DRY_RUN=1 python3 scripts/enrich.py

enrich-force:
	FORCE_REFRESH=1 OLLAMA_HOST=$(OLLAMA) python3 scripts/enrich.py

enrich-source:
	REFRESH_SOURCE=$(SOURCE) OLLAMA_HOST=$(OLLAMA) python3 scripts/enrich.py

enrich-check:
	curl -sf $(OLLAMA)/api/tags | python3 -c "import json,sys; m=[x['name'] for x in json.load(sys.stdin)['models']]; print('Ollama reachable — models:', ', '.join(m))"

# ---- Bluesky posting ----------------------------------------------------

# Post new lectures to Bluesky (requires BSKY_HANDLE + BSKY_APP_PASSWORD)
post:
	go run ./cmd/post

# Preview posts without publishing
post-dry:
	DRY_RUN=1 go run ./cmd/post
