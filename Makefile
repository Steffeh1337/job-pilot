.PHONY: dev once funnel reclassify test lint build web web-install web-build test-web ollama ollama-pull calibrate

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

# Local Ollama for CV matching, kept contained: only this machine can reach it,
# cloud models are off, one model is held in memory, and it is unloaded after
# two idle minutes. It is not started at login; run this when you want it, and
# stop it with Ctrl-C.
ollama:
	OLLAMA_HOST=127.0.0.1:11434 OLLAMA_NO_CLOUD=1 OLLAMA_KEEP_ALIVE=2m OLLAMA_MAX_LOADED_MODELS=1 OLLAMA_NUM_PARALLEL=1 ollama serve

# One-time model downloads (about 274 MB and 4.7 GB). Needs `make ollama` running.
ollama-pull:
	OLLAMA_HOST=127.0.0.1:11434 ollama pull nomic-embed-text
	OLLAMA_HOST=127.0.0.1:11434 ollama pull qwen2.5:7b

# Measure the wording-similarity scale on your own CV and jobs, and print the
# fit settings to use. Needs a CV uploaded and Ollama running.
calibrate:
	cd backend && go run ./cmd/server calibrate
