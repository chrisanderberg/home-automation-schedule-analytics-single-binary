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
