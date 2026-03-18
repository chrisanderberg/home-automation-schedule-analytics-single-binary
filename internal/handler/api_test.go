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

func testConfig() ingest.Config {
	return ingest.Config{TimeZone: "UTC", Latitude: 0, Longitude: 0}
}

func postJSON(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestHealthReturns200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	HandleHealth().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestControlsAccepted(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "light", "controlType": "discrete", "numStates": 3}
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

func TestControlsRejectsMissingFields(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlType": "discrete", "numStates": 3}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestControlsRejectsInvalidNumStates(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "discrete", "numStates": 1}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestControlsRejectsBadControlType(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "invalid", "numStates": 2}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestControlsRejectsStateLabelsMismatch(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "x", "controlType": "discrete", "numStates": 2, "stateLabels": []string{"a"}}
	w := postJSON(HandleControls(db), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHoldingAccepted(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "state": 0, "startTimeMs": 1000, "endTimeMs": 2000}
	w := postJSON(HandleHolding(db, testConfig()), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHoldingRejectsUnknownControl(t *testing.T) {
	db := openTestDB(t)
	body := map[string]any{"controlId": "nope", "modelId": "m", "state": 0, "startTimeMs": 1000, "endTimeMs": 2000}
	w := postJSON(HandleHolding(db, testConfig()), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTransitionAccepted(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeDiscrete, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "fromState": 0, "toState": 2, "timestampMs": 1000}
	w := postJSON(HandleTransitions(db, testConfig()), body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTransitionRejectsSelfTransition(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeDiscrete, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	body := map[string]any{"controlId": "c", "modelId": "m", "fromState": 1, "toState": 1, "timestampMs": 1000}
	w := postJSON(HandleTransitions(db, testConfig()), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

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
	if _, err := os.Stat(resp["snapshotPath"]); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
}
