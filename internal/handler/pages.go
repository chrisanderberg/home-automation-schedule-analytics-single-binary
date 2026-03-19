package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/snapshot"
	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/view"
)

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

func HandleControlPage(db *sql.DB) http.HandlerFunc {
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

		data := view.ControlPageData{
			ControlID:   control.ControlID,
			ControlType: string(control.ControlType),
			NumStates:   control.NumStates,
			StateLabels: control.StateLabels,
		}

		quarterSet := make(map[int]string)
		var modelID string
		for _, k := range keys {
			if modelID == "" {
				modelID = k.ModelID
			}
			if _, ok := quarterSet[k.QuarterIndex]; !ok {
				quarterSet[k.QuarterIndex] = quarterLabel(k.QuarterIndex)
			}
		}
		data.ModelID = modelID

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

		if modelID != "" && selectedQuarter >= 0 {
			data.BucketJSON = buildBucketJSON(r.Context(), db, controlID, modelID, selectedQuarter, control.NumStates)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := view.ControlPage(data).Render(r.Context(), w); err != nil {
			log.Printf("render control: %v", err)
		}
	}
}

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

func HandleHeatmapPartial(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controlID := r.URL.Query().Get("controlId")
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
		var modelID string
		for _, k := range keys {
			modelID = k.ModelID
			break
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

	// Sum holding times across all states for the UTC clock to produce a 2016-element array
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

func quarterLabel(qi int) string {
	year := 1970 + qi/4
	q := qi%4 + 1
	return fmt.Sprintf("%d Q%d", year, q)
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}
