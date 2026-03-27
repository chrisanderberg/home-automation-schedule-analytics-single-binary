package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/analytics"
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

// controlRequest is the JSON payload accepted by the control upsert endpoint.
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
		existing, err := storage.GetControl(r.Context(), db, control.ControlID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			err = storage.SaveControl(r.Context(), db, "", control)
		case err != nil:
			log.Printf("handleControls get control failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		default:
			if lockErr := rejectStructuralChange(existing, control); lockErr != "" {
				writeError(w, http.StatusBadRequest, lockErr)
				return
			}
			err = storage.SaveControl(r.Context(), db, existing.ControlID, control)
		}
		if err != nil {
			status := http.StatusInternalServerError
			message := "internal server error"
			switch {
			case errors.Is(err, storage.ErrConflict):
				status = http.StatusBadRequest
				message = "control ID already exists"
			case errors.Is(err, storage.ErrStructureLocked):
				status = http.StatusBadRequest
				message = "cannot change control structure after aggregate data has been recorded"
			}
			log.Printf("handleControls save failed: %v", err)
			writeError(w, status, message)
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

// HandleAnalytics returns structured analytics for one control/model/quarter, optionally filtered to one clock.
func HandleAnalytics(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, control, ok := loadAnalyticsInput(w, r, db, "handleAnalytics")
		if !ok {
			return
		}

		report, err := analytics.BuildReport(r.Context(), db, control, input.modelID, input.quarterIndex)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "analytics not found")
				return
			}
			log.Printf("handleAnalytics build report failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if input.clock == "" {
			writeJSON(w, http.StatusOK, report)
			return
		}
		selectedClock, err := report.ClockBySlug(input.clock)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid clock")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"controlId":    report.ControlID,
			"modelId":      report.ModelID,
			"quarterIndex": report.QuarterIndex,
			"quarterLabel": report.QuarterLabel,
			"clock":        selectedClock,
		})
	}
}

// HandleAnalyticsRaw returns the stored per-bucket holdings and transitions without derived processing.
func HandleAnalyticsRaw(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, control, ok := loadAnalyticsInput(w, r, db, "handleAnalyticsRaw")
		if !ok {
			return
		}

		report, err := analytics.BuildRawReport(r.Context(), db, control, input.modelID, input.quarterIndex)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "analytics not found")
				return
			}
			log.Printf("handleAnalyticsRaw build report failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if input.clock == "" {
			writeJSON(w, http.StatusOK, report)
			return
		}
		selectedClock, err := report.ClockBySlug(input.clock)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid clock")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"controlId":    report.ControlID,
			"modelId":      report.ModelID,
			"quarterIndex": report.QuarterIndex,
			"quarterLabel": report.QuarterLabel,
			"numStates":    report.NumStates,
			"stateLabels":  report.StateLabels,
			"clock":        selectedClock,
		})
	}
}

// HandleAnalyticsReport returns a parameterized derived analytics report.
func HandleAnalyticsReport(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, control, ok := loadAnalyticsInput(w, r, db, "handleAnalyticsReport")
		if !ok {
			return
		}
		opts, err := parseReportOptions(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		report, err := analytics.BuildDerivedReport(r.Context(), db, control, input.modelID, input.quarterIndex, opts)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "analytics not found")
				return
			}
			log.Printf("handleAnalyticsReport build report failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if input.clock == "" {
			writeJSON(w, http.StatusOK, report)
			return
		}
		selectedClock, err := report.ClockBySlug(input.clock)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid clock")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"controlId":    report.ControlID,
			"modelId":      report.ModelID,
			"quarterIndex": report.QuarterIndex,
			"quarterLabel": report.QuarterLabel,
			"numStates":    report.NumStates,
			"stateLabels":  report.StateLabels,
			"parameters":   report.Parameters,
			"clock":        selectedClock,
		})
	}
}

type analyticsRequest struct {
	controlID    string
	modelID      string
	quarterIndex int
	clock        string
}

func loadAnalyticsInput(w http.ResponseWriter, r *http.Request, db *sql.DB, logLabel string) (analyticsRequest, storage.Control, bool) {
	input, err := parseAnalyticsRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return analyticsRequest{}, storage.Control{}, false
	}
	control, err := storage.GetControl(r.Context(), db, input.controlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "control not found")
			return analyticsRequest{}, storage.Control{}, false
		}
		log.Printf("%s get control failed: %v", logLabel, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return analyticsRequest{}, storage.Control{}, false
	}
	return input, control, true
}

