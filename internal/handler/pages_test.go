package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/demodata"
	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

// TestHomePageRendersControls verifies the home page renders persisted controls.
func TestHomePageRendersControls(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "light", ControlType: storage.ControlTypeRadioButtons, NumStates: 3,
		StateLabels: []string{"off", "dim", "bright"},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	HandleHomePage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "light") {
		t.Fatalf("expected body to contain 'light'")
	}
	if !strings.Contains(body, "Controls") {
		t.Fatalf("expected body to contain 'Controls'")
	}
	if !strings.Contains(body, `href="/controls/new"`) {
		t.Fatalf("expected add-control link in home page")
	}
}

// TestHomePageEscapesControlLinks verifies control links are URL-escaped in the rendered page.
func TestHomePageEscapesControlLinks(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "mode/scene", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	HandleHomePage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `/controls/mode%2Fscene`) {
		t.Fatalf("expected escaped control link, got body %q", w.Body.String())
	}
}

// TestHomePageEmptyState verifies the home page shows an empty state with no controls.
func TestHomePageEmptyState(t *testing.T) {
	db := openTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	HandleHomePage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No controls") {
		t.Fatalf("expected empty state message")
	}
	if strings.Contains(w.Body.String(), "Use the API to add controls") {
		t.Fatalf("expected UI-first empty state")
	}
}

// TestControlPageReturns200 verifies the control detail page renders for an existing control.
func TestControlPageReturns200(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save model: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "mode") {
		t.Fatalf("expected body to contain 'mode'")
	}
	if !strings.Contains(body, "weekday") {
		t.Fatalf("expected body to contain model list")
	}
}

// TestControlPageSelectsLatestQuarterAndOrdersOptions verifies quarter buttons are sorted and default to the newest quarter.
func TestControlPageSelectsLatestQuarterAndOrdersOptions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "default"}); err != nil {
		t.Fatalf("save model: %v", err)
	}

	for _, quarterIndex := range []int{12, 10, 11} {
		if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
			ControlID:    "mode",
			ModelID:      "default",
			QuarterIndex: quarterIndex,
		}, 2); err != nil {
			t.Fatalf("seed aggregate %d: %v", quarterIndex, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	pos10 := strings.Index(body, "1972 Q3")
	pos11 := strings.Index(body, "1972 Q4")
	pos12 := strings.Index(body, "1973 Q1")
	if pos10 == -1 || pos11 == -1 || pos12 == -1 {
		t.Fatalf("expected all quarter labels in body")
	}
	if !(pos10 < pos11 && pos11 < pos12) {
		t.Fatalf("expected sorted quarter options, got body %q", body)
	}
	if !strings.Contains(body, `class="selector-pill selected"`) || !strings.Contains(body, `href="/controls/mode?clock=utc&amp;model=default&amp;quarter=12"`) {
		t.Fatalf("expected latest quarter link to be selected")
	}
}

// TestControlPageEscapesQuarterRequests verifies the heatmap partial URLs escape control identifiers correctly.
func TestControlPageEscapesQuarterRequests(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode/scene", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode/scene", "", storage.Model{ModelID: "default"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode/scene",
		ModelID:      "default",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode%2Fscene", nil)
	req.SetPathValue("controlID", "mode/scene")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `href="/controls/mode%2Fscene?clock=utc&amp;model=default&amp;quarter=12"`) {
		t.Fatalf("expected escaped quarter link, got body %q", w.Body.String())
	}
}

// TestControlPageSelectsRequestedModel verifies the page can display a non-default model partition.
func TestControlPageSelectsRequestedModel(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekend"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "weekday",
		QuarterIndex: 10,
	}, 2); err != nil {
		t.Fatalf("seed weekday aggregate: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "weekend",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed weekend aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode?model=weekend", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Analytics View") || !strings.Contains(body, ">weekend<") {
		t.Fatalf("expected analytics model selector in body, got %q", body)
	}
	if strings.Contains(body, "1972 Q3") {
		t.Fatalf("expected quarter buttons only for selected model, got %q", body)
	}
	if !strings.Contains(body, `href="/controls/mode?clock=utc&amp;model=weekend&amp;quarter=12"`) {
		t.Fatalf("expected weekend analytics link, got %q", body)
	}
	if !strings.Contains(body, "Inferred preference") {
		t.Fatalf("expected inferred preference report content, got %q", body)
	}
}

