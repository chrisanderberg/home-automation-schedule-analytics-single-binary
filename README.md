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
| `GET` | `/api/v1/analytics` | Read analytics/reporting data as JSON for one control/model/quarter, optionally filtered to one clock |
| `GET` | `/api/v1/analytics/raw` | Read stored per-bucket holdings and transitions without smoothing, damping, or inference |
| `GET` | `/api/v1/analytics/report` | Read parameterized derived analytics with optional intermediate series |
| `POST` | `/api/v1/controls` | Register/update a control |
| `POST` | `/api/v1/holding-intervals` | Ingest a holding interval |
| `POST` | `/api/v1/transitions` | Ingest a user-initiated transition |
| `POST` | `/api/v1/snapshots` | Export a SQLite snapshot |

### Example: register a control

```bash
curl -X POST http://localhost:8080/api/v1/controls \
  -H 'Content-Type: application/json' \
  -d '{"controlId":"light","controlType":"radio buttons","numStates":3,"stateLabels":["off","dim","bright"]}'
```

### Example: ingest a holding interval

```bash
curl -X POST http://localhost:8080/api/v1/holding-intervals \
  -H 'Content-Type: application/json' \
  -d '{"controlId":"light","modelId":"default","state":1,"startTimeMs":1578268800000,"endTimeMs":1578269100000}'
```

### Example: read legacy analytics as JSON

Required query parameters:

- `controlId`
- `modelId`
- `quarter`

Optional query parameter:

- `clock` (`utc`, `local`, `mean-solar`, `apparent-solar`, or `unequal-hours`)

```bash
curl "http://localhost:8080/api/v1/analytics?controlId=living-room-scene&modelId=weekday&quarter=224&clock=utc"
```

If `clock` is omitted, the endpoint returns the full report across all clocks.

### Example: read raw per-bucket analytics

`/api/v1/analytics/raw` returns stored aggregate counters exactly as recorded.
It exposes per-state holding milliseconds and per-transition counts for each
bucket, without smoothing, damping, normalization, or CTMC inference.

```bash
curl "http://localhost:8080/api/v1/analytics/raw?controlId=living-room-scene&modelId=weekday&quarter=224&clock=utc"
```

### Example: read parameterized derived analytics

`/api/v1/analytics/report` returns derived occupancy and inferred-preference
series and accepts explicit report parameters.

Supported query parameters:

- `controlId` (required)
- `modelId` (required)
- `quarter` (required)
- `clock` (optional)
- `smoothing=gaussian|none`
- `kernelRadius=<int>` when `smoothing=gaussian`
- `kernelSigma=<float>` when `smoothing=gaussian`
- `holdingDampingMillis=<float>|none`
- `transitionDampingCount=<float>|none`
- `include=raw,smoothed,rates,diagnostics`

```bash
curl "http://localhost:8080/api/v1/analytics/report?controlId=living-room-scene&modelId=weekday&quarter=224&clock=utc&smoothing=none&holdingDampingMillis=none&transitionDampingCount=none&include=raw,rates"
```

By default, `/api/v1/analytics/report` uses the same smoothing and damping
behavior as the existing `/api/v1/analytics` endpoint. The report response
echoes the parameters applied so harnesses can reproduce the result exactly.

## Pages

| Path | Description |
|---|---|
| `/` | Home — list of registered controls with aggregate counts |
| `/controls/{controlID}` | Control detail with heatmap visualization |
| `/snapshots` | Snapshot management (export + history) |

## Development

```bash
make test       # run all tests
make test-analytics
make test-analytics-golden
make test-ui-parity
make test-analytics-reference
make fmt        # format Go and templ files
make generate   # regenerate templ Go files
make seed-demo  # populate a fresh SQLite db with deterministic demo data
make clean      # remove binary and generated files
make build      # templ generate + go build
```

## Seed demo data

Use the demo seeder to create a fresh SQLite database with a small, deterministic
set of controls, models, holding intervals, and transitions.

```bash
HAA_LATITUDE=37.77 \
HAA_LONGITUDE=-122.42 \
HAA_TIMEZONE=America/Los_Angeles \
HAA_DB_PATH=data/demo.sqlite \
make seed-demo

HAA_LATITUDE=37.77 \
HAA_LONGITUDE=-122.42 \
HAA_TIMEZONE=America/Los_Angeles \
HAA_DB_PATH=data/demo.sqlite \
make run
```

The seeder refuses to run against a non-empty database so repeated runs do not
silently double-count aggregate data.

## Analytics testing

The repository now tests analytics in layers:

- raw aggregate contract checks for `/api/v1/analytics/raw`
- parameterized report contract checks for `/api/v1/analytics/report`
- fixture-driven golden tests under `testdata/analytics/`
- UI/API parity tests for the control page
- an independent Python reference checker in `scripts/check_analytics.py`

### Analytics test commands

```bash
make test-analytics
make test-analytics-golden
make test-ui-parity
make test-analytics-reference
```

### Fixture philosophy

Analytics fixtures live under `testdata/analytics/` and are intentionally tiny:

- prefer 2-state controls first
- prefer only 1-3 nonzero buckets unless a case is explicitly about smoothing
- keep expected behavior hand-checkable

### Independent reference checker

The Python checker recomputes derived report output from raw endpoint data
without using the Go analytics implementation.

Self-test:

```bash
make test-analytics-reference
```

Against captured JSON files:

```bash
python3 scripts/check_analytics.py \
  --raw-file raw.json \
  --report-file report.json
```

Against a running server:

```bash
python3 scripts/check_analytics.py \
  --base-url http://localhost:8080 \
  --control-id living-room-scene \
  --model-id weekday-v1 \
  --quarter 224 \
  --clock utc \
  --smoothing gaussian \
  --kernel-radius 6 \
  --kernel-sigma 3.0
```

## Project structure

```text
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
