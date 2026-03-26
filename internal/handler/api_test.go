package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