// TestControlPageDefaultsToModelWithData verifies the default analytics selection prefers a model that has aggregates.
func TestControlPageDefaultsToModelWithData(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekend"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "weekend",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `href="/controls/mode?clock=utc&amp;model=weekend&amp;quarter=12"`) {
		t.Fatalf("expected model with data to be selected, got %q", body)
	}
}

// TestControlPageRendersSeededDemoReport verifies the full report view renders seeded analytics content.
func TestControlPageRendersSeededDemoReport(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/living-room-scene?model="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc", nil)
	req.SetPathValue("controlID", "living-room-scene")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Per-model diagnostics and inferred preference") {
		t.Fatalf("expected report header, got %q", body)
	}
	if !strings.Contains(body, "Stacked weekly distribution") || !strings.Contains(body, "Stacked weekly stationary distribution") {
		t.Fatalf("expected both report panels, got %q", body)
	}
	if !strings.Contains(body, "ambient") || !strings.Contains(body, "bright") || !strings.Contains(body, `class="weekly-chart"`) {
		t.Fatalf("expected seeded state labels in report, got %q", body)
	}
}

// TestControlPageCanRenderRawAnalytics verifies the control page can switch into raw analytics mode.
func TestControlPageCanRenderRawAnalytics(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/living-room-scene?model="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc&mode=raw", nil)
	req.SetPathValue("controlID", "living-room-scene")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Raw Analytics") {
		t.Fatalf("expected raw analytics header, got %q", body)
	}
	if !strings.Contains(body, "Total holding time") || !strings.Contains(body, "Total transitions") {
		t.Fatalf("expected raw analytics panels, got %q", body)
	}
}

// TestControlPageShowsReportParameterControls verifies the report-mode UI exposes parameter controls.
func TestControlPageShowsReportParameterControls(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/living-room-scene?model="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc&smoothing=none&holdingDampingMillis=none&transitionDampingCount=none&include=raw&include=rates", nil)
	req.SetPathValue("controlID", "living-room-scene")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Report Parameters") {
		t.Fatalf("expected report parameter form, got %q", body)
	}
	if !strings.Contains(body, `name="smoothing"`) || !strings.Contains(body, `name="holdingDampingMillis"`) {
		t.Fatalf("expected smoothing and damping controls, got %q", body)
	}
	if !strings.Contains(body, `name="include" value="raw" checked`) || !strings.Contains(body, `name="include" value="rates" checked`) {
		t.Fatalf("expected include checkboxes to reflect query params, got %q", body)
	}
}

// TestControlPageRawModeEmbedsSameBucketsAsAPI verifies the raw-mode page uses the same bucket arrays as the raw API.
func TestControlPageRawModeEmbedsSameBucketsAsAPI(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/raw?controlId=living-room-scene&modelId="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc", nil)
	apiW := httptest.NewRecorder()
	HandleAnalyticsRaw(db).ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", apiW.Code, apiW.Body.String())
	}
	var apiBody struct {
		Clock struct {
			HoldingMillis []struct {
				Buckets []uint64 `json:"buckets"`
			} `json:"holdingMillis"`
		} `json:"clock"`
	}
	if err := json.Unmarshal(apiW.Body.Bytes(), &apiBody); err != nil {
		t.Fatalf("decode api body: %v", err)
	}
	firstSeries, err := json.Marshal(apiBody.Clock.HoldingMillis[0].Buckets)
	if err != nil {
		t.Fatalf("marshal expected series: %v", err)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/controls/living-room-scene?model="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc&mode=raw", nil)
	pageReq.SetPathValue("controlID", "living-room-scene")
	pageW := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(pageW, pageReq)
	if pageW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", pageW.Code, pageW.Body.String())
	}
	if !strings.Contains(pageW.Body.String(), string(firstSeries)) {
		t.Fatalf("expected raw page to embed API bucket series %s", string(firstSeries))
	}
}

