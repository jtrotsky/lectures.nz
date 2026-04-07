.PHONY: collect build serve dev tidy setup

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
