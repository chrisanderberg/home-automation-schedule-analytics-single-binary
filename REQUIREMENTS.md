# REQUIREMENTS.md

## Purpose
This file tracks the current implementation contract for the project. It may be
partially defined upfront and extended as development proceeds.

## How to interpret this file
- Hard requirements are mandatory. Agents must not violate them unless the
  human explicitly changes or approves an exception.
- Soft requirements are recommended defaults. Agents should follow them unless
  there is a clear, task-specific reason not to.
- Human approval is required before adding, removing, or changing a hard
  requirement.

## Hard requirements
- The system shall be implemented as a single-binary Go application.
- The system shall remain simple to run locally using the documented project
  commands.
- The analytics model shall focus on time-based behavioral modeling rather than
  only basic counts or static summaries.
- Time shall be represented using Monday-based weekly five-minute buckets.
- The aggregate domain model shall support five clock interpretations:
  UTC, local time, mean solar time, apparent solar time, and unequal hours.
- Aggregates shall be keyed by control ID, model ID, and UTC quarter index.
- Local-time bucket splitting shall follow exact DST fold semantics by using
  the earliest UTC instant that changes the local bucket, even when a repeated
  local hour causes bucket order to revisit an earlier wall-clock time.
  Rationale: local bucketing is defined by actual local wall time, not by a
  monotonic approximation in UTC.

## Soft requirements
- Prefer simple designs that keep the import graph and runtime topology easy to
  understand.
- Prefer analytics designs that are compatible with continuous-time Markov
  chain style reasoning about transitions and dwell behavior.
- Prefer schedule estimation or smoothing designs that are compatible with
  KDE-style approaches when they improve the model or user-facing analysis.
- Treat holding intervals and state transitions as first-class analytical
  concepts rather than flattening everything into one generic event stream.
- Keep a clear distinction between stored aggregate richness and any simplified
  projections used by a UI or downstream consumer.
- Treat control metadata such as control type, state cardinality, and state
  labels as part of the analytical contract, not just display decoration.
- Prefer a single HTTP server on one port rather than a multi-port testing
  topology.
  Rationale: standard in-memory tests and `httptest` are sufficient for this
  project and keep the runtime model simpler.
- Prefer JSON API endpoints under `/api/v1/` so API routes can coexist cleanly
  with HTML page routes at the root.
  Rationale: this keeps page routing and API routing distinct without adding
  another process or application boundary.
- Prefer co-locating blob layout, bucketing, solar clocks, and quarter
  handling logic rather than splitting them into many tiny packages unless
  there is a strong reason to do otherwise.
  Rationale: a flatter domain package reduces import graph complexity while
  keeping conceptual boundaries in file layout.
- Prefer per-quarter aggregate updates for holding interval ingestion rather
  than requiring one transaction across all affected quarters.
  Rationale: per-quarter atomicity is sufficient for this project and keeps the
  storage contract simpler.
- Prefer CGO-free SQLite snapshot export approaches such as schema-and-data
  copy over designs that require the SQLite backup API.
  Rationale: staying CGO-free keeps the single-binary build and local setup
  simpler.
- Prefer a default heatmap projection that shows UTC holding totals summed
  across states while keeping richer stored aggregates available for future
  views.
  Rationale: this provides a useful normalized overview without discarding the
  richer per-state, per-clock, and transition data model.
- Prefer vendored client-side dependencies over CDN-hosted ones when the
  application may be deployed on a local network.
  Rationale: CDN access may not be dependable in the target environment.

## Candidate promotions to hard requirements
- None yet.

## Open questions
- None yet.