// TestControlPageReturns404 verifies missing controls return a not-found page response.
func TestControlPageReturns404(t *testing.T) {
	db := openTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/controls/missing", nil)
	req.SetPathValue("controlID", "missing")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestNewControlPageRendersForm verifies the create-control page is available from the UI.
func TestNewControlPageRendersForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/controls/new", nil)
	w := httptest.NewRecorder()
	HandleNewControlPage().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "New control") || !strings.Contains(body, `name="controlId"`) {
		t.Fatalf("expected create form, got %q", body)
	}
}

// TestCreateControlFromUI verifies UI submissions create new controls and redirect to the detail page.
func TestCreateControlFromUI(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"mode"},
		"controlType": {"radio buttons"},
		"numStates":   {"3"},
		"stateLabel":  {"off", "dim", "bright"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "/controls/mode" {
		t.Fatalf("unexpected redirect: %q", location)
	}

	control, err := storage.GetControl(context.Background(), db, "mode")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	if control.NumStates != 3 || len(control.StateLabels) != 3 || control.StateLabels[2] != "bright" {
		t.Fatalf("unexpected control: %+v", control)
	}
}

// TestCreateControlFromUIEscapesRedirect verifies the post-create redirect path-escapes the control ID.
func TestCreateControlFromUIEscapesRedirect(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"mode/scene"},
		"controlType": {"radio buttons"},
		"numStates":   {"2"},
		"stateLabel":  {"off", "on"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "/controls/mode%2Fscene" {
		t.Fatalf("unexpected redirect: %q", location)
	}
}

// TestCreateControlFromUIRejectsReservedNewID verifies the UI rejects the reserved create-page segment as a control ID.
func TestCreateControlFromUIRejectsReservedNewID(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"new"},
		"controlType": {"radio buttons"},
		"numStates":   {"2"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid controlId") {
		t.Fatalf("expected validation error, got %q", w.Body.String())
	}
}

// TestCreateRadioButtonsControlDefaultsToOnOff verifies the default two-state radio-buttons flow seeds on/off labels.
func TestCreateRadioButtonsControlDefaultsToOnOff(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"switch"},
		"controlType": {"radio buttons"},
		"numStates":   {"2"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}

	control, err := storage.GetControl(context.Background(), db, "switch")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	want := []string{"on", "off"}
	if control.NumStates != 2 || strings.Join(control.StateLabels, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected control: %+v", control)
	}
}

// TestCreateRadioButtonsControlDefaultsAdditionalStates verifies larger radio-button controls seed placeholder labels.
func TestCreateRadioButtonsControlDefaultsAdditionalStates(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"scene"},
		"controlType": {"radio buttons"},
		"numStates":   {"5"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}

	control, err := storage.GetControl(context.Background(), db, "scene")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	want := []string{"on", "off", "state 3", "state 4", "state 5"}
	if control.NumStates != 5 || strings.Join(control.StateLabels, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected control: %+v", control)
	}
}

// TestCreateSlidersFromUIDefaultsToSixStates verifies sliders creation ignores state-count choice and seeds default labels.
func TestCreateSlidersFromUIDefaultsToSixStates(t *testing.T) {
	db := openTestDB(t)
	form := url.Values{
		"controlId":   {"level"},
		"controlType": {"sliders"},
		"numStates":   {"3"},
	}

	req := httptest.NewRequest(http.MethodPost, "/controls/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	HandleCreateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}

	control, err := storage.GetControl(context.Background(), db, "level")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	if control.NumStates != 6 {
		t.Fatalf("expected 6 sliders states, got %d", control.NumStates)
	}
	want := []string{"min", "trans 1", "trans 2", "trans 3", "trans 4", "max"}
	if strings.Join(control.StateLabels, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected sliders labels: %+v", control.StateLabels)
	}
}

// TestControlPageLocksStructureWhenAggregatesExist verifies blob-shaping fields are locked once data exists.
func TestControlPageLocksStructureWhenAggregatesExist(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID: "mode", ModelID: "default", QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "locked because aggregate data already exists") {
		t.Fatalf("expected lock hint, got %q", body)
	}
	if !strings.Contains(body, `readonly`) {
		t.Fatalf("expected readonly structural field, got %q", body)
	}
}

