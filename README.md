# Home Automation Schedule Analytics — Single Binary

Iteration 5 of the home automation schedule analytics project. A single Go
binary that serves both a JSON ingestion API and server-rendered HTML views
for understanding user preferences in automated home environments.

Built with Go, [TEMPL](https://templ.guide), SQLite (via
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)), htmx, and CSS
View Transitions.

## What changed from previous iterations

Previous iterations split aggregation (Go or Python) and reporting (Dagster)
into separate services with cross-process contracts, snapshot workflows, and
Docker orchestration. This iteration consolidates everything into a single Go
binary:

- No Dagster, no Docker, no separate reporting service.
- Aggregation, views, and snapshot export all in one process.
- Standard Go testing replaces the dual-port testing topology.
- Server-rendered HTML replaces the React SPA.

## Prerequisites

- Go 1.22+ (uses method-aware routing in `net/http`)
- [templ CLI](https://templ.guide/quick-start/installation)

## Quick start

```bash
make build
HAA_LATITUDE=59.33 HAA_LONGITUDE=18.07 ./home-automation-schedule-analytics
```

Or without building:

```bash
HAA_LATITUDE=59.33 HAA_LONGITUDE=18.07 make run
```

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `HAA_LATITUDE` | Yes | — | Latitude for solar clock calculations |
| `HAA_LONGITUDE` | Yes | — | Longitude for solar clock calculations |
| `HAA_TIMEZONE` | No | `UTC` | IANA timezone for local clock |
| `HAA_DB_PATH` | No | `data/data.sqlite` | SQLite database path |
| `HAA_PORT` | No | `8080` | HTTP listen port |

## API endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Health check |
| `POST` | `/api/v1/controls` | Register/update a control |
| `POST` | `/api/v1/holding-intervals` | Ingest a holding interval |
| `POST` | `/api/v1/transitions` | Ingest a user-initiated transition |
| `POST` | `/api/v1/snapshots` | Export a SQLite snapshot |

### Example: register a control

```bash
curl -X POST http://localhost:8080/api/v1/controls \
  -H 'Content-Type: application/json' \
  -d '{"controlId":"light","controlType":"discrete","numStates":3,"stateLabels":["off","dim","bright"]}'
```

### Example: ingest a holding interval

```bash
curl -X POST http://localhost:8080/api/v1/holding-intervals \
  -H 'Content-Type: application/json' \
  -d '{"controlId":"light","modelId":"default","state":1,"startTimeMs":1578268800000,"endTimeMs":1578269100000}'
```

## Pages

| Path | Description |
|---|---|
| `/` | Home — list of registered controls with aggregate counts |
| `/controls/{controlID}` | Control detail with heatmap visualization |
| `/snapshots` | Snapshot management (export + history) |

## Development

```bash
make test       # run all tests
make fmt        # format Go and templ files
make generate   # regenerate templ Go files
make clean      # remove binary and generated files
make build      # templ generate + go build
```

## Project structure

```
internal/
  config/     # environment variable parsing
  domain/     # blob index math, bucketing (5 clocks), quarter splitting
  storage/    # SQLite schema, CRUD, concurrent-safe aggregate updates
  ingest/     # holding interval + transition ingestion pipeline
  snapshot/   # SQLite backup export
  handler/    # JSON API handlers + HTML page handlers
  server/     # HTTP router wiring
  view/       # TEMPL templates (layout, home, control, snapshot)
static/
  css/        # dark-themed stylesheet with view transitions
  js/         # vendored htmx + vanilla JS heatmap renderer
```
