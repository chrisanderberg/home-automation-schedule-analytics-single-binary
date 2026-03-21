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
| `/controls/{controlID}` | Control detail with heatmap |
| `/snapshots` | Snapshot management |

## Development

```bash
make test      # run all tests
make fmt       # format Go and templ files
make generate  # regenerate templ files
make clean     # remove binary and generated files
```
