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
- The primary reported preference output shall be the stationary distribution
  of an inferred CTMC rather than raw observed holding time alone.
- Controls in scope shall be explicit user-adjustable home-automation settings
  rather than implicit signals such as occupancy or motion detection.
- Time shall be represented using Monday-based weekly five-minute buckets.
- The aggregate domain model shall support five clock interpretations:
  UTC, local time, mean solar time, apparent solar time, and unequal hours.
- The aggregate domain model shall treat the five clock interpretations as
  parallel views computed for the same underlying behavior, not as
  mutually-exclusive single-clock modes.
- Aggregates shall be keyed by control ID, model ID, and UTC quarter index.
- CTMC inference shall be defined per `(control ID, model ID, UTC quarter
  index, clock, bucket)` before any derived cross-model comparison or
  aggregation is performed.
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
- Prefer documenting the analytics model explicitly in terms of the project's
  core hypothesis:
  raw holding time mostly reflects model behavior, while user-initiated
  transitions provide corrective evidence about user preference.
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
- Prefer KDE to be defined as cyclic smoothing along the time-of-week axis,
  applied independently per control, model, and clock before CTMC estimation.
  Rationale: sparse 5-minute weekly buckets need nearby time buckets to inform
  one another before transition-rate inference is stable.
- Prefer KDE and damping to be the primary sparse-data handling mechanisms for
  analytics and reporting rather than leaving sparse behavior undefined.
  Rationale: sparse data is expected in many weekly buckets, and the intended
  model already relies on smoothing and damping rather than ad hoc fallbacks.
- Prefer CTMC construction to use smoothed holding times and smoothed
  transition statistics as its immediate inputs.
- Prefer reporting to treat actual holding-time heatmaps as diagnostic views,
  while stationary distributions are the main preference-oriented outputs.
- Prefer reporting and UI terminology to distinguish clearly between actual
  occupancy and inferred preference.
- Prefer cross-model comparison views to be derived from per-model inference,
  not to replace per-model CTMC estimation as the primary analytical unit.
- Prefer reporting to support comparison across the five clocks as alternative
  time coordinate systems for the same underlying behavior.
- Prefer reporting to make it possible to compare:
  per-model actual occupancy,
  per-model inferred preference,
  cross-model preference convergence,
  and clock-by-clock preference differences.
- Prefer comments to describe what a declaration, test, or complex code section
  is responsible for, while leaving the code itself to communicate how it works.
  Keep those comments concise and add them when they reduce reader cognitive
  load rather than restating obvious mechanics.
- Prefer adding inline comments around non-obvious invariants, edge-case
  handling, fallback behavior, domain-model translations, and data-shape
  transitions when those details would otherwise force the reader to reverse
  engineer intent from dense code.
  Rationale: those are the points where future readers are most likely to stop
  and ask "why is this here?" even if the mechanics are already visible.
- Prefer the stationary distribution to be interpreted as the expected
  long-run time allocation of the inferred CTMC and therefore the best
  preference estimate for a given analytical slice.
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
- Prefer structured analytics read endpoints under `/api/v1/analytics` for
  retrieving one `(control, model, quarter, clock)` analytical slice as JSON.
  Rationale: read-side analytics should be available to UI and automation
  clients without introducing a separate reporting service.
- Prefer exposing raw analytics reads separately from derived reporting reads,
  so test harnesses can inspect stored per-bucket holdings and transitions
  without conflating them with smoothing, damping, or CTMC inference.
  Rationale: keeping raw and derived reads distinct makes analytical
  correctness easier to validate stage by stage.
- Prefer derived analytics reads to echo the smoothing and damping parameters
  applied to the response.
  Rationale: harnesses and automation clients need reproducible report
  metadata, not just the final derived series.
- Prefer derived analytics reads to support explicit bypasses for smoothing and
  damping rather than requiring clients to infer that a zero-valued parameter
  disables a transform.
  Rationale: explicit `none`-style controls make tests clearer and avoid
  overloading numeric parameters with hidden semantics.
- Prefer derived analytics reads to optionally expose intermediate raw,
  smoothed, and rate series for harness validation without making them part of
  every default payload.
  Rationale: intermediates are useful for analytical verification but would add
  unnecessary payload size to the common case.
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
- Prefer control pages that default their analytics selection to a model that
  actually has aggregate data when one exists.
  Rationale: choosing a data-backed model by default avoids landing on an empty
  analytics view when the control already has recorded aggregates.
- Prefer read-side analytics to be computed directly from the stored
  per-quarter aggregate blobs rather than requiring a second materialized
  analytics table.
  Rationale: the aggregate blob is already the source of truth for holdings and
  transitions, so deriving reports on read keeps the storage model simpler.
- Prefer control pages to report one `(control, model, quarter, clock)` slice
  at a time, with selectors for model, quarter, and clock, plus side-by-side
  actual occupancy and inferred-preference views for each state.
  Rationale: this keeps the UI aligned with the analytical unit defined by the
  model while still making actual occupancy and inferred preference easy to
  compare.
- Prefer control-page analytics selectors to preserve the same report
  parameters the API supports, including analytics mode, smoothing, damping,
  and optional intermediate-series toggles.
  Rationale: keeping the UI URL model aligned with the API makes ad hoc visual
  inspection and automated harness testing comparable.
- Prefer the control page to render raw and derived analytics as separate
  panels rather than forcing one view model to collapse both concepts.
  Rationale: raw per-bucket holdings/transitions and derived preference reports
  answer different questions, so separate panels keep each view simpler.
- Prefer analytics tests to use tiny deterministic fixtures with only a few
  populated buckets unless a case is specifically about broader weekly shapes.
  Rationale: small fixtures keep expected raw and derived behavior reviewable
  and make analytical regressions easier to localize.
- Prefer analytics harnesses to validate the pipeline in layers:
  raw stored counters,
  parameterized intermediates,
  and final derived distributions.
  Rationale: stage-by-stage assertions make it clear whether a failure belongs
  to storage, smoothing, damping, or CTMC inference.
- Prefer at least one analytics reference checker that recomputes report output
  independently from the raw endpoint rather than only reusing application
  logic in tests.
  Rationale: an independent implementation is the strongest guard against
  coherent but incorrect production/report code.
- Prefer weekly analytics charts to treat the 2016 Monday-based five-minute
  buckets as one continuous x-axis with day-boundary markers, rather than
  splitting weekday and time-of-day into separate plot axes by default.
  Rationale: the stored series are fundamentally one-dimensional weekly bucket
  sequences, and a single weekly axis makes totals, spikes, and transitions
  easier to compare directly.
- Prefer raw holding totals and raw transition totals to render as weekly bar
  charts aggregated across states or transition pairs before exposing per-state
  or per-pair drill-down charts.
  Rationale: aggregate diagnostic charts show overall activity patterns first
  and reduce the visual noise of many small per-series views.
- Prefer derived actual occupancy and inferred preference to render as stacked
  weekly bar charts with stable state colors shared across related charts.
  Rationale: stacked weekly bars preserve the full 2016-bucket timeline while
  making state-allocation comparisons readable and keeping the stationary
  distribution visually distinct as the primary preference view.

## Candidate promotions to hard requirements
- None yet.

## Open questions
- None yet.
