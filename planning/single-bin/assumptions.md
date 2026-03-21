# Assumptions — Single-Binary Implementation

## Deployment
- Local machine or home network only.
- No public hosting, no production scaling.
- Authentication omitted on trusted local networks.

## Architecture
- Single Go binary serves both JSON API and HTML views on one port.
- No separate reporting service; aggregation and views coexist in one process.
- No Dagster; orchestration replaced by in-process logic.
- No Docker; single static binary, no containerization needed.

## Storage
- SQLite via `modernc.org/sqlite` (pure Go, CGO-free).
- WAL mode for concurrent reads.
- Dense blobs stored as native SQLite BLOBs.

## Views
- TEMPL for type-safe server-rendered HTML.
- htmx for partial page updates without a full SPA.
- CSS View Transitions API for smooth page navigation.
- Lightweight vanilla JS for heatmap visualization (canvas-based).

## Testing
- Standard Go testing with in-memory SQLite and `httptest`.
- No testing API endpoints; the dual-port topology from previous iterations
  is replaced by direct function calls in tests.

## Scope
- Aggregation pipeline: ingestion, bucketing, storage, snapshot export.
- Server-rendered views: control listing, per-control detail with heatmap,
  snapshot management.
- Analytics (KDE, CTMC, stationary distributions) are future work.
- All five clocks are implemented in the domain layer and wired into ingest.

## Tooling
- Go 1.24.
- `github.com/a-h/templ` for HTML templates.
- `modernc.org/sqlite` for SQLite.
