# DECISIONS.md

## 2026-03-18 — Module path
- Module path set to `home-automation-schedule-analytics-single-bin`.
- Matches directory name. May change if a canonical VCS import path is desired.

## 2026-03-18 — Single port, no testing API
- Single HTTP server on one port (default 8080).
- No dual-port testing topology. Tests use in-memory SQLite and httptest directly.
- Rationale: the dual-port design existed to support Dagster-driven external
  validation. Without Dagster, standard Go testing is simpler and sufficient.

## 2026-03-18 — API prefix /api/v1/
- JSON API endpoints live under `/api/v1/` to coexist with HTML page routes
  at the root.
- Previous iterations used `/v1/` directly since there were no HTML routes.

## 2026-03-18 — Flat domain package
- Blob, bucketing, solar, and quarter logic merged into a single `internal/domain`
  package instead of separate packages.
- Rationale: reduces import graph complexity for a single-binary project while
  keeping related domain concepts co-located. The previous iteration's
  multi-package separation is preserved in file boundaries.

## 2026-03-18 — Per-quarter UpdateAggregate (no multi-quarter transaction)
- Holding interval ingestion uses `UpdateAggregate` per quarter span instead
  of wrapping all quarters in a single transaction.
- Rationale: for a single-user hobby project, per-quarter atomicity is sufficient.
  Eliminates the need for `UpdateAggregateTx` in the storage layer.

## 2026-03-18 — CGO-free SQLite snapshot via schema+data copy
- Snapshot export recreates schema and copies table data row by row rather than
  using the SQLite backup API (which requires CGO).
- Rationale: `modernc.org/sqlite` is CGO-free. The copy approach is simple,
  correct for small databases, and avoids any CGO dependency.

## 2026-03-18 — Heatmap shows UTC holding totals by default
- The heatmap visualization sums holding times across all states for the UTC
  clock to produce a 2016-element array.
- Rationale: provides a useful overview. Per-state and per-clock views can be
  added as future enhancements.

## 2026-03-18 — htmx vendored, not CDN
- htmx.min.js is vendored in `static/js/` rather than loaded from a CDN.
- Rationale: local-network deployment means the CDN may not be reachable.
