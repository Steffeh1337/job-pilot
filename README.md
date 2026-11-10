# job-pilot

A local job-search pipeline. It polls free job APIs, drops everything that doesn't fit, and logs how many listings survive each step so you can see which sources are worth keeping.

**Status: stage 1, measurement only.** Fetchers, filters and the funnel report. There is no scoring, cover-letter or apply flow yet; the plan is to run this for a week first and build on real numbers.

## Quick start

```bash
cp backend/config.example.yaml backend/config.yaml   # optional; defaults work without it
make once    # poll every source once, print the funnel
make dev     # poll on a schedule + serve the API on :8080
make test
```

Needs Go 1.23+. Data lives in `backend/data/jobpilot.db` (SQLite, no server to run).

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
| `b2b` | subset of matches that mention B2B / freelance / contract work |

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
| Arbeitnow | 1h | Mostly German on-site jobs |
| Hacker News | 6h | "Who is hiring?" and "Freelancer? Seeking freelancer?" threads |
| Greenhouse, Lever, Ashby | 1h | Per-company boards listed under `boards:` |

The scheduler wakes every 15 minutes but skips any source polled more recently than its own interval. The Muse is not included: its API sits behind a Cloudflare challenge.

## API

`GET /api/health`, `/api/funnel?days=7`, `/api/matches?limit=50`, `/api/polls`

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
```
