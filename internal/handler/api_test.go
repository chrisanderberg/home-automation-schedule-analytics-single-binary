package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/demodata"
	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

// openTestDB creates an in-memory handler test database with the application schema loaded.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := storage.InitSchema(context.Background(), db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// testConfig returns a minimal ingest configuration suitable for handler tests.
func testConfig() ingest.Config {
	return ingest.Config{TimeZone: "UTC", Latitude: 0, Longitude: 0}
}

// postJSON sends a JSON POST request to a handler and captures the response.
func postJSON(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// postRaw sends a raw JSON body to a handler and captures the response.
func postRaw(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// TestHealthReturns200 verifies the health endpoint responds successfully.
func TestHealthReturns200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	HandleHealth().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestControlsAccepted verifies valid control payloads are accepted and persisted.
func TestControlsAccepted(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "light", "controlType": "radio buttons", "numStates": 3}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	c, err := storage.GetControl(context.Background(), db, "light")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	if c.NumStates != 3 {
		t.Fatalf("expected 3 states, got %d", c.NumStates)
	}
	if c.ControlType != storage.ControlTypeRadioButtons {
		t.Fatalf("expected normalized control type %q, got %q", storage.ControlTypeRadioButtons, c.ControlType)
	}
}

// TestControlsRejectsMissingFields verifies controls requests fail when required fields are missing.
func TestControlsRejectsMissingFields(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlType": "radio buttons", "numStates": 3}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectReservedNewControlID verifies the reserved UI route segment cannot be stored as a control ID.
func TestControlsRejectReservedNewControlID(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "new", "controlType": "radio buttons", "numStates": 2}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectEmptyBody verifies an empty request body is rejected as invalid JSON.
func TestControlsRejectEmptyBody(t *testing.T) {
	db := openTestDB(t)
	w := postRaw(HandleControls(db), "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectTrailingJSON verifies the API rejects multiple concatenated JSON values.
func TestControlsRejectTrailingJSON(t *testing.T) {
	db := openTestDB(t)
	w := postRaw(HandleControls(db), `{"controlId":"light","controlType":"radio buttons","numStates":3}{"extra":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsAcceptSingleJSONValueWithTrailingWhitespace verifies trailing whitespace does not invalidate a single JSON payload.
func TestControlsAcceptSingleJSONValueWithTrailingWhitespace(t *testing.T) {
	db := openTestDB(t)
	w := postRaw(HandleControls(db), "{\n\"controlId\":\"light\",\"controlType\":\"radio buttons\",\"numStates\":3\n}\n")
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

// TestControlsRejectsInvalidNumStates verifies controls validation enforces the supported state range.
func TestControlsRejectsInvalidNumStates(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "radio buttons", "numStates": 1}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectsBadControlType verifies unknown control types are rejected.
func TestControlsRejectsBadControlType(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "invalid", "numStates": 2}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectsStateLabelsMismatch verifies provided state labels must match the configured state count.
func TestControlsRejectsStateLabelsMismatch(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "radio buttons", "numStates": 2, "stateLabels": []string{"a"}}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectsSlidersWithNonSixStates verifies sliders keep their fixed six-state contract.
func TestControlsRejectsSliderWithNonSixStates(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "slider", "controlType": "sliders", "numStates": 5}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestControlsRejectStructuralChangeWithAggregates verifies the API cannot change blob shape once data exists.
func TestControlsRejectStructuralChangeWithAggregates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "default",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	body := map[string]any{"controlId": "mode", "controlType": "radio buttons", "numStates": 3}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	control, err := storage.GetControl(ctx, db, "mode")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	if control.NumStates != 2 {
		t.Fatalf("expected control to keep 2 states, got %d", control.NumStates)
	}
	data, err := storage.GetAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "default",
		QuarterIndex: 12,
	}, 2)
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected existing aggregate data to remain readable")
	}
}

// TestControlsCanUpdateLabelsWithAggregates verifies non-structural API edits still succeed once aggregates exist.
func TestControlsCanUpdateLabelsWithAggregates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
		StateLabels: []string{"off", "on"},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, storage.AggregateKey{
		ControlID:    "mode",
		ModelID:      "default",
		QuarterIndex: 12,
	}, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	body := map[string]any{"controlId": "mode", "controlType": "radio buttons", "numStates": 2, "stateLabels": []string{"cool", "warm"}}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	control, err := storage.GetControl(ctx, db, "mode")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	if control.StateLabels[0] != "cool" || control.StateLabels[1] != "warm" {
		t.Fatalf("expected labels update, got %+v", control.StateLabels)
	}
}

// TestHoldingAccepted verifies valid holding ingest requests are accepted.
func TestHoldingAccepted(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "state": 0, "startTimeMs": 1000, "endTimeMs": 2000}
	w := postJSON(HandleHolding(db, testConfig()), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHoldingRejectsUnknownControl verifies holding ingest returns a client error for unknown controls.
func TestHoldingRejectsUnknownControl(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "nope", "modelId": "m", "state": 0, "startTimeMs": 1000, "endTimeMs": 2000}
	w := postJSON(HandleHolding(db, testConfig()), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestTransitionAccepted verifies valid transition ingest requests are accepted.
func TestTransitionAccepted(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeRadioButtons, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "fromState": 0, "toState": 2, "timestampMs": 1000}
	w := postJSON(HandleTransitions(db, testConfig()), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTransitionRejectsSelfTransition verifies self-transitions are rejected at the handler boundary.
func TestTransitionRejectsSelfTransition(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeRadioButtons, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "fromState": 1, "toState": 1, "timestampMs": 1000}
	w := postJSON(HandleTransitions(db, testConfig()), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAnalyticsReturnsStructuredClockReport verifies analytics can be retrieved as JSON for one clock.
func TestAnalyticsReturnsStructuredClockReport(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
		StateLabels: []string{"off", "on"},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}
	if err := storage.UpdateAggregate(ctx, db, key, 2, func(blob []byte) error {
		b, err := domain.NewBlob(2)
		if err != nil {
			return err
		}
		copy(b.Data(), blob)
		idx, err := domain.HoldIndex(0, domain.ClockUTC, 0, 2)
		if err != nil {
			return err
		}
		if err := b.SetU64(idx, 300000); err != nil {
			return err
		}
		copy(blob, b.Data())
		return nil
	}); err != nil {
		t.Fatalf("update aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics?controlId=mode&modelId=weekday&quarter=12&clock=utc", nil)
	w := httptest.NewRecorder()
	HandleAnalytics(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"clockSlug":"utc"`) || !strings.Contains(body, `"quarterLabel":"1973 Q1"`) {
		t.Fatalf("expected analytics clock payload, got %q", body)
	}
}

// TestAnalyticsRawReturnsStoredBucketData verifies the raw analytics endpoint returns exact stored counters.
func TestAnalyticsRawReturnsStoredBucketData(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}
	if err := storage.UpdateAggregate(ctx, db, key, 2, func(blob []byte) error {
		b, err := domain.NewBlob(2)
		if err != nil {
			return err
		}
		copy(b.Data(), blob)
		holdIdx, err := domain.HoldIndex(0, domain.ClockUTC, 0, 2)
		if err != nil {
			return err
		}
		if err := b.SetU64(holdIdx, 300000); err != nil {
			return err
		}
		transIdx, err := domain.TransIndex(0, 1, domain.ClockUTC, 0, 2)
		if err != nil {
			return err
		}
		if err := b.SetU64(transIdx, 2); err != nil {
			return err
		}
		copy(blob, b.Data())
		return nil
	}); err != nil {
		t.Fatalf("update aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/raw?controlId=mode&modelId=weekday&quarter=12&clock=utc", nil)
	w := httptest.NewRecorder()
	HandleAnalyticsRaw(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"holdingMillis"`) || !strings.Contains(body, `"transitionCounts"`) {
		t.Fatalf("expected raw series in payload, got %q", body)
	}
	if !strings.Contains(body, `"buckets":[300000`) || !strings.Contains(body, `"buckets":[2`) {
		t.Fatalf("expected exact stored counters, got %q", body)
	}
}

// TestAnalyticsRawWithoutClockReturnsAllClocks verifies the raw endpoint returns the full clock list when unfiltered.
func TestAnalyticsRawWithoutClockReturnsAllClocks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	control := storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/raw?controlId=mode&modelId=weekday&quarter=12", nil)
	w := httptest.NewRecorder()
	HandleAnalyticsRaw(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	if count := strings.Count(w.Body.String(), `"clockSlug":`); count != domain.Clocks {
		t.Fatalf("expected %d clocks, got %d body=%q", domain.Clocks, count, w.Body.String())
	}
}

// TestAnalyticsReportSupportsBypassingSmoothingAndDamping verifies report knobs can disable those transforms.
func TestAnalyticsReportSupportsBypassingSmoothingAndDamping(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}
	if err := storage.UpdateAggregate(ctx, db, key, 2, func(blob []byte) error {
		b, err := domain.NewBlob(2)
		if err != nil {
			return err
		}
		copy(b.Data(), blob)
		holdIdx, err := domain.HoldIndex(0, domain.ClockUTC, 0, 2)
		if err != nil {
			return err
		}
		if err := b.SetU64(holdIdx, 300000); err != nil {
			return err
		}
		copy(blob, b.Data())
		return nil
	}); err != nil {
		t.Fatalf("update aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/report?controlId=mode&modelId=weekday&quarter=12&clock=utc&smoothing=none&holdingDampingMillis=none&transitionDampingCount=none&include=raw,rates", nil)
	w := httptest.NewRecorder()
	HandleAnalyticsReport(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}

	var body struct {
		Parameters struct {
			Smoothing struct {
				Kind string `json:"kind"`
			} `json:"smoothing"`
			Damping struct {
				HoldingMillis   float64 `json:"holdingMillis"`
				TransitionCount float64 `json:"transitionCount"`
			} `json:"damping"`
		} `json:"parameters"`
		Clock struct {
			Intermediates struct {
				RawHoldingMillis []struct {
					Buckets []uint64 `json:"buckets"`
				} `json:"rawHoldingMillis"`
				TransitionRates []struct {
					Buckets []float64 `json:"buckets"`
				} `json:"transitionRates"`
			} `json:"intermediates"`
		} `json:"clock"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Parameters.Smoothing.Kind != "none" {
		t.Fatalf("expected no smoothing, got %+v", body.Parameters.Smoothing)
	}
	if body.Parameters.Damping.HoldingMillis != 0 || body.Parameters.Damping.TransitionCount != 0 {
		t.Fatalf("expected zero damping, got %+v", body.Parameters.Damping)
	}
	if len(body.Clock.Intermediates.RawHoldingMillis) == 0 || body.Clock.Intermediates.RawHoldingMillis[0].Buckets[0] != 300000 {
		t.Fatalf("expected raw holding intermediates, got %+v", body.Clock.Intermediates.RawHoldingMillis)
	}
	if len(body.Clock.Intermediates.TransitionRates) == 0 {
		t.Fatalf("expected rate intermediates in payload")
	}
	if math.Abs(body.Clock.Intermediates.TransitionRates[0].Buckets[0]) > 1e-9 {
		t.Fatalf("expected zero rate without transitions or damping, got %+v", body.Clock.Intermediates.TransitionRates[0].Buckets[:1])
	}
}

// TestAnalyticsReportRejectsKernelParamsWithoutGaussian verifies invalid option combinations fail fast.
func TestAnalyticsReportRejectsKernelParamsWithoutGaussian(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/report?controlId=mode&modelId=weekday&quarter=12&smoothing=none&kernelRadius=6", nil)
	w := httptest.NewRecorder()
	HandleAnalyticsReport(db).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%q", w.Code, w.Body.String())
	}
}

// TestAnalyticsReportAcceptsCommaSeparatedInclude verifies include values can be passed in one comma-separated query string.
func TestAnalyticsReportAcceptsCommaSeparatedInclude(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeRadioButtons, NumStates: 2,
		StateLabels: []string{"off", "on"},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, 2); err != nil {
		t.Fatalf("seed aggregate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/report?controlId=mode&modelId=weekday&quarter=12&clock=utc&include=raw,smoothed,rates", nil)
	w := httptest.NewRecorder()
	HandleAnalyticsReport(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"rawHoldingMillis"`) || !strings.Contains(body, `"smoothedHoldingMillis"`) || !strings.Contains(body, `"transitionRates"`) {
		t.Fatalf("expected all include sections, got %q", body)
	}
}

// TestAnalyticsRejectsMissingQueryParams verifies analytics requires a fully specified analytical slice.
func TestAnalyticsRejectsMissingQueryParams(t *testing.T) {
	db := openTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics?controlId=mode", nil)
	w := httptest.NewRecorder()
	HandleAnalytics(db).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%q", w.Code, w.Body.String())
	}
}

// TestAnalyticsReturnsSeededDemoScenario verifies the analytics endpoint surfaces seeded weekday/weekend report structure.
func TestAnalyticsReturnsSeededDemoScenario(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics?controlId=living-room-scene&modelId="+demodata.DefaultModelID+"&quarter="+fmt.Sprintf("%d", demodata.DefaultQuarterIndex)+"&clock=utc", nil)
	w := httptest.NewRecorder()
	HandleAnalytics(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"controlId":"living-room-scene"`) || !strings.Contains(body, `"clockSlug":"utc"`) {
		t.Fatalf("expected seeded analytics payload, got %q", body)
	}
	if !strings.Contains(body, `"label":"bright"`) {
		t.Fatalf("expected state labels in analytics payload, got %q", body)
	}
}

// TestSnapshotExportsFile verifies the snapshot endpoint writes a file and returns its filename.
func TestSnapshotExportsFile(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	snapDir := filepath.Join(dir, "snapshots")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	HandleSnapshots(db, snapDir).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["snapshotFilename"] == "" {
		t.Fatalf("expected snapshot filename in response")
	}
	if _, err := os.Stat(filepath.Join(snapDir, resp["snapshotFilename"])); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
}
