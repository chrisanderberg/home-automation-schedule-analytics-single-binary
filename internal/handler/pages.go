package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/snapshot"
	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/view"
)

// HandleHomePage renders the control index page with lightweight aggregate counts.
func HandleHomePage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controls, err := storage.ListControls(r.Context(), db)
		if err != nil {
			log.Printf("list controls: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		var stats []view.ControlWithStats
		for _, c := range controls {
			keys, err := storage.ListAggregateKeys(r.Context(), db, c.ControlID)
			if err != nil {
				log.Printf("list aggregate keys for %s: %v", c.ControlID, err)
				keys = nil
			}
			stats = append(stats, view.ControlWithStats{
				ControlID:      c.ControlID,
				ControlType:    string(c.ControlType),
				NumStates:      c.NumStates,
				StateLabels:    c.StateLabels,
				AggregateCount: len(keys),
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.HomePage(stats).Render(r.Context(), w); err != nil {
			log.Printf("render home: %v", err)
		}
	}
}

// HandleNewControlPage renders the create-control form.
func HandleNewControlPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderControlFormPage(w, r, controlFormPageData(newControlFormData(storage.Control{}), "", false, ""))
	}
}

// HandleCreateControl saves a newly configured control from the UI.
func HandleCreateControl(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		control, form, errMsg := parseControlForm(r)
		if errMsg != "" {
			renderControlFormPage(w, r, controlFormPageData(form, "", false, errMsg))
			return
		}

		if err := storage.SaveControl(r.Context(), db, "", control); err != nil {
			log.Printf("create control %s: %v", control.ControlID, err)
			renderControlFormPage(w, r, controlFormPageData(form, "", false, mapControlSaveError(err)))
			return
		}

		http.Redirect(w, r, controlPageURL(url.PathEscape(control.ControlID), ""), http.StatusSeeOther)
	}
}

// HandleControlPage renders one control detail page and its quarter selector.
func HandleControlPage(db *sql.DB) http.HandlerFunc {
	return handleControlPage(db, "")
}

