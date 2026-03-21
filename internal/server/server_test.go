package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/config"
	"home-automation-schedule-analytics-single-bin/internal/server"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		TimeZone:  "UTC",
		Location:  time.UTC,
		Latitude:  59.33,
		Longitude: 18.07,
		DBPath:    t.TempDir() + "/data.sqlite",
		Port:      "8080",
	}
	handler, err := server.New(nil, cfg)
	if err != nil {
		t.Fatalf("server.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := handler.Close(); err != nil {
			t.Fatalf("handler.Close() error = %v", err)
		}
	})
	return handler
}

func TestAPIFlow(t *testing.T) {
	t.Parallel()

	handler := newHandler(t)

	postJSON(t, handler, "/api/v1/controls", map[string]any{
		"controlId":   "lamp",
		"controlType": "discrete",
		"numStates":   2,
	}, http.StatusCreated)

	ts := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	postJSON(t, handler, "/api/v1/holding-intervals", map[string]any{
		"controlId":   "lamp",
		"state":       1,
		"startTimeMs": ts.UnixMilli(),
		"endTimeMs":   ts.Add(5 * time.Minute).UnixMilli(),
	}, http.StatusAccepted)
	postJSON(t, handler, "/api/v1/transitions", map[string]any{
		"controlId":   "lamp",
		"fromState":   0,
		"toState":     1,
		"timestampMs": ts.Add(10 * time.Minute).UnixMilli(),
	}, http.StatusAccepted)

	body := postJSON(t, handler, "/api/v1/snapshots", map[string]any{
		"name": "daily-export",
	}, http.StatusCreated)
	if !strings.Contains(body, "daily-export") {
		t.Fatalf("snapshot response = %s", body)
	}
}

func TestHTMLPagesAndPartials(t *testing.T) {
	t.Parallel()

	handler := newHandler(t)
	postJSON(t, handler, "/api/v1/controls", map[string]any{
		"controlId":   "lamp",
		"controlType": "discrete",
		"numStates":   2,
	}, http.StatusCreated)
	ts := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	postJSON(t, handler, "/api/v1/holding-intervals", map[string]any{
		"controlId":   "lamp",
		"state":       1,
		"startTimeMs": ts.UnixMilli(),
		"endTimeMs":   ts.Add(5 * time.Minute).UnixMilli(),
	}, http.StatusAccepted)

	homeResp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK)
	if !strings.Contains(homeResp.Body.String(), "lamp") {
		t.Fatalf("home page missing control: %s", homeResp.Body.String())
	}

	controlResp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/controls/lamp", nil), http.StatusOK)
	if !strings.Contains(controlResp.Body.String(), "heatmap-canvas") {
		t.Fatalf("control page missing heatmap: %s", controlResp.Body.String())
	}

	partialResp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/controls/lamp/heatmap?quarter=224&clock=utc&metric=holding", nil), http.StatusOK)
	if !strings.Contains(partialResp.Body.String(), "data-heatmap-values") {
		t.Fatalf("heatmap partial missing values: %s", partialResp.Body.String())
	}

	snapshotsResp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/snapshots", nil), http.StatusOK)
	if !strings.Contains(snapshotsResp.Body.String(), "Create Snapshot") {
		t.Fatalf("snapshots page missing form: %s", snapshotsResp.Body.String())
	}
}

func TestInvalidRequestsReturnBadRequest(t *testing.T) {
	t.Parallel()

	handler := newHandler(t)

	body := postJSON(t, handler, "/api/v1/controls", map[string]any{
		"controlId":   "dimmer",
		"controlType": "slider",
		"numStates":   5,
	}, http.StatusBadRequest)
	if !strings.Contains(body, "slider controls must have 6 states") {
		t.Fatalf("bad control response = %s", body)
	}

	body = postJSON(t, handler, "/api/v1/snapshots", map[string]any{
		"name": "   ",
	}, http.StatusBadRequest)
	if !strings.Contains(body, "snapshot name is required") {
		t.Fatalf("bad snapshot response = %s", body)
	}

	resp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/controls/missing/heatmap?clock=bad", nil), http.StatusNotFound)
	if strings.Contains(resp.Body.String(), "internal server error") {
		t.Fatalf("unexpected server error body = %s", resp.Body.String())
	}
}

func TestInvalidHeatmapParamsReturnBadRequest(t *testing.T) {
	t.Parallel()

	handler := newHandler(t)
	postJSON(t, handler, "/api/v1/controls", map[string]any{
		"controlId":   "lamp",
		"controlType": "discrete",
		"numStates":   2,
	}, http.StatusCreated)

	body := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/controls/lamp/heatmap?clock=bad", nil), http.StatusBadRequest).Body.String()
	if !strings.Contains(body, "invalid clock") {
		t.Fatalf("bad clock response = %s", body)
	}

	body = doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/controls/lamp/heatmap?metric=bad", nil), http.StatusBadRequest).Body.String()
	if !strings.Contains(body, "invalid metric") {
		t.Fatalf("bad metric response = %s", body)
	}
}

func TestRootRouteDoesNotMatchOtherPaths(t *testing.T) {
	t.Parallel()

	handler := newHandler(t)
	resp := doRequest(t, handler, httptest.NewRequest(http.MethodGet, "/not-found", nil), http.StatusNotFound)
	if strings.Contains(resp.Body.String(), "Create Snapshot") || strings.Contains(resp.Body.String(), "Controls") {
		t.Fatalf("unexpected root content for non-root path: %s", resp.Body.String())
	}
}

func postJSON(t *testing.T, handler http.Handler, path string, payload map[string]any, wantStatus int) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, handler, req, wantStatus)
	return resp.Body.String()
}

func doRequest(t *testing.T, handler http.Handler, req *http.Request, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", req.Method, req.URL.String(), rr.Code, wantStatus, rr.Body.String())
	}
	return rr
}
