package server

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"home-automation-schedule-analytics-single-bin/internal/config"
)

func New(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return mux
}
