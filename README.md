# Home Automation Schedule Analytics — Single Binary

Iteration 5 of the home automation schedule analytics project. A single Go
binary that serves both a JSON ingestion API and server-rendered HTML views
for understanding user preferences in automated home environments.

## Prerequisites

- Go 1.24+
- [templ CLI](https://templ.guide/quick-start/installation)

## Quick start

```bash
make build
HAA_LATITUDE=59.33 HAA_LONGITUDE=18.07 ./home-automation-schedule-analytics
```

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `HAA_LATITUDE` | Yes | — | Latitude for solar clock calculations |
| `HAA_LONGITUDE` | Yes | — | Longitude for solar clock calculations |
| `HAA_TIMEZONE` | No | `UTC` | IANA timezone for local clock |
| `HAA_DB_PATH` | No | `data/data.sqlite` | SQLite database path |
| `HAA_PORT` | No | `8080` | HTTP listen port |

Snapshot exports are written under a `snapshots/` directory adjacent to the
configured database file.

## API endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Health check |
| `POST` | `/api/v1/controls` | Register/update a control |
| `POST` | `/api/v1/holding-intervals` | Ingest a holding interval |
| `POST` | `/api/v1/transitions` | Ingest a transition |
| `POST` | `/api/v1/snapshots` | Export a SQLite snapshot |

## Pages

| Path | Description |
|---|---|
| `/` | Home — list of controls with stats |
| `/controls/{controlID}` | Control detail with heatmap filters for quarter, clock, and metric |
| `/snapshots` | Snapshot management |

## Request payloads

Register or update a control:

```json
{
  "controlId": "kitchen-light",
  "controlType": "discrete",
  "numStates": 3
}
```

Ingest a holding interval:

```json
{
  "controlId": "kitchen-light",
  "state": 2,
  "startTimeMs": 1774017600000,
  "endTimeMs": 1774017900000
}
```

Ingest a transition:

```json
{
  "controlId": "kitchen-light",
  "fromState": 0,
  "toState": 1,
  "timestampMs": 1774018200000
}
```

Create a snapshot:

```json
{
  "name": "q1-export"
}
```

## Notes

- Slider controls must be registered with `numStates=6`.
- The UI uses server-rendered HTML with TEMPL components, htmx partial updates,
  and lightweight canvas rendering for the heatmap.
- `htmx` is vendored under `internal/server/static/vendor/` and embedded into
  the single binary at build time.
- Solar clocks use an in-process approximation documented in `DECISIONS.md`.

## Development

```bash
make test      # run all tests
make fmt       # format Go and templ files
make generate  # regenerate templ files
make clean     # remove binary and generated files
```
