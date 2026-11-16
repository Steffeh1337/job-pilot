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
| `role` | title looks like a software engineering role, in any language or stack (not sales, design, support, QA, management…) |
| `senior` | title isn't junior/intern/mid-level. **This is a match.** |
| `b2b` | subset of matches that read as contract-style work: B2B, contract, contractor, freelance, independent, short-term or temporary, project-based, hourly/day rates, or a source employment type of Contract/Freelance/Temporary. Boilerplate such as "B2B SaaS", "smart contract" and "employees and contractors" is ignored |

```bash
server funnel 7        # last 7 days, per source, plus why jobs were dropped
server reclassify      # re-run the filters over stored jobs after editing them
```

**The funnel decides eligibility, your CV decides fit.** The filters don't look at technology at all: a Java, Python or Rust role matches like any other, so changing your CV never changes which jobs are in the list. What it changes is the order: once a CV is uploaded the list is sorted by **fit score** (best first by default), and you can filter by minimum fit. Contract/B2B work is a flag and a checkbox filter, never a reason to drop a job.

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

## Tracking applications

Every posting links out to the company's own site, so applying can't be automatic. Open a job, press **Open posting**, apply there, then press **Mark as applied** in the job's panel to keep a record. Nothing is sent anywhere.

- The panel then lets you move it along (*Applied, Interviewing, Offer, Rejected, Withdrawn*), change the date, and keep notes (where you applied, who you spoke to, next steps).
- Jobs you applied to show their status in the list. The **Applied** view is your tracker: everything you applied to, latest first, even jobs a filter would hide. **Hide applied** keeps the main list to jobs you haven't dealt with.
- It is stored in the same local database as everything else (`backend/data/jobpilot.db`) and is synced to the main file on every save, so it survives restarts and power-off. Re-running `reclassify` or refreshing the boards never touches it. **Remove** deletes the record; the job stays.

## CV matching

Upload your CV on the **Profile** page and jobs get a **fit score** (0-100) you can sort and filter by. Everything runs on this computer.

- **Skills match (always on).** Your skills are read from the CV against a vocabulary of ~130 technologies (`backend/internal/skills/vocab.txt`; add your own lines). Each job's skills are read the same way, so the job drawer shows exactly which skills you have and which you lack. Skills marked ★ (used day to day) count more than ones you merely know, and a skill in the job title counts double.
- **Wording similarity (needs Ollama).** A local embedding model compares how close a posting reads to your CV. It is combined with the skills score (60% / 40% by default).
- **A local model reads the CV (needs Ollama).** It drafts your headline, seniority, years and summary. Anything it says is checked against the CV text: a skill or language that isn't actually written there is dropped. You can edit everything.

Without Ollama you still get the skills match; the Profile page shows what is missing and how to add it.

### Privacy

- The CV is parsed in this process. PDFs and DOCX are read in memory and never written to a temp file.
- The text is stored only in your local SQLite database (see *Where your data lives*), and **Delete CV** erases it from the file.
- The only thing it is ever sent to is your own Ollama. The server **refuses to start** if `ollama.url` is not on this machine (set `allow_remote: true` to override, knowingly). The client never follows redirects and never uses a proxy.
- Start Ollama with `OLLAMA_NO_CLOUD=1` so cloud models can never be used.
- Nothing from the CV is logged.

### Where your data lives

Your jobs **and your profile** (the CV text, your skills, and the embeddings) are in one SQLite file, `backend/data/jobpilot.db`. It is git-ignored, and the server logs its absolute path when it starts. Because it is a file on disk, it survives restarts and powering the machine off.

- Saving, editing or deleting the profile is flushed and synced to the main database file straight away, so a power cut right after uploading doesn't lose it.
- Only the extracted text is kept, not the original PDF or DOCX.
- **Delete CV** overwrites the text in the file (SQLite `secure_delete`), it does not just unlink the row.
- To back it up, stop the server and copy the file, or run `sqlite3 backend/data/jobpilot.db ".backup 'backup.db'"`. A backup contains your CV text, so keep it private.
- The path is relative to where the server starts. `make dev` always starts it from `backend/`, so use that.

### Setting up Ollama

```bash
brew install ollama   # installs it but does not start it, and not at login
make ollama           # run when you want CV matching; Ctrl-C stops it
make ollama-pull      # once, in another terminal: ~274 MB wording model + ~4.7 GB CV-reading model
```

`make ollama` keeps it contained: reachable only from this machine, cloud models off (`OLLAMA_NO_CLOUD=1`), one model in memory at a time, and unloaded after two idle minutes, so it doesn't hold on to your RAM.

Then press **Re-index jobs** on the Profile page (and **Re-read CV** if you already uploaded). Models are set under `ollama:` in the config; the scoring weights and the similarity calibration are under `fit:`.

**Calibration.** Raw embedding similarity is never near zero: with the default model it ran about 0.62-0.86 on real jobs, with unrelated postings around 0.71 and engineering ones around 0.79. `semantic_low` and `semantic_high` (defaults 0.72 and 0.82, measured that way) map that range onto 0-100%. The right values depend on the model and on your CV, so once you have uploaded it, run `make calibrate` to measure them on your own data and copy the suggested settings into `config.yaml`.

## API

`POST /api/refresh` polls every source now in the background (the Jobs page's **Refresh** button); `GET /api/refresh` reports progress and what it found. Sources polled in the last 5 minutes are skipped, so repeated clicks are harmless.

`GET /api/health`, `/api/funnel?days=7`, `/api/polls`, `/api/jobs/{id}`, and
`/api/jobs` with `status` (match, rejected, all), `source`, `reason`, `b2b=1`, `unverified=1`, `applied` (1 = only applied, 0 = hide applied), `q`, `days`, `sort` (new, fit), `min_fit`, `limit`, `offset`. With a CV uploaded, jobs carry a `fit` object.

Applications: `PUT /api/jobs/{id}/application` (`status`, `applied_on`, `notes`; an empty body means applied today) and `DELETE /api/jobs/{id}/application`. Jobs carry an `application` object once tracked.

CV matching: `GET /api/profile`, `PUT /api/profile`, `DELETE /api/profile`, `POST /api/profile/cv` (multipart field `cv`), `POST /api/profile/reextract`, `GET /api/fit/status`, `POST /api/fit/sync`. `/api/matches?limit=50` is the older, simpler list.

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
    skills/          skills vocabulary and extractor
    cv/              PDF / DOCX / text extraction
    ollama/          local-only Ollama client
    fit/             pure scoring
    matcher/         profile, indexing and ranking
  testdata/          real API responses, trimmed
web/                 React + Vite + TypeScript dashboard
```
