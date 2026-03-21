package server

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"home-automation-schedule-analytics-single-bin/internal/config"
	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/bucketing"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/snapshot"
	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/views"
)

//go:embed static/*
var staticFS embed.FS

type Handler struct {
	store     *storage.Store
	ingest    *ingest.Service
	exporter  *snapshot.Exporter
	ownsStore bool
	router    http.Handler
}

func New(db *sql.DB, cfg config.Config) (*Handler, error) {
	var store *storage.Store
	ownsStore := false
	if db != nil {
		store = storage.NewFromDB(db)
		if err := store.Init(context.Background()); err != nil {
			return nil, err
		}
	} else {
		var err error
		store, err = storage.Open(cfg.DBPath)
		if err != nil {
			return nil, err
		}
		ownsStore = true
	}

	engine, err := bucketing.New(bucketing.Config{
		Location:  cfg.Location,
		Latitude:  cfg.Latitude,
		Longitude: cfg.Longitude,
	})
	if err != nil {
		return nil, err
	}
	h := &Handler{
		store:     store,
		ingest:    ingest.NewService(store, engine),
		exporter:  snapshot.NewExporter(store, cfg.DBPath),
		ownsStore: ownsStore,
	}
	h.router, err = h.routes()
	if err != nil {
		if ownsStore {
			_ = store.Close()
		}
		return nil, err
	}
	return h, nil
}

