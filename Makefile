.PHONY: dev once funnel reclassify test lint build web web-install web-build test-web

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

# Dashboard. `make web` runs Vite with hot reload on :5173 and proxies /api to
# the backend on :8080 (start that with `make dev`). `make web-build` produces
# web/dist, which `make dev` then serves itself at http://127.0.0.1:8080.
web-install:
	cd web && npm install

web:
	cd web && npm run dev

web-build:
	cd web && npm run build

test-web:
	cd web && npm run typecheck && npm test

build:
	cd backend && go build -o bin/server ./cmd/server
