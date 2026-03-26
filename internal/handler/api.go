package handler

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/snapshot"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

const snapshotExportTimeout = 30 * time.Second

// HandleHealth returns a minimal liveness endpoint.
func HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type controlRequest struct {
	ControlID   string   `json:"controlId"`
	ControlType string   `json:"controlType"`
	NumStates   int      `json:"numStates"`
	StateLabels []string `json:"stateLabels"`
}

// HandleControls validates and upserts control metadata from the API.
func HandleControls(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req controlRequest
		if err := decodeStrictJSON(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		control, errMsg := validateControlInput(controlInput{
			ControlID:   req.ControlID,
			ControlType: req.ControlType,
			NumStates:   req.NumStates,
			StateLabels: req.StateLabels,
		})
		if errMsg != "" {
			writeError(w, http.StatusBadRequest, errMsg)
			return
		}
		if err := storage.UpsertControl(r.Context(), db, control); err != nil {
			log.Printf("handleControls upsert failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// HandleHolding accepts holding-interval ingest requests.
func HandleHolding(db *sql.DB, cfg ingest.Config) http.HandlerFunc {
	return makeIngestHandler(db, cfg, "handleHolding", ingest.IngestHolding)
}

// HandleTransitions accepts transition ingest requests.
func HandleTransitions(db *sql.DB, cfg ingest.Config) http.HandlerFunc {
	return makeIngestHandler(db, cfg, "handleTransitions", ingest.IngestTransition)
}

// makeIngestHandler shares JSON decoding and error mapping across ingest endpoints.
func makeIngestHandler[T any](
	db *sql.DB,
	cfg ingest.Config,
	logLabel string,
	ingestFn func(context.Context, *sql.DB, ingest.Config, T) error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input T
		if err := decodeStrictJSON(r.Body, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := ingestFn(r.Context(), db, cfg, input); err != nil {
			if ingest.IsValidationError(err) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			log.Printf("%s ingest failed: %v", logLabel, err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// HandleSnapshots exports a snapshot file and returns its filename.
func HandleSnapshots(db *sql.DB, snapshotDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), snapshotExportTimeout)
		defer cancel()

		path, err := snapshot.Export(ctx, db, snapshotDir)
		if err != nil {
			log.Printf("handleSnapshots export failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"snapshotFilename": filepath.Base(path)})
	}
}