func parseAnalyticsRequest(r *http.Request) (analyticsRequest, error) {
	controlID := r.URL.Query().Get("controlId")
	modelID := r.URL.Query().Get("modelId")
	quarterStr := r.URL.Query().Get("quarter")
	if controlID == "" || modelID == "" || quarterStr == "" {
		return analyticsRequest{}, fmt.Errorf("missing required query params")
	}
	quarterIndex, err := strconv.Atoi(quarterStr)
	if err != nil {
		return analyticsRequest{}, fmt.Errorf("invalid quarter")
	}
	return analyticsRequest{
		controlID:    controlID,
		modelID:      modelID,
		quarterIndex: quarterIndex,
		clock:        r.URL.Query().Get("clock"),
	}, nil
}

func parseReportOptions(r *http.Request) (analytics.ReportOptions, error) {
	opts := analytics.DefaultReportOptions()
	query := r.URL.Query()

	if smoothing := query.Get("smoothing"); smoothing != "" {
		switch smoothing {
		case analytics.SmoothingGaussian:
			opts.SmoothingKind = smoothing
		case analytics.SmoothingNone:
			opts.SmoothingKind = smoothing
			opts.KernelRadius = 0
			opts.KernelSigma = 0
		default:
			return analytics.ReportOptions{}, fmt.Errorf("invalid smoothing")
		}
	}
	if radiusStr := query.Get("kernelRadius"); radiusStr != "" {
		radius, err := strconv.Atoi(radiusStr)
		if err != nil {
			return analytics.ReportOptions{}, fmt.Errorf("invalid kernel radius")
		}
		opts.KernelRadius = radius
	}
	if sigmaStr := query.Get("kernelSigma"); sigmaStr != "" {
		sigma, err := strconv.ParseFloat(sigmaStr, 64)
		if err != nil {
			return analytics.ReportOptions{}, fmt.Errorf("invalid kernel sigma")
		}
		opts.KernelSigma = sigma
	}
	if holdingStr := query.Get("holdingDampingMillis"); holdingStr != "" {
		if holdingStr == "none" {
			opts.HoldingDampingMillis = 0
		} else {
			value, err := strconv.ParseFloat(holdingStr, 64)
			if err != nil {
				return analytics.ReportOptions{}, fmt.Errorf("invalid holding damping")
			}
			opts.HoldingDampingMillis = value
		}
	}
	if transitionStr := query.Get("transitionDampingCount"); transitionStr != "" {
		if transitionStr == "none" {
			opts.TransitionDampingCount = 0
		} else {
			value, err := strconv.ParseFloat(transitionStr, 64)
			if err != nil {
				return analytics.ReportOptions{}, fmt.Errorf("invalid transition damping")
			}
			opts.TransitionDampingCount = value
		}
	}
	for _, includeValue := range query["include"] {
		for _, token := range strings.Split(includeValue, ",") {
			switch strings.TrimSpace(token) {
			case "":
				continue
			case "raw":
				opts.Include.Raw = true
			case "smoothed":
				opts.Include.Smoothed = true
			case "rates":
				opts.Include.Rates = true
			case "diagnostics":
				opts.Include.Diagnostics = true
			default:
				return analytics.ReportOptions{}, fmt.Errorf("invalid include")
			}
		}
	}
	if opts.SmoothingKind == analytics.SmoothingNone && (query.Get("kernelRadius") != "" || query.Get("kernelSigma") != "") {
		return analytics.ReportOptions{}, fmt.Errorf("kernel parameters are only valid with gaussian smoothing")
	}
	if opts.SmoothingKind == analytics.SmoothingGaussian {
		if opts.KernelRadius < 0 {
			return analytics.ReportOptions{}, fmt.Errorf("invalid kernel radius")
		}
		if opts.KernelSigma <= 0 {
			return analytics.ReportOptions{}, fmt.Errorf("invalid kernel sigma")
		}
	}
	if opts.HoldingDampingMillis < 0 {
		return analytics.ReportOptions{}, fmt.Errorf("invalid holding damping")
	}
	if opts.TransitionDampingCount < 0 {
		return analytics.ReportOptions{}, fmt.Errorf("invalid transition damping")
	}
	return opts, nil
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
