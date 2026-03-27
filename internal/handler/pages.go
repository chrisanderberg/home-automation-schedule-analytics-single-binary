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

	"home-automation-schedule-analytics-single-bin/internal/analytics"
	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/snapshot"
	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/view"
)

var stateChartPalette = []string{
	"#4f7cff",
	"#ff8a3d",
	"#30b98f",
	"#ef5b7a",
	"#8c6dfd",
	"#e3b341",
	"#28a0c7",
	"#c268d8",
}

type weeklyBarPayload struct {
	Series      []float64 `json:"series"`
	ValueFormat string    `json:"valueFormat"`
}

type weeklyStackedPayload struct {
	Stacks      []weeklyStack `json:"stacks"`
	ValueFormat string        `json:"valueFormat"`
}

type weeklyStack struct {
	Label  string    `json:"label"`
	Color  string    `json:"color"`
	Values []float64 `json:"values"`
}

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

// handleControlPage renders one control page and optionally surfaces a top-level form error.
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

// renderExistingControlPage renders the combined control configuration and analytics page.
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

// buildControlPageData assembles the selector state, analytics report, and model table for one control page.
func buildControlPageData(r *http.Request, db *sql.DB, control storage.Control, keys []storage.AggregateKey) (view.ControlPageData, error) {
	query := cloneQueryValues(r.URL.Query())
	data := view.ControlPageData{
		ControlID:     control.ControlID,
		ControlType:   string(control.ControlType),
		NumStates:     control.NumStates,
		StateLabels:   control.StateLabels,
		FormData:      controlFormPageData(newControlFormData(control), control.ControlID, len(keys) > 0, ""),
		ModelForm:     defaultModelForm(control.ControlID),
		HasAggregates: len(keys) > 0,
	}
	selectedMode := query.Get("mode")
	if selectedMode == "" {
		selectedMode = "report"
	}
	if selectedMode != "report" && selectedMode != "raw" {
		selectedMode = "report"
	}
	data.AnalyticsMode = selectedMode
	models, err := storage.ListModels(r.Context(), db, control.ControlID)
	if err != nil {
		return view.ControlPageData{}, err
	}
	for _, model := range models {
		data.Models = append(data.Models, view.ModelRowData{
			ModelID: model.ModelID,
			Action:  fmt.Sprintf("/controls/%s/models/%s", url.PathEscape(control.ControlID), url.PathEscape(model.ModelID)),
		})
	}

	selectedModelID := ""
	// Page selection prefers an explicit query parameter, then a model with data,
	// then any known model so the page lands on the most useful analytics slice.
	if requestedModelID := r.URL.Query().Get("model"); requestedModelID != "" {
		for _, model := range data.Models {
			if model.ModelID == requestedModelID {
				selectedModelID = requestedModelID
				break
			}
		}
	}
	if selectedModelID == "" && len(data.Models) > 0 {
		keyedModels := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			keyedModels[key.ModelID] = struct{}{}
		}
		for _, model := range data.Models {
			if _, ok := keyedModels[model.ModelID]; ok {
				selectedModelID = model.ModelID
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

	quarterSet := make(map[int]string)
	// Quarter choices are scoped to the selected model because reports are built
	// one `(control, model, quarter)` slice at a time.
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
	if selectedQuarter >= 0 {
		if _, ok := quarterSet[selectedQuarter]; !ok && latestQuarter >= 0 {
			selectedQuarter = latestQuarter
		}
	}

	quarters := make([]view.QuarterOption, 0, len(quarterIndexes))
	for _, qi := range quarterIndexes {
		quarters = append(quarters, view.QuarterOption{
			QuarterIndex: qi,
			Label:        quarterSet[qi],
			Selected:     qi == selectedQuarter,
			PageURL:      controlAnalyticsPageURL(control.ControlID, analyticsPageURLQuery(query, selectedModelID, qi, query.Get("clock"), selectedMode)),
		})
	}
	data.Quarters = quarters

	selectedClock := query.Get("clock")
	reportOpts, reportOptsErr := parseReportOptions(r)
	if reportOptsErr != nil {
		log.Printf("parse report options for control %s: %v", control.ControlID, reportOptsErr)
		reportOpts = analytics.DefaultReportOptions()
	}
	if selectedModelID != "" && selectedQuarter >= 0 {
		rawReport, err := analytics.BuildRawReport(r.Context(), db, control, selectedModelID, selectedQuarter)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return view.ControlPageData{}, err
		}
		if err == nil {
			if selectedClock == "" && len(rawReport.Clocks) > 0 {
				selectedClock = rawReport.Clocks[0].ClockSlug
			}
			if selectedClock != "" {
				if _, err := rawReport.ClockBySlug(selectedClock); err != nil && len(rawReport.Clocks) > 0 {
					selectedClock = rawReport.Clocks[0].ClockSlug
				}
			}
			for _, clockReport := range rawReport.Clocks {
				data.ClockOptions = append(data.ClockOptions, view.ClockOption{
					ClockSlug: clockReport.ClockSlug,
					Label:     clockReport.ClockLabel,
					Selected:  clockReport.ClockSlug == selectedClock,
					PageURL:   controlAnalyticsPageURL(control.ControlID, analyticsPageURLQuery(query, selectedModelID, selectedQuarter, clockReport.ClockSlug, selectedMode)),
				})
			}
			selectedRaw, err := rawReport.ClockBySlug(selectedClock)
			if err == nil {
				data.RawAnalytics = buildRawAnalyticsViewData(rawReport, selectedRaw)
			}
			derived, err := analytics.BuildDerivedReportFromRaw(rawReport, reportOpts)
			if err != nil {
				return view.ControlPageData{}, err
			}
			selectedReport, err := derived.ClockBySlug(selectedClock)
			if err == nil {
				data.Analytics = buildAnalyticsViewData(derived, selectedReport)
			}
		}
	}
	for _, model := range models {
		data.ModelOptions = append(data.ModelOptions, view.ModelOption{
			ModelID:  model.ModelID,
			Selected: model.ModelID == selectedModelID,
			PageURL:  controlAnalyticsPageURL(control.ControlID, analyticsPageURLQuery(query, model.ModelID, selectedQuarter, selectedClock, selectedMode)),
		})
	}
	for _, mode := range []struct {
		label string
		value string
	}{
		{label: "Report", value: "report"},
		{label: "Raw", value: "raw"},
	} {
		data.ModeOptions = append(data.ModeOptions, view.AnalyticsModeOption{
			Label:    mode.label,
			Mode:     mode.value,
			Selected: selectedMode == mode.value,
			PageURL:  controlAnalyticsPageURL(control.ControlID, analyticsPageURLQuery(query, selectedModelID, selectedQuarter, selectedClock, mode.value)),
		})
	}
	data.ReportOptions = buildAnalyticsOptionsFormData(control.ControlID, selectedModelID, selectedQuarter, selectedClock, selectedMode, reportOpts, reportOptsErr)
	return data, nil
}

// defaultModelForm returns the empty model form shown on a control page.
func defaultModelForm(controlID string) view.ModelFormData {
	return view.ModelFormData{
		Action: fmt.Sprintf("/controls/%s/models/new", url.PathEscape(controlID)),
	}
}

// applyModelRowError injects a model form error back into the matching control-page row.
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

// controlPageURL builds a control-page URL while preserving the selected model when present.
func controlPageURL(escapedControlID, modelID string) string {
	pageURL := fmt.Sprintf("/controls/%s", escapedControlID)
	if modelID == "" {
		return pageURL
	}
	return fmt.Sprintf("%s?model=%s", pageURL, url.QueryEscape(modelID))
}

// controlAnalyticsPageURL builds the analytics-selector URL for one control page state.
func controlAnalyticsPageURL(controlID string, values url.Values) string {
	path := fmt.Sprintf("/controls/%s", url.PathEscape(controlID))
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

func analyticsPageURLQuery(base url.Values, modelID string, quarter int, clock, mode string) url.Values {
	values := cloneQueryValues(base)
	if modelID != "" {
		values.Set("model", modelID)
	} else {
		values.Del("model")
	}
	if quarter >= 0 {
		values.Set("quarter", strconv.Itoa(quarter))
	} else {
		values.Del("quarter")
	}
	if clock != "" {
		values.Set("clock", clock)
	} else {
		values.Del("clock")
	}
	if mode != "" && mode != "report" {
		values.Set("mode", mode)
	} else {
		values.Del("mode")
	}
	return values
}

func cloneQueryValues(values url.Values) url.Values {
	cloned := url.Values{}
	for key, all := range values {
		cloned[key] = append([]string(nil), all...)
	}
	return cloned
}

func buildAnalyticsOptionsFormData(controlID, modelID string, quarter int, clock, mode string, opts analytics.ReportOptions, parseErr error) view.AnalyticsOptionsFormData {
	data := view.AnalyticsOptionsFormData{
		Action:                 fmt.Sprintf("/controls/%s", url.PathEscape(controlID)),
		ModelID:                modelID,
		Mode:                   mode,
		Smoothing:              opts.SmoothingKind,
		HoldingDampingMillis:   formatFloatParam(opts.HoldingDampingMillis),
		TransitionDampingCount: formatFloatParam(opts.TransitionDampingCount),
		IncludeRaw:             opts.Include.Raw,
		IncludeSmoothed:        opts.Include.Smoothed,
		IncludeRates:           opts.Include.Rates,
	}
	if quarter >= 0 {
		data.Quarter = strconv.Itoa(quarter)
	}
	if clock != "" {
		data.Clock = clock
	}
	if opts.SmoothingKind == analytics.SmoothingGaussian {
		data.KernelRadius = strconv.Itoa(opts.KernelRadius)
		data.KernelSigma = formatFloatParam(opts.KernelSigma)
	}
	if parseErr != nil {
		data.Error = parseErr.Error()
	}
	return data
}

func formatFloatParam(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// buildAnalyticsViewData projects analytics report data into the view model consumed by the template.
func buildAnalyticsViewData(report analytics.DerivedReport, selected analytics.DerivedClockReport) view.AnalyticsViewData {
	data := view.AnalyticsViewData{
		HasData:      true,
		ModelID:      report.ModelID,
		QuarterLabel: report.QuarterLabel,
		ClockLabel:   selected.ClockLabel,
	}
	if selected.Diagnostics != nil {
		data.Diagnostics = view.AnalyticsDiagnostics{
			TotalHolding:    formatDurationMillis(selected.Diagnostics.TotalHoldingMillis),
			TransitionCount: fmt.Sprintf("%.0f", selected.Diagnostics.TransitionCount),
			FallbackBuckets: selected.Diagnostics.FallbackBuckets,
		}
	}
	legendItems := make([]view.ChartLegendItem, 0, len(selected.OccupancySeries))
	for idx := range selected.OccupancySeries {
		occupancySeries := selected.OccupancySeries[idx]
		preferenceSeries := selected.PreferenceSeries[idx]
		data.States = append(data.States, view.AnalyticsStateData{
			OccupancyMean:  formatShare(occupancySeries.Mean),
			PreferenceMean: formatShare(preferenceSeries.Mean),
			Label:          occupancySeries.Label,
		})
		legendItems = append(legendItems, view.ChartLegendItem{
			Label: occupancySeries.Label,
			Color: chartColor(idx),
		})
	}
	data.ChartLegendItems = legendItems
	data.OccupancyChart = buildWeeklyStackedChartData(selected.OccupancySeries, "share", "Actual occupancy by bucket")
	data.PreferenceChart = buildWeeklyStackedChartData(selected.PreferenceSeries, "share", "Inferred preference by bucket")
	return data
}

func buildRawAnalyticsViewData(report analytics.RawReport, selected analytics.RawClockReport) view.RawAnalyticsViewData {
	data := view.RawAnalyticsViewData{
		HasData:      true,
		ModelID:      report.ModelID,
		QuarterLabel: report.QuarterLabel,
		ClockLabel:   selected.ClockLabel,
	}
	var totalHolding uint64
	var totalTransitions uint64
	totalHoldingBuckets := make([]uint64, domain.BucketsPerWeek)
	totalTransitionBuckets := make([]uint64, domain.BucketsPerWeek)
	for _, holding := range selected.HoldingMillis {
		stateTotal := sumUint64Series(holding.Buckets)
		totalHolding += stateTotal
		addUint64Series(totalHoldingBuckets, holding.Buckets)
		data.HoldingStates = append(data.HoldingStates, view.RawAnalyticsStateData{
			Label:        holding.Label,
			TotalHolding: formatDurationMillis(float64(stateTotal)),
			HoldingChart: buildWeeklyBarChartDataFromUint(holding.Buckets, "durationMillis", holding.Label),
		})
	}
	for _, transition := range selected.TransitionCounts {
		transitionTotal := sumUint64Series(transition.Buckets)
		totalTransitions += transitionTotal
		addUint64Series(totalTransitionBuckets, transition.Buckets)
		data.TransitionSeries = append(data.TransitionSeries, view.RawAnalyticsTransitionData{
			Label:           fmt.Sprintf("%s → %s", transition.FromLabel, transition.ToLabel),
			TransitionTotal: fmt.Sprintf("%d", transitionTotal),
			TransitionChart: buildWeeklyBarChartDataFromUint(transition.Buckets, "count", fmt.Sprintf("%s → %s", transition.FromLabel, transition.ToLabel)),
		})
	}
	data.TotalHolding = formatDurationMillis(float64(totalHolding))
	data.TransitionCount = fmt.Sprintf("%d", totalTransitions)
	data.TotalHoldingChart = buildWeeklyBarChartDataFromUint(totalHoldingBuckets, "durationMillis", "Total holding time")
	data.TotalTransitionChart = buildWeeklyBarChartDataFromUint(totalTransitionBuckets, "count", "Total transitions")
	return data
}

func sumUint64Series(values []uint64) uint64 {
	var total uint64
	for _, value := range values {
		total += value
	}
	return total
}

func addUint64Series(dst, src []uint64) {
	for i := range min(len(dst), len(src)) {
		dst[i] += src[i]
	}
}

func buildWeeklyBarChartDataFromUint(values []uint64, valueFormat, summary string) view.ChartData {
	series := make([]float64, len(values))
	for i, value := range values {
		series[i] = float64(value)
	}
	return buildWeeklyBarChartData(series, valueFormat, summary)
}

func buildWeeklyBarChartData(values []float64, valueFormat, summary string) view.ChartData {
	return view.ChartData{
		Kind:    "bars",
		Payload: marshalChartPayload(weeklyBarPayload{Series: values, ValueFormat: valueFormat}),
		Summary: summary,
	}
}

func buildWeeklyStackedChartData(series []analytics.Series, valueFormat, summary string) view.ChartData {
	stacks := make([]weeklyStack, 0, len(series))
	for idx, stateSeries := range series {
		stacks = append(stacks, weeklyStack{
			Label:  stateSeries.Label,
			Color:  chartColor(idx),
			Values: append([]float64(nil), stateSeries.Buckets...),
		})
	}
	return view.ChartData{
		Kind:    "stacked",
		Payload: marshalChartPayload(weeklyStackedPayload{Stacks: stacks, ValueFormat: valueFormat}),
		Summary: summary,
	}
}

func marshalChartPayload(value any) string {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func chartColor(index int) string {
	return stateChartPalette[index%len(stateChartPalette)]
}

// formatShare renders a normalized share as a percentage string.
func formatShare(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

// formatDurationMillis renders milliseconds as an hours string for diagnostics.
func formatDurationMillis(value float64) string {
	totalSeconds := int64(value / 1000)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
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

// HandleHeatmapPartial renders a compact chart fragment for one control and quarter selection.
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

		chart := buildAggregateHoldingChart(r.Context(), db, controlID, modelID, quarterIndex, control.NumStates)
		if chart.Payload == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<p class="empty">No data for this quarter.</p>`))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.ChartCanvas(chart).Render(r.Context(), w); err != nil {
			log.Printf("render analytics partial: %v", err)
		}
	}
}

// buildAggregateHoldingChart reduces one aggregate into a single UTC holding chart.
func buildAggregateHoldingChart(ctx context.Context, db *sql.DB, controlID, modelID string, quarterIndex, numStates int) view.ChartData {
	if modelID == "" {
		return view.ChartData{}
	}
	key := storage.AggregateKey{ControlID: controlID, ModelID: modelID, QuarterIndex: quarterIndex}
	data, err := storage.GetAggregate(ctx, db, key, numStates)
	if err != nil {
		return view.ChartData{}
	}

	b, err := domain.NewBlob(numStates)
	if err != nil {
		return view.ChartData{}
	}
	copy(b.Data(), data)

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
	return buildWeeklyBarChartDataFromUint(buckets, "durationMillis", "Total UTC holding time")
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
