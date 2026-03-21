<!-- Single-Binary Go + TEMPL + SQLite — Domain Spec -->

# Schedule Analytics — Single-Binary Spec

This document adapts the project-independent domain spec for the single-binary
Go + TEMPL + SQLite implementation (iteration 5).

## Architecture

A single Go binary serves both the JSON ingestion API and server-rendered HTML
views. There is no separate reporting service, no Dagster, and no Docker.

- **Aggregation:** JSON REST API for ingesting holding intervals and transitions.
- **Storage:** SQLite with WAL mode via `modernc.org/sqlite` (CGO-free).
- **Views:** TEMPL-compiled server-rendered HTML with htmx partial updates and
  CSS View Transitions for smooth navigation.
- **Analytics (future):** KDE smoothing and CTMC stationary distributions
  computed in-process, results rendered via TEMPL views.

## Domain (unchanged from previous iterations)

### Controls
- Discrete controls: 2–10 states.
- Slider controls: discretized to 6 states.
- Each control has a unique `controlId`, a `controlType`, and `numStates`.

### Five clocks (computed in parallel)
1. UTC
2. Local time
3. Mean solar time
4. Apparent solar time
5. Unequal hours

### Time-of-week bucketing
- 5-minute buckets, 288/day, 2016/week.
- Week starts Monday (day index 0).
- Bucket index: `dayIndex * 288 + bucketWithinDay`.

### Measurement semantics
- **Holding time:** elapsed milliseconds in a state, split across overlapped
  buckets per clock. Half-open interval `[startTimeMs, endTimeMs)`.
- **Transitions:** user-initiated state changes, counted in the bucket
  containing the timestamp per clock. Self-transitions rejected.
- **Local-time DST folds:** when local wall time repeats during fall-back, local
  bucketing follows the repeated wall clock exactly, so bucket indices may move
  backward from one local bucket to an earlier local-hour bucket.

### Dense blob layout
- B=2016, C=5, G=10080, N=numStates.
- Total values per blob: N² × G.
- `holdIndex(s, c, b) = (s × G) + (c × B) + b`
- `transGroupIndex(from, to) = from × (N-1) + offsetWithinFromBlock(from, to)`
- `transIndex(from, to, c, b) = (N × G) + (transGroupIndex(from, to) × G) + (c × B) + b`
- All values stored as little-endian u64.

### Quarter windows
- UTC calendar quarters: Q1 Jan–Mar, Q2 Apr–Jun, Q3 Jul–Sep, Q4 Oct–Dec.
- `quarterIndex = (utcYear - 1970) * 4 + (quarterNumber - 1)`.
- Intervals crossing quarter boundaries are split.

### Data quality
- Invalid or incomplete data is discarded, not ingested.

## What is NOT in this implementation
- Dagster or any external orchestrator.
- Docker or container deployment.
- Separate reporting/analytics service.
- Authentication (local/home-network only).
- Data retention / quarter rollover policy (future).
