.PHONY: collect build serve dev tidy setup post post-dry

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

# Post new lectures to Bluesky (requires BSKY_HANDLE + BSKY_APP_PASSWORD)
post:
	go run ./cmd/post

# Preview posts without publishing
post-dry:
	DRY_RUN=1 go run ./cmd/post