func (h *Handler) Close() error {
	if !h.ownsStore || h.store == nil {
		return nil
	}
	return h.store.Close()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) routes() (http.Handler, error) {
	mux := http.NewServeMux()
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))))
	mux.HandleFunc("GET /api/v1/health", h.handleHealth)
	mux.HandleFunc("POST /api/v1/controls", h.handleCreateControl)
	mux.HandleFunc("POST /api/v1/holding-intervals", h.handleHoldingInterval)
	mux.HandleFunc("POST /api/v1/transitions", h.handleTransition)
	mux.HandleFunc("POST /api/v1/snapshots", h.handleSnapshotAPI)
	mux.HandleFunc("GET /{$}", h.handleHome)
	mux.HandleFunc("GET /controls/{controlID}", h.handleControlPage)
	mux.HandleFunc("GET /controls/{controlID}/heatmap", h.handleHeatmapPanel)
	mux.HandleFunc("GET /snapshots", h.handleSnapshotsPage)
	mux.HandleFunc("POST /snapshots", h.handleSnapshotsCreate)
	return mux, nil
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleCreateControl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ControlID   string `json:"controlId"`
		ControlType string `json:"controlType"`
		NumStates   int    `json:"numStates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	item := control.Control{
		ID:        req.ControlID,
		Type:      control.Type(req.ControlType),
		NumStates: req.NumStates,
	}
	if err := h.ingest.RegisterControl(r.Context(), item); err != nil {
		writeValidation(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) handleHoldingInterval(w http.ResponseWriter, r *http.Request) {
	var req ingest.HoldingInterval
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if err := h.ingest.IngestHolding(r.Context(), req); err != nil {
		writeValidation(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) handleTransition(w http.ResponseWriter, r *http.Request) {
	var req ingest.Transition
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if err := h.ingest.IngestTransition(r.Context(), req); err != nil {
		writeValidation(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) handleSnapshotAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	record, err := h.exporter.Export(r.Context(), req.Name)
	if err != nil {
		writeValidation(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) handleHome(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListControls(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	renderComponent(w, r, http.StatusOK, views.HomePage(views.HomePageData{Controls: items}))
}

func (h *Handler) handleControlPage(w http.ResponseWriter, r *http.Request) {
	controlID := r.PathValue("controlID")
	pageData, err := h.controlPageData(r, controlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, ingest.ErrValidation) {
			writeValidation(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	renderComponent(w, r, http.StatusOK, views.ControlPage(pageData))
}

func (h *Handler) handleHeatmapPanel(w http.ResponseWriter, r *http.Request) {
	controlID := r.PathValue("controlID")
	pageData, err := h.controlPageData(r, controlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, ingest.ErrValidation) {
			writeValidation(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	renderComponent(w, r, http.StatusOK, views.HeatmapPanel(pageData.Heatmap))
}

func (h *Handler) handleSnapshotsPage(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	renderComponent(w, r, http.StatusOK, views.SnapshotsPage(views.SnapshotsPageData{Snapshots: items}))
}

func (h *Handler) handleSnapshotsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := h.exporter.Export(r.Context(), r.FormValue("name")); err != nil {
		writeValidation(w, err)
		return
	}
	items, err := h.store.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		renderComponent(w, r, http.StatusOK, views.SnapshotList(items))
		return
	}
	http.Redirect(w, r, "/snapshots", http.StatusSeeOther)
}

func (h *Handler) controlPageData(r *http.Request, controlID string) (views.ControlPageData, error) {
	c, err := h.store.GetControl(r.Context(), controlID)
	if err != nil {
		return views.ControlPageData{}, err
	}
	quarters, err := h.store.ListQuarterIndices(r.Context(), controlID)
	if err != nil {
		return views.ControlPageData{}, err
	}
	clockName, err := normalizeClock(r.URL.Query().Get("clock"))
	if err != nil {
		return views.ControlPageData{}, err
	}
	metricName, err := normalizeMetric(r.URL.Query().Get("metric"))
	if err != nil {
		return views.ControlPageData{}, err
	}
	heatmap := views.HeatmapData{
		ControlID:      controlID,
		QuarterOptions: quarters,
		Clock:          clockName,
		Metric:         metricName,
	}
	if len(quarters) == 0 {
		return views.ControlPageData{Control: c, Heatmap: heatmap}, nil
	}
	selectedQuarter, err := parseQuarter(defaultString(r.URL.Query().Get("quarter"), strconv.Itoa(quarters[len(quarters)-1])))
	if err != nil {
		selectedQuarter = quarters[len(quarters)-1]
	}
	heatmap.QuarterIndex = selectedQuarter

	record, err := h.store.GetAggregate(r.Context(), controlID, selectedQuarter)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return views.ControlPageData{Control: c, Heatmap: heatmap}, nil
		}
		return views.ControlPageData{}, err
	}
	values, err := buildHeatmapValues(record, heatmap.Clock, heatmap.Metric)
	if err != nil {
		return views.ControlPageData{}, err
	}
	heatmap.Values = values
	heatmap.HasData = true
	return views.ControlPageData{Control: c, Heatmap: heatmap}, nil
}

func buildHeatmapValues(record storage.AggregateRecord, clockName, metric string) ([]uint64, error) {
	acc, err := blob.FromBytes(record.NumStates, record.Data)
	if err != nil {
		return nil, err
	}
	clockIndex, err := parseClock(clockName)
	if err != nil {
		return nil, err
	}
	values := make([]uint64, blob.BucketsPerWeek)
	switch metric {
	case "holding":
		for bucket := 0; bucket < blob.BucketsPerWeek; bucket++ {
			var total uint64
			for state := 0; state < record.NumStates; state++ {
				value, err := acc.Holding(state, clockIndex, bucket)
				if err != nil {
					return nil, err
				}
				total += value
			}
			values[bucket] = total
		}
	case "transition":
		for bucket := 0; bucket < blob.BucketsPerWeek; bucket++ {
			var total uint64
			for from := 0; from < record.NumStates; from++ {
				for to := 0; to < record.NumStates; to++ {
					if from == to {
						continue
					}
					value, err := acc.Transition(from, to, clockIndex, bucket)
					if err != nil {
						return nil, err
					}
					total += value
				}
			}
			values[bucket] = total
		}
	default:
		return nil, fmt.Errorf("unknown metric %q", metric)
	}
	return values, nil
}

func parseClock(name string) (int, error) {
	switch name {
	case "utc":
		return int(bucketing.ClockUTC), nil
	case "local":
		return int(bucketing.ClockLocal), nil
	case "mean_solar":
		return int(bucketing.ClockMeanSolar), nil
	case "apparent_solar":
		return int(bucketing.ClockApparentSolar), nil
	case "unequal_hours":
		return int(bucketing.ClockUnequalHours), nil
	default:
		return 0, fmt.Errorf("unknown clock %q", name)
	}
}

func normalizeClock(value string) (string, error) {
	name := defaultString(value, "utc")
	if _, err := parseClock(name); err != nil {
		return "", fmt.Errorf("%w: invalid clock %q", ingest.ErrValidation, value)
	}
	return name, nil
}

func normalizeMetric(value string) (string, error) {
	name := defaultString(value, "holding")
	switch name {
	case "holding", "transition":
		return name, nil
	default:
		return "", fmt.Errorf("%w: invalid metric %q", ingest.ErrValidation, value)
	}
}

func parseQuarter(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("writeJSON encode error: status=%d err=%v", status, err)
	}
}

func writeValidation(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if !errors.Is(err, ingest.ErrValidation) && !errors.Is(err, snapshot.ErrValidation) {
		status = http.StatusInternalServerError
	}
	writeError(w, status, err)
}

func writeError(w http.ResponseWriter, status int, err error) {
	log.Printf("http error: status=%d err=%v", status, err)
	message := err.Error()
	if status >= 500 {
		message = http.StatusText(http.StatusInternalServerError)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func renderComponent(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("renderComponent error: status=%d method=%s path=%s err=%v", status, r.Method, r.URL.Path, err)
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
