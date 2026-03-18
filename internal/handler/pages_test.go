package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func TestHomePageRendersControls(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "light", ControlType: storage.ControlTypeDiscrete, NumStates: 3,
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
}

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
}

func TestControlPageReturns200(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
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
}

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
