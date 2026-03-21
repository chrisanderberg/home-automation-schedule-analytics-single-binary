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

## 2026-03-20 — Solar clock calculation model
- Mean solar time is computed as UTC shifted by longitude at 4 minutes per
  degree.
- Apparent solar time is computed as mean solar time plus the NOAA-style
  equation of time approximation.
- Unequal hours use local civil dates for sunrise/sunset lookup, approximate
  sunrise/sunset from the same solar model, and split daytime and nighttime
  into 12 equal variable-length hours each.
- Rationale: the domain spec requires all five clocks but does not prescribe an
  astronomical algorithm. This keeps the implementation deterministic and
  reviewable without adding an external solar library.

## 2026-03-20 — Slider control registration
- Slider controls must be registered with `numStates=6`; any other value is
  rejected.
- Rationale: the spec states slider controls are discretized to 6 states. The
  API keeps `numStates` explicit rather than silently rewriting user input.

## 2026-03-20 — Snapshot storage location
- Exported snapshot files are written under `data/snapshots/` by default, or
  alongside the configured database path in a sibling `snapshots/` directory.
- Snapshot filenames use UTC timestamps plus a sanitized snapshot name.
- Rationale: the spec requires snapshot export and HTML management views but
  does not define a storage location.
