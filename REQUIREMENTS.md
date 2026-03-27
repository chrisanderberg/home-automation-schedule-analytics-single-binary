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
- Controls in scope shall be explicit user-adjustable home-automation settings
  rather than implicit signals such as occupancy or motion detection.
- Time shall be represented using Monday-based weekly five-minute buckets.
- The aggregate domain model shall support five clock interpretations:
  UTC, local time, mean solar time, apparent solar time, and unequal hours.
- The aggregate domain model shall treat the five clock interpretations as
  parallel views computed for the same underlying behavior, not as
  mutually-exclusive single-clock modes.
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
- Prefer preserving quarter-scoped aggregates as seasonal windows rather than
  collapsing all historical data for a control/model into one undifferentiated
  long-running series.
  Rationale: seasonal differences are analytically meaningful for both civil
  and solar-derived clocks.
- Treat holding intervals and state transitions as first-class analytical
  concepts rather than flattening everything into one generic event stream.
- Prefer preserving per-model aggregates so future analytics can compare
  different models for the same control instead of only exposing fully
  flattened cross-model totals.
  Rationale: cross-model comparison is part of the project's analytical
  direction, even when a UI presents a simplified single-model view.
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
- Prefer blocking UI edits that would invalidate existing aggregate blob shape
  once a control already has aggregate data.
  Rationale: aggregate blobs are keyed to the configured state cardinality, so
  silently allowing incompatible metadata edits would make stored analytics
  unreadable.
- Prefer the UI to be the primary authoring path for control metadata, while
  keeping the JSON API compatible with the same control model and validation.
  Rationale: control configuration is now intended to live in the app UI, but
  the API remains useful for compatibility and automation.
- Prefer existing-control pages to keep configuration editing and control data
  review on the same page instead of splitting them into separate view and edit
  screens.
  Rationale: inline editing keeps the UI flow simpler and establishes a clearer
  pattern for moving additional configuration into the UI over time.
- Prefer model configuration to be managed on the control page alongside
  control configuration, rather than on a separate model-only screen.
  Rationale: models are part of how one control is automated over time, so
  keeping model management attached to that control makes the UI flow clearer.
- Prefer model metadata to be optional configuration layered on top of ingest,
  rather than a prerequisite for writing aggregate data.
  Rationale: controls and their states are the minimum configuration required
  for the system, while model IDs may still be supplied directly by clients at
  ingest time.
- Prefer optional UI model metadata to mean only that the application can keep
  a per-control list of known model IDs or model variants; do not treat that
  metadata as the source of truth for ingest attribution.
  Rationale: in this iteration, model IDs are analytical partitions supplied by
  data and clients, not a separate responsibility-tracking domain object.
- Prefer the model-management UI to surface model IDs inferred from stored
  aggregate data even when explicit model metadata has not been configured yet.
  Rationale: older data may already contain model-specific aggregates, and the
  UI should let users adopt and edit those models without losing visibility.
- Prefer radio-button-style controls to use the `radio buttons` control type
  and slider-style controls to use the `sliders` control type.
  Rationale: the existing model already represents explicit state-based
  controls, so reusing it keeps storage and analytics contracts stable.
- Prefer new two-state radio-button controls to default their labels to `on`
  and `off`, and when additional radio-button states are added, prefer placeholder
  labels `state 3`, `state 4`, and so on unless the user provides something
  more specific.
  Rationale: common radio-button-style controls usually start as an on/off
  toggle, and non-blank defaults make UI configuration faster and clearer.
- Prefer sliders to be UI-configured as a fixed six-state control with
  default labels `min`, `trans 1`, `trans 2`, `trans 3`, `trans 4`, and `max`.
  Rationale: slider analytics already assume the fixed six-state shape, so the
  UI should reflect that contract directly instead of offering an invalid state
  count choice.
- Prefer reserving the literal ID `new` so it cannot be used as a control ID or
  model ID in persisted configuration.
  Rationale: the UI uses `/controls/new` and `/controls/{controlID}/models/new`
  as create routes, so allowing `new` as stored data would collide with page
  routing and make those resources ambiguous.

## Candidate promotions to hard requirements
- None yet.

## Open questions
- None yet.
