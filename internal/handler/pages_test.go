package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain"
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

func TestHomePageEscapesControlLinks(t *testing.T) {
	db := openTestDB(t)
	if err := storage.UpsertControl(context.Background(), db, storage.Control{
		ControlID: "mode/scene", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
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

func TestControlPageSelectsLatestQuarterAndOrdersOptions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
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
	if !strings.Contains(body, `class="quarter-btn selected"`) || !strings.Contains(body, `hx-get="/partials/heatmap?controlId=mode&amp;quarter=12"`) {
		t.Fatalf("expected latest quarter to be selected")
	}
}

func TestControlPageEscapesQuarterRequests(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode/scene", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
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
	if !strings.Contains(w.Body.String(), `hx-get="/partials/heatmap?controlId=mode%2Fscene&amp;quarter=12"`) {
		t.Fatalf("expected escaped quarter request, got body %q", w.Body.String())
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

func TestHeatmapPartialDoesNotCreateMissingAggregate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
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