// TestUpdateControlRejectsStateCountChangeWithAggregates verifies UI edits cannot invalidate stored aggregate shape.
func TestUpdateControlRejectsStateCountChangeWithAggregates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID: "mode", ModelID: "default", QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	form := url.Values{
		"controlId":   {"mode"},
		"controlType": {"radio buttons"},
		"numStates":   {"3"},
		"stateLabel":  {"off", "on", "boost"},
	}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleUpdateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "cannot change state count after aggregate data has been recorded") {
		t.Fatalf("expected structural change error, got %q", w.Body.String())
	}
}

// TestUpdateControlCanRenameControl verifies UI editing can change the control ID while preserving aggregate linkage.
func TestUpdateControlCanRenameControl(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
		StateLabels: []string{"off", "on"},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID: "mode", ModelID: "default", QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	form := url.Values{
		"controlId":   {"scene"},
		"controlType": {"radio buttons"},
		"numStates":   {"2"},
		"stateLabel":  {"off", "on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleUpdateControl(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "/controls/scene" {
		t.Fatalf("unexpected redirect: %q", location)
	}

	if _, err := storage.GetControl(ctx, db, "mode"); err != storage.ErrNotFound {
		t.Fatalf("expected old control to be removed, got %v", err)
	}
	keys, err := storage.ListAggregateKeys(ctx, db, "scene")
	if err != nil {
		t.Fatalf("list aggregate keys: %v", err)
	}
	if len(keys) != 1 || keys[0].ControlID != "scene" {
		t.Fatalf("expected aggregate to move with renamed control, got %+v", keys)
	}
}

// TestCreateModelFromUI verifies model creation is available from the control page.
func TestCreateModelFromUI(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	form := url.Values{"modelId": {"weekday"}}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode/models/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleCreateModel(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "/controls/mode?model=weekday" {
		t.Fatalf("unexpected redirect: %q", location)
	}
	models, err := storage.ListModels(ctx, db, "mode")
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "weekday" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

// TestUpdateModelCanRename verifies model editing can rename models.
func TestUpdateModelCanRename(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekend"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{ControlID: "mode", ModelID: "weekend", QuarterIndex: 12}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	form := url.Values{"modelId": {"vacation"}}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode/models/weekend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	req.SetPathValue("modelID", "weekend")
	w := httptest.NewRecorder()
	HandleUpdateModel(db).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%q", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "/controls/mode?model=vacation" {
		t.Fatalf("unexpected redirect: %q", location)
	}
	models, err := storage.ListModels(ctx, db, "mode")
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	foundVacation := false
	for _, model := range models {
		if model.ModelID == "vacation" {
			foundVacation = true
		}
	}
	if !foundVacation {
		t.Fatalf("unexpected models after update: %+v", models)
	}
	keys, err := storage.ListAggregateKeys(ctx, db, "mode")
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 || keys[0].ModelID != "vacation" {
		t.Fatalf("expected aggregate keys to follow renamed model, got %+v", keys)
	}
}

// TestCreateModelRejectsReservedNewID verifies the reserved create-page segment cannot be stored as a model ID.
func TestCreateModelRejectsReservedNewID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	form := url.Values{"modelId": {"new"}}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode/models/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleCreateModel(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid modelId") {
		t.Fatalf("expected validation error, got %q", w.Body.String())
	}
}

// TestUpdateModelShowsErrorsOnSubmittedRow verifies update failures stay attached to the edited row.
func TestUpdateModelShowsErrorsOnSubmittedRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := storage.SaveModel(ctx, db, "mode", "", storage.Model{ModelID: "weekend"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "vacation",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	form := url.Values{"modelId": {"vacation"}}
	req := httptest.NewRequest(http.MethodPost, "/controls/mode/models/weekend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("controlID", "mode")
	req.SetPathValue("modelID", "weekend")
	w := httptest.NewRecorder()
	HandleUpdateModel(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `action="/controls/mode/models/weekend"`) || !strings.Contains(body, `value="vacation"`) {
		t.Fatalf("expected submitted draft to stay on the edited row, got %q", body)
	}
	if strings.Contains(body, `<span>New model ID</span></label><input type="text" name="modelId" value="vacation"`) {
		t.Fatalf("expected new-model form to stay empty, got %q", body)
	}
}

// TestControlPageReturns500WhenModelLookupFails verifies model-list storage failures are not silently ignored.
func TestControlPageReturns500WhenModelLookupFails(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE models`); err != nil {
		t.Fatalf("drop models: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls/mode", nil)
	req.SetPathValue("controlID", "mode")
	w := httptest.NewRecorder()
	HandleControlPage(db).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%q", w.Code, w.Body.String())
	}
}

// TestSnapshotPageRendersEmpty verifies the snapshot page shows an empty state when no files exist.
func TestSnapshotPageRendersEmpty(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	HandleSnapshotPage(dir).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No snapshots") {
		t.Fatalf("expected empty state message")
	}
}

// TestSnapshotPageRendersList verifies the snapshot page renders existing snapshot filenames.
func TestSnapshotPageRendersList(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "snapshot-20260101-120000.sqlite"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Close()

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	HandleSnapshotPage(dir).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "snapshot-20260101-120000.sqlite") {
		t.Fatalf("expected snapshot filename in body")
	}
}

// TestSnapshotPageTruncatesLongLists verifies the snapshot page limits long listings and reports omitted entries.
func TestSnapshotPageTruncatesLongLists(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 52; i++ {
		name := filepath.Join(dir, fmt.Sprintf("snapshot-%02d.sqlite", i))
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		f.Close()
	}

	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	w := httptest.NewRecorder()
	HandleSnapshotPage(dir).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "... and 2 more") {
		t.Fatalf("expected truncation message, got body %q", body)
	}
	if strings.Contains(body, "snapshot-00.sqlite") || strings.Contains(body, "snapshot-01.sqlite") {
		t.Fatalf("expected oldest snapshots to be truncated from the rendered list")
	}
}

// TestHeatmapPartialDoesNotCreateMissingAggregate verifies empty heatmap requests do not create new aggregates.
func TestHeatmapPartialDoesNotCreateMissingAggregate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	start := time.Date(2020, 1, 6, 0, 1, 0, 0, time.UTC)
	key := storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "default",
		QuarterIndex: domain.QuarterIndexUTC(start.UnixMilli()),
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/partials/heatmap?controlId=mode&quarter=999", nil)
	w := httptest.NewRecorder()
	HandleHeatmapPartial(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No data for this quarter.") {
		t.Fatalf("expected empty heatmap message")
	}

	keys, err := storage.ListAggregateKeys(ctx, db, "mode")
	if err != nil {
		t.Fatalf("list aggregate keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected existing aggregate count to remain 1, got %d", len(keys))
	}
	if keys[0].QuarterIndex != key.QuarterIndex {
		t.Fatalf("unexpected aggregate created: %+v", keys)
	}
}

// TestHeatmapPartialReturns500OnControlLookupError verifies heatmap rendering fails closed on storage errors.
func TestHeatmapPartialReturns500OnControlLookupError(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	req := httptest.NewRequest(http.MethodGet, "/partials/heatmap?controlId=mode&quarter=1", nil)
	w := httptest.NewRecorder()
	HandleHeatmapPartial(db).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
