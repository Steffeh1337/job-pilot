# job-pilot

A local job-search pipeline. It polls free job APIs, drops everything that doesn't fit, and logs how many listings survive each step so you can see which sources are worth keeping.

**Status: measurement stage.** Fetchers, filters, the funnel report and a review dashboard. There is no scoring, cover-letter or apply flow yet; the plan is to run this for a week first and build on real numbers.

## Quick start

```bash
cp backend/config.example.yaml backend/config.yaml   # optional; defaults work without it
make once    # poll every source once, print the funnel
make dev     # poll on a schedule + serve the API on :8080
make test
```

### Dashboard

```bash
make web-install   # once
make web-build     # then http://127.0.0.1:8080 serves the dashboard from `make dev`
# or, for hot reload while editing the UI:
make web           # Vite on http://localhost:5173, proxying /api to :8080
```

Three views: **Funnel** (per-source survival, and why jobs were dropped), **Jobs** (matches, or the dropped jobs by reason, with full descriptions) and **Sources** (last poll, next poll, errors). Reading the *Dropped* list is how you find filters that are too strict.

Matches flagged **Region unverified** passed only because nothing ruled them out: the source gave no location (RemoteOK never does). Read the description before applying.

Needs Go 1.23+ (and Node 20+ for the dashboard). Data lives in `backend/data/jobpilot.db` (SQLite, no server to run).

## The funnel

Each distinct job is judged once, when first seen, and keeps the furthest stage it reached:

| Stage | Passes when |
|---|---|
| `new` | first time this source+id was seen |
| `unique` | no other source already has the same company+title |
| `region` | remote, and open to someone in Romania (worldwide / Europe / EU / EMEA / Romania) |
| `role` | title looks like an engineering role and isn't sales/design/Java/etc. |
| `stack` | Go, TypeScript, React or Node appears in the title, tags or description |
| `senior` | title isn't junior/intern/mid-level. **This is a match.** |
| `b2b` | subset of matches that read as contract-style work: B2B, contract, contractor, freelance, independent, short-term or temporary, project-based, hourly/day rates, or a source employment type of Contract/Freelance/Temporary. Boilerplate such as "B2B SaaS", "smart contract" and "employees and contractors" is ignored |

```bash
server funnel 7        # last 7 days, per source, plus why jobs were dropped
server reclassify      # re-run the filters over stored jobs after editing them
```

All filters are regular expressions in the `filter:` block of `config.yaml`. Edit, `reclassify`, and compare, with no need to re-fetch.

## Sources

| Source | Interval | Notes |
|---|---|---|
| RemoteOK, Himalayas, We Work Remotely, Jobicy | 30m–1h | Boards refresh hourly at best |
| Remotive | 6h | Asks API users to poll sparingly |
| Arbeitnow | 1h | **Off by default:** 0 of 286 jobs passed the region stage in the first measurement. Enable it under `sources:` to re-check |
| Hacker News | 6h | "Who is hiring?" and "Freelancer? Seeking freelancer?" threads |
| Greenhouse, Lever, Ashby | 1h | Per-company boards listed under `boards:` |

The scheduler wakes every 15 minutes but skips any source polled more recently than its own interval. The Muse is not included: its API sits behind a Cloudflare challenge.

## API

`POST /api/refresh` polls every source now in the background (the Jobs page's **Refresh** button); `GET /api/refresh` reports progress and what it found. Sources polled in the last 5 minutes are skipped, so repeated clicks are harmless.

`GET /api/health`, `/api/funnel?days=7`, `/api/polls`, `/api/jobs/{id}`, and
`/api/jobs` with `status` (match, rejected, all), `source`, `reason`, `b2b=1`, `unverified=1`, `q`, `days`, `limit`, `offset`. `/api/matches?limit=50` is the older, simpler list.

## Layout

```
backend/
  cmd/server/        CLI + HTTP server
  internal/
    domain/          Job, Stage
    source/          one adapter per board
    pipeline/        dedupe + filters + ingest
    scheduler/       per-source polling
    store/           SQLite
    httpapi/         read-only JSON API
    config/          YAML + defaults
  testdata/          real API responses, trimmed
web/                 React + Vite + TypeScript dashboard
```