func handleControlPage(db *sql.DB, errMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controlID := r.PathValue("controlID")
		if controlID == "" {
			http.NotFound(w, r)
			return
		}

		control, err := storage.GetControl(r.Context(), db, controlID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("get control %s: %v", controlID, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		keys, err := storage.ListAggregateKeys(r.Context(), db, controlID)
		if err != nil {
			log.Printf("list aggregate keys for %s: %v", controlID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		data, err := buildControlPageData(r, db, control, keys)
		if err != nil {
			log.Printf("build control page data for %s: %v", controlID, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		data.FormData = controlFormPageData(newControlFormData(control), control.ControlID, len(keys) > 0, errMsg)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.ControlPage(data).Render(r.Context(), w); err != nil {
			log.Printf("render control: %v", err)
		}
	}
}

// HandleUpdateControl saves edits for an existing control from the UI.
func HandleUpdateControl(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		existing, hasAggregates, err := loadExistingControl(r, db)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("load control for update: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		control, form, errMsg := parseControlForm(r)
		if errMsg != "" {
			renderExistingControlPage(w, r, db, existing, hasAggregates, form, errMsg, view.ModelFormData{}, "")
			return
		}
		if hasAggregates {
			if lockErr := rejectStructuralChange(existing, control); lockErr != "" {
				renderExistingControlPage(w, r, db, existing, true, form, lockErr, view.ModelFormData{}, "")
				return
			}
		}

		if err := storage.SaveControl(r.Context(), db, existing.ControlID, control); err != nil {
			log.Printf("update control %s: %v", existing.ControlID, err)
			renderExistingControlPage(w, r, db, existing, hasAggregates, form, mapControlSaveError(err), view.ModelFormData{}, "")
			return
		}

		http.Redirect(w, r, controlPageURL(url.PathEscape(control.ControlID), ""), http.StatusSeeOther)
	}
}

// HandleCreateModel saves a new model for an existing control from the UI.
func HandleCreateModel(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		control, hasAggregates, err := loadExistingControl(r, db)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("load control for model create: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		model, form, errMsg := parseModelForm(r)
		if errMsg != "" {
			renderExistingControlPage(w, r, db, control, hasAggregates, newControlFormData(control), "", form, errMsg)
			return
		}
		if err := storage.SaveModel(r.Context(), db, control.ControlID, "", model); err != nil {
			log.Printf("create model %s/%s: %v", control.ControlID, model.ModelID, err)
			renderExistingControlPage(w, r, db, control, hasAggregates, newControlFormData(control), "", form, mapModelSaveError(err))
			return
		}
		http.Redirect(w, r, controlPageURL(url.PathEscape(control.ControlID), model.ModelID), http.StatusSeeOther)
	}
}

// HandleUpdateModel saves edits for an existing model from the UI.
func HandleUpdateModel(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		control, hasAggregates, err := loadExistingControl(r, db)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("load control for model update: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		previousModelID := r.PathValue("modelID")
		model, form, errMsg := parseModelForm(r)
		if errMsg != "" {
			renderExistingControlPage(w, r, db, control, hasAggregates, newControlFormData(control), "", form, errMsg)
			return
		}
		if err := storage.SaveModel(r.Context(), db, control.ControlID, previousModelID, model); err != nil {
			log.Printf("update model %s/%s: %v", control.ControlID, previousModelID, err)
			renderExistingControlPage(w, r, db, control, hasAggregates, newControlFormData(control), "", form, mapModelSaveError(err))
			return
		}
		http.Redirect(w, r, controlPageURL(url.PathEscape(control.ControlID), model.ModelID), http.StatusSeeOther)
	}
}

func renderExistingControlPage(w http.ResponseWriter, r *http.Request, db *sql.DB, control storage.Control, hasAggregates bool, form view.ControlFormData, errMsg string, modelForm view.ModelFormData, modelErr string) {
	keys, err := storage.ListAggregateKeys(r.Context(), db, control.ControlID)
	if err != nil {
		log.Printf("list aggregate keys for %s: %v", control.ControlID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data, err := buildControlPageData(r, db, control, keys)
	if err != nil {
		log.Printf("build control page data for %s: %v", control.ControlID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data.FormData = controlFormPageData(form, control.ControlID, hasAggregates, errMsg)
	data.ModelForm = defaultModelForm(control.ControlID)
	if r.PathValue("modelID") != "" && (modelForm.ModelID != "" || modelErr != "") {
		applyModelRowError(&data, r.PathValue("modelID"), modelForm, modelErr)
	} else if modelForm.ModelID != "" || modelErr != "" {
		data.ModelForm = modelForm
		data.ModelForm.Action = fmt.Sprintf("/controls/%s/models/new", url.PathEscape(control.ControlID))
		data.ModelForm.Error = modelErr
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := view.ControlPage(data).Render(r.Context(), w); err != nil {
		log.Printf("render control: %v", err)
	}
}

func buildControlPageData(r *http.Request, db *sql.DB, control storage.Control, keys []storage.AggregateKey) (view.ControlPageData, error) {
	data := view.ControlPageData{
		ControlID:     control.ControlID,
		ControlType:   string(control.ControlType),
		NumStates:     control.NumStates,
		StateLabels:   control.StateLabels,
		FormData:      controlFormPageData(newControlFormData(control), control.ControlID, len(keys) > 0, ""),
		ModelForm:     defaultModelForm(control.ControlID),
		HasAggregates: len(keys) > 0,
	}
	models, err := storage.ListModels(r.Context(), db, control.ControlID)
	if err != nil {
		return view.ControlPageData{}, err
	}
	for _, model := range models {
		data.Models = append(data.Models, view.ModelRowData{
			ModelID: model.ModelID,
			Action:  fmt.Sprintf("/controls/%s/models/%s", url.PathEscape(control.ControlID), url.PathEscape(model.ModelID)),
		})
		data.ModelOptions = append(data.ModelOptions, view.ModelOption{
			ModelID:  model.ModelID,
			Selected: false,
			PageURL:  controlPageURL(url.PathEscape(control.ControlID), model.ModelID),
		})
	}

	selectedModelID := ""
	if requestedModelID := r.URL.Query().Get("model"); requestedModelID != "" {
		for _, model := range data.Models {
			if model.ModelID == requestedModelID {
				selectedModelID = requestedModelID
				break
			}
		}
	}
	if selectedModelID == "" && len(data.Models) > 0 {
		selectedModelID = data.Models[0].ModelID
	}
	if selectedModelID == "" && len(keys) > 0 {
		selectedModelID = keys[0].ModelID
	}
	data.ModelID = selectedModelID
	for i := range data.ModelOptions {
		data.ModelOptions[i].Selected = data.ModelOptions[i].ModelID == selectedModelID
	}

	quarterSet := make(map[int]string)
	for _, k := range keys {
		if k.ModelID != selectedModelID {
			continue
		}
		if _, ok := quarterSet[k.QuarterIndex]; !ok {
			quarterSet[k.QuarterIndex] = quarterLabel(k.QuarterIndex)
		}
	}

	selectedQuarter := -1
	if qStr := r.URL.Query().Get("quarter"); qStr != "" {
		if q, err := strconv.Atoi(qStr); err == nil {
			selectedQuarter = q
		}
	}

	quarterIndexes := make([]int, 0, len(quarterSet))
	latestQuarter := -1
	for qi := range quarterSet {
		if qi > latestQuarter {
			latestQuarter = qi
		}
		quarterIndexes = append(quarterIndexes, qi)
	}
	slices.Sort(quarterIndexes)

	if selectedQuarter < 0 && latestQuarter >= 0 {
		selectedQuarter = latestQuarter
	}

	quarters := make([]view.QuarterOption, 0, len(quarterIndexes))
	for _, qi := range quarterIndexes {
		quarters = append(quarters, view.QuarterOption{
			QuarterIndex: qi,
			Label:        quarterSet[qi],
			Selected:     qi == selectedQuarter,
		})
	}
	data.Quarters = quarters

	if selectedModelID != "" && selectedQuarter >= 0 {
		data.BucketJSON = buildBucketJSON(r.Context(), db, control.ControlID, selectedModelID, selectedQuarter, control.NumStates)
	}
	return data, nil
}

func defaultModelForm(controlID string) view.ModelFormData {
	return view.ModelFormData{
		Action: fmt.Sprintf("/controls/%s/models/new", url.PathEscape(controlID)),
	}
}

func applyModelRowError(data *view.ControlPageData, previousModelID string, form view.ModelFormData, errMsg string) {
	for i := range data.Models {
		if data.Models[i].ModelID != previousModelID {
			continue
		}
		data.Models[i].DraftModelID = form.ModelID
		data.Models[i].Error = errMsg
		return
	}
}

func controlPageURL(escapedControlID, modelID string) string {
	pageURL := fmt.Sprintf("/controls/%s", escapedControlID)
	if modelID == "" {
		return pageURL
	}
	return fmt.Sprintf("%s?model=%s", pageURL, url.QueryEscape(modelID))
}

// HandleSnapshotPage renders the snapshot listing page.
func HandleSnapshotPage(snapshotDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infos, err := snapshot.ListSnapshots(snapshotDir)
		if err != nil {
			log.Printf("list snapshots: %v", err)
			infos = nil
		}

		var entries []view.SnapshotEntry
		for _, info := range infos {
			entries = append(entries, view.SnapshotEntry{
				Name:    info.Name,
				Size:    formatBytes(info.Size),
				ModTime: info.ModTime.Format("2006-01-02 15:04:05"),
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.SnapshotPage(entries).Render(r.Context(), w); err != nil {
			log.Printf("render snapshots: %v", err)
		}
	}
}

// HandleHeatmapPartial renders the heatmap fragment for one control and quarter selection.
func HandleHeatmapPartial(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controlID := r.URL.Query().Get("controlId")
		modelID := r.URL.Query().Get("modelId")
		quarterStr := r.URL.Query().Get("quarter")
		if controlID == "" || quarterStr == "" {
			http.Error(w, "missing params", http.StatusBadRequest)
			return
		}

		quarterIndex, err := strconv.Atoi(quarterStr)
		if err != nil {
			http.Error(w, "invalid quarter", http.StatusBadRequest)
			return
		}

		control, err := storage.GetControl(r.Context(), db, controlID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("get control %s: %v", controlID, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		keys, err := storage.ListAggregateKeys(r.Context(), db, controlID)
		if err != nil {
			log.Printf("list aggregate keys for %s: %v", controlID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if modelID == "" && len(keys) > 0 {
			modelID = keys[0].ModelID
		}

		bucketJSON := buildBucketJSON(r.Context(), db, controlID, modelID, quarterIndex, control.NumStates)
		if bucketJSON == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<p class="empty">No data for this quarter.</p>`))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.HeatmapCanvas(bucketJSON).Render(r.Context(), w); err != nil {
			log.Printf("render heatmap partial: %v", err)
		}
	}
}

// buildBucketJSON reduces one aggregate into the UTC holding series used by the heatmap.
func buildBucketJSON(ctx context.Context, db *sql.DB, controlID, modelID string, quarterIndex, numStates int) string {
	if modelID == "" {
		return ""
	}
	key := storage.AggregateKey{ControlID: controlID, ModelID: modelID, QuarterIndex: quarterIndex}
	data, err := storage.GetAggregate(ctx, db, key, numStates)
	if err != nil {
		return ""
	}

	b, err := domain.NewBlob(numStates)
	if err != nil {
		return ""
	}
	copy(b.Data(), data)

	// The heatmap is intentionally a single normalized view: UTC holding time
	// summed across states. The aggregate still retains all other clocks and
	// transition counters for future visualizations.
	buckets := make([]uint64, domain.BucketsPerWeek)
	for state := 0; state < numStates; state++ {
		for bkt := 0; bkt < domain.BucketsPerWeek; bkt++ {
			idx, err := domain.HoldIndex(state, domain.ClockUTC, bkt, numStates)
			if err != nil {
				continue
			}
			v, err := b.GetU64(idx)
			if err != nil {
				continue
			}
			buckets[bkt] += v
		}
	}

	jsonBytes, err := json.Marshal(buckets)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// quarterLabel formats the internal quarter index for display.
func quarterLabel(qi int) string {
	year := 1970 + qi/4
	q := qi%4 + 1
	return fmt.Sprintf("%d Q%d", year, q)
}

// formatBytes renders byte counts using compact binary-style units for the UI.
func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}
