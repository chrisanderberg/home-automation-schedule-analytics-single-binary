package server

import (
	"database/sql"
	"io/fs"
	"net/http"

	"home-automation-schedule-analytics-single-bin/internal/handler"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
)

// Config bundles the dependencies needed to construct the HTTP server.
type Config struct {
	DB          *sql.DB
	IngestCfg   ingest.Config
	SnapshotDir string
	StaticFS    fs.FS
}

// New wires the API, page, and static routes into a single HTTP handler.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// JSON API
	mux.HandleFunc("GET /api/health", handler.HandleHealth())
	mux.HandleFunc("GET /api/analytics/raw", handler.HandleAnalyticsRaw(cfg.DB))
	mux.HandleFunc("GET /api/analytics/report", handler.HandleAnalyticsReport(cfg.DB))
	mux.HandleFunc("POST /api/controls", handler.HandleControls(cfg.DB))
	mux.HandleFunc("POST /api/holding-intervals", handler.HandleHolding(cfg.DB, cfg.IngestCfg))
	mux.HandleFunc("POST /api/transitions", handler.HandleTransitions(cfg.DB, cfg.IngestCfg))
	mux.HandleFunc("POST /api/snapshots", handler.HandleSnapshots(cfg.DB, cfg.SnapshotDir))

	// HTML pages
	mux.HandleFunc("GET /{$}", handler.HandleHomePage(cfg.DB))
	mux.HandleFunc("GET /controls/new", handler.HandleNewControlPage())
	mux.HandleFunc("POST /controls/new", handler.HandleCreateControl(cfg.DB))
	mux.HandleFunc("GET /controls/{controlID}/analytics", handler.HandleAnalyticsPage(cfg.DB))
	mux.HandleFunc("GET /controls/{controlID}/analytics/raw", handler.HandleRawAnalyticsPage(cfg.DB))
	mux.HandleFunc("GET /controls/{controlID}", handler.HandleControlPage(cfg.DB))
	mux.HandleFunc("POST /controls/{controlID}", handler.HandleUpdateControl(cfg.DB))
	mux.HandleFunc("POST /controls/{controlID}/models/new", handler.HandleCreateModel(cfg.DB))
	mux.HandleFunc("POST /controls/{controlID}/models/{modelID}", handler.HandleUpdateModel(cfg.DB))
	mux.HandleFunc("GET /snapshots", handler.HandleSnapshotPage(cfg.SnapshotDir))
	mux.HandleFunc("GET /partials/heatmap", handler.HandleHeatmapPartial(cfg.DB))

	// Static files
	if cfg.StaticFS != nil {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(cfg.StaticFS))))
	}

	return mux
}
