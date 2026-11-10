.PHONY: dev once funnel reclassify test lint build

# Run the poller and API (uses backend/config.yaml if present).
dev:
	cd backend && go run ./cmd/server serve

# Poll every source once and print the funnel.
once:
	cd backend && go run ./cmd/server once

# Print the funnel for the last 7 days; DAYS=30 for a different window.
DAYS ?= 7
funnel:
	cd backend && go run ./cmd/server funnel $(DAYS)

# Re-run the filters over everything collected, after editing the config.
reclassify:
	cd backend && go run ./cmd/server reclassify

test:
	cd backend && go test -race ./...

lint:
	cd backend && gofmt -l . && go vet ./...

build:
	cd backend && go build -o bin/server ./cmd/server
