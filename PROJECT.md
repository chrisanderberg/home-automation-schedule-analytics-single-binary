# PROJECT.md

## Overview
This project is a single-binary Go application for analyzing home automation
schedule data. It combines ingestion, aggregation, persistence, API endpoints,
and UI delivery in one executable. The analytics layer is centered on
continuous-time behavior, including continuous-time Markov-chain-style modeling
of state transitions and kernel density estimation (KDE)-style smoothing where
useful for understanding schedule preferences.

The domain model is organized around controls, states, model IDs, holding
intervals, and user-initiated transitions. Aggregates are time-based and retain
multiple clock interpretations of the same underlying behavior instead of
collapsing everything into a single wall-clock view.

In this project, a model ID is not just an arbitrary partition key. It
identifies a model-specific analytical partition for a control. Different
models may produce different behavior for the same control, and the analytics
are intended to preserve that distinction so time-in-state and transition
patterns can be compared across models.

## Goals
- Keep the project simple to run and review as a single binary.
- Support iterative development through prototypes and examples instead of
  requiring a complete upfront spec.
- Keep the core analytical direction centered on time-aware transition modeling
  and density-based schedule analysis.
- Keep the project's multi-clock view of time so analysis can compare the same
  behavior across UTC, local, and solar-derived interpretations.
- Capture new constraints and implementation lessons as the project evolves so
  future work stays consistent with what has already been learned.

## Core domain concepts
- Time is bucketed into Monday-based weekly five-minute buckets.
- Controls in scope are explicit user-adjustable home-automation settings,
  rather than implicit signals such as occupancy or motion detection.
- Aggregates retain five clock interpretations for each series:
  UTC, local time, mean solar time, apparent solar time, and unequal hours.
- The five clocks are a parallel analytical view of the same behavior rather
  than mutually exclusive operating modes.
- Aggregates are quarter-scoped using UTC quarter boundaries so seasonal
  variation is preserved instead of collapsed into one year-round series.
- Controls have a control ID, control type, state cardinality, and state labels.
- Analytical data is keyed by control ID, model ID, and quarter index.
- Model IDs partition analytical data so behavior can be retained and compared
  across different automation configurations or model variants for the same
  control instead of flattening everything into one aggregate.
- Holding intervals capture how long a control remains in one state.
- Transitions capture user-initiated moves from one state to another.
- The current UI surfaces a normalized UTC heatmap that sums holding time
  across states, while the stored aggregates retain richer per-state,
  per-clock, and transition data for future analysis.

## Current instruction model
- `AGENTS.md` defines how agents should operate in this worktree.
- `PROJECT.md` defines the project, intent, and current goals.
- `REQUIREMENTS.md` defines the current implementation contract.
- `REQUIREMENTS.md` also carries durable rationale for settled design choices
  when that context helps future implementation.

## How to use this file
- Read this file first to understand project intent and scope.
- Read `REQUIREMENTS.md` next before implementing behavior.
- Use `REQUIREMENTS.md` as the source for both constraints and settled
  tradeoffs.

## Scope guidance
- Treat `PROJECT.md` as the place for stable project description and goals.
- Do not turn this file into a detailed changelog or task tracker.
- Add context here when it helps future agents understand why the project
  exists or what broad direction it is taking.
- Keep domain-defining concepts here when they describe the analytical shape of
  the product, even if the exact implementation details evolve.
