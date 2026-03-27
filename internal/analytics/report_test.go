package analytics

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/demodata"
	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/testutil"
)

// setHoldValue seeds one holding counter inside a stored aggregate for report tests.
func setHoldValue(t *testing.T, ctx context.Context, db *sql.DB, key storage.AggregateKey, numStates, state, clock, bucket int, value uint64) {
	t.Helper()
	if err := storage.UpdateAggregate(ctx, db, key, numStates, func(blobData []byte) error {
		blob, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(blob.Data(), blobData)
		idx, err := domain.HoldIndex(state, clock, bucket, numStates)
		if err != nil {
			return err
		}
		if err := blob.SetU64(idx, value); err != nil {
			return err
		}
		copy(blobData, blob.Data())
		return nil
	}); err != nil {
		t.Fatalf("set hold value: %v", err)
	}
}

// setTransitionValue seeds one transition counter inside a stored aggregate for report tests.
func setTransitionValue(t *testing.T, ctx context.Context, db *sql.DB, key storage.AggregateKey, numStates, fromState, toState, clock, bucket int, value uint64) {
	t.Helper()
	if err := storage.UpdateAggregate(ctx, db, key, numStates, func(blobData []byte) error {
		blob, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(blob.Data(), blobData)
		idx, err := domain.TransIndex(fromState, toState, clock, bucket, numStates)
		if err != nil {
			return err
		}
		if err := blob.SetU64(idx, value); err != nil {
			return err
		}
		copy(blobData, blob.Data())
		return nil
	}); err != nil {
		t.Fatalf("set transition value: %v", err)
	}
}

// TestBuildReportReturnsNormalizedSeriesPerClock verifies each clock report returns normalized occupancy and preference series.
func TestBuildReportReturnsNormalizedSeriesPerClock(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}

	setHoldValue(t, ctx, db, key, 2, 0, domain.ClockUTC, 0, 270000)
	setHoldValue(t, ctx, db, key, 2, 1, domain.ClockUTC, 0, 30000)
	setTransitionValue(t, ctx, db, key, 2, 0, 1, domain.ClockUTC, 0, 4)
	setTransitionValue(t, ctx, db, key, 2, 1, 0, domain.ClockUTC, 0, 1)

	report, err := BuildReport(ctx, db, control, "weekday", 12)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if len(report.Clocks) != domain.Clocks {
		t.Fatalf("expected %d clocks, got %d", domain.Clocks, len(report.Clocks))
	}

	utc, err := report.ClockBySlug("utc")
	if err != nil {
		t.Fatalf("lookup utc clock: %v", err)
	}
	if utc.OccupancySeries[0].Raw[0] <= utc.OccupancySeries[1].Raw[0] {
		t.Fatalf("expected state 0 to dominate actual occupancy at bucket 0")
	}
	if utc.PreferenceSeries[0].Raw[0] <= 0 || utc.PreferenceSeries[1].Raw[0] <= 0 {
		t.Fatalf("expected inferred preference values to be populated")
	}

	occupancySum := utc.OccupancySeries[0].Raw[0] + utc.OccupancySeries[1].Raw[0]
	preferenceSum := utc.PreferenceSeries[0].Raw[0] + utc.PreferenceSeries[1].Raw[0]
	if math.Abs(occupancySum-1) > 1e-6 {
		t.Fatalf("expected normalized occupancy distribution, got %f", occupancySum)
	}
	if math.Abs(preferenceSum-1) > 1e-6 {
		t.Fatalf("expected normalized preference distribution, got %f", preferenceSum)
	}
}

// TestBuildReportRemainsModelScoped verifies reports only read aggregates from the selected model.
func TestBuildReportRemainsModelScoped(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	weekday := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	weekend := storage.AggregateKey{ControlID: "mode", ModelID: "weekend", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, weekday, control.NumStates); err != nil {
		t.Fatalf("create weekday aggregate: %v", err)
	}
	if _, err := storage.GetOrCreateAggregate(ctx, db, weekend, control.NumStates); err != nil {
		t.Fatalf("create weekend aggregate: %v", err)
	}

	setHoldValue(t, ctx, db, weekday, 2, 0, domain.ClockUTC, 0, 300000)
	setHoldValue(t, ctx, db, weekend, 2, 1, domain.ClockUTC, 0, 300000)

	weekdayReport, err := BuildReport(ctx, db, control, "weekday", 12)
	if err != nil {
		t.Fatalf("build weekday report: %v", err)
	}
	weekendReport, err := BuildReport(ctx, db, control, "weekend", 12)
	if err != nil {
		t.Fatalf("build weekend report: %v", err)
	}

	weekdayUTC, _ := weekdayReport.ClockBySlug("utc")
	weekendUTC, _ := weekendReport.ClockBySlug("utc")
	if weekdayUTC.OccupancySeries[0].Raw[0] <= weekdayUTC.OccupancySeries[1].Raw[0] {
		t.Fatalf("expected weekday aggregate to favor state 0")
	}
	if weekendUTC.OccupancySeries[1].Raw[0] <= weekendUTC.OccupancySeries[0].Raw[0] {
		t.Fatalf("expected weekend aggregate to favor state 1")
	}
}

// TestBuildReportFallsBackWhenBucketIsEmpty verifies empty buckets fall back to a uniform preference distribution.
func TestBuildReportFallsBackWhenBucketIsEmpty(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}

	report, err := BuildReport(ctx, db, control, "weekday", 12)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	utc, err := report.ClockBySlug("utc")
	if err != nil {
		t.Fatalf("lookup utc clock: %v", err)
	}
	if utc.Diagnostics.FallbackBuckets == 0 {
		t.Fatalf("expected empty aggregate to use fallback buckets")
	}
}

// TestSeededDemoReportShowsDistinctWeekdayWeekendPatterns verifies the seeded living-room demo produces different weekday and weekend preferences.
func TestSeededDemoReportShowsDistinctWeekdayWeekendPatterns(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	control, err := storage.GetControl(ctx, db, "living-room-scene")
	if err != nil {
		t.Fatalf("get control: %v", err)
	}
	quarterIndex := domain.QuarterIndexUTC(time.Date(2026, time.January, 5, 20, 0, 0, 0, time.UTC).UnixMilli())
	report, err := BuildReport(ctx, db, control, demodata.DefaultModelID, quarterIndex)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	utc, err := report.ClockBySlug("utc")
	if err != nil {
		t.Fatalf("lookup utc clock: %v", err)
	}

	weekdayBucket, err := domain.BucketAtUTC(time.Date(2026, time.January, 5, 20, 0, 0, 0, time.UTC).UnixMilli())
	if err != nil {
		t.Fatalf("weekday bucket: %v", err)
	}
	weekendBucket, err := domain.BucketAtUTC(time.Date(2026, time.January, 10, 20, 0, 0, 0, time.UTC).UnixMilli())
	if err != nil {
		t.Fatalf("weekend bucket: %v", err)
	}

	ambientWeekday := utc.OccupancySeries[1].Raw[weekdayBucket]
	brightWeekday := utc.OccupancySeries[2].Raw[weekdayBucket]
	if brightWeekday <= ambientWeekday {
		t.Fatalf("expected weekday evening bright occupancy to dominate ambient, got bright=%f ambient=%f", brightWeekday, ambientWeekday)
	}
	ambientWeekend := utc.OccupancySeries[1].Raw[weekendBucket]
	brightWeekend := utc.OccupancySeries[2].Raw[weekendBucket]
	if ambientWeekend <= brightWeekend {
		t.Fatalf("expected weekend evening ambient occupancy to dominate bright, got ambient=%f bright=%f", ambientWeekend, brightWeekend)
	}

	if utc.PreferenceSeries[2].Raw[weekdayBucket] <= utc.PreferenceSeries[1].Raw[weekdayBucket] {
		t.Fatalf("expected weekday inferred preference to favor bright over ambient")
	}
	if utc.PreferenceSeries[1].Raw[weekendBucket] <= utc.PreferenceSeries[2].Raw[weekendBucket] {
		t.Fatalf("expected weekend inferred preference to favor ambient over bright")
	}
}

// TestSeededDemoReportCapturesBedroomAndOfficePatterns verifies the seeded demo preserves the bedroom and office state patterns.
func TestSeededDemoReportCapturesBedroomAndOfficePatterns(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	cfg := ingest.Config{
		TimeZone:  "America/Los_Angeles",
		Latitude:  37.77,
		Longitude: -122.42,
	}
	if err := demodata.SeedDemoData(ctx, db, cfg); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	bedroom, err := storage.GetControl(ctx, db, "bedroom-mode")
	if err != nil {
		t.Fatalf("get bedroom control: %v", err)
	}
	quarterIndex := domain.QuarterIndexUTC(time.Date(2026, time.January, 5, 21, 0, 0, 0, time.UTC).UnixMilli())
	bedroomReport, err := BuildReport(ctx, db, bedroom, demodata.DefaultModelID, quarterIndex)
	if err != nil {
		t.Fatalf("build bedroom report: %v", err)
	}
	bedroomUTC, _ := bedroomReport.ClockBySlug("utc")
	weekdayBucket, _ := domain.BucketAtUTC(time.Date(2026, time.January, 5, 21, 0, 0, 0, time.UTC).UnixMilli())
	weekendBucket, _ := domain.BucketAtUTC(time.Date(2026, time.January, 10, 21, 0, 0, 0, time.UTC).UnixMilli())
	if bedroomUTC.OccupancySeries[1].Raw[weekdayBucket] <= bedroomUTC.OccupancySeries[2].Raw[weekdayBucket] {
		t.Fatalf("expected weekday bedroom occupancy to favor reading")
	}
	if bedroomUTC.OccupancySeries[2].Raw[weekendBucket] <= bedroomUTC.OccupancySeries[1].Raw[weekendBucket] {
		t.Fatalf("expected weekend bedroom occupancy to favor bright")
	}

	office, err := storage.GetControl(ctx, db, "office-dimmer")
	if err != nil {
		t.Fatalf("get office control: %v", err)
	}
	officeReport, err := BuildReport(ctx, db, office, demodata.DefaultModelID, quarterIndex)
	if err != nil {
		t.Fatalf("build office report: %v", err)
	}
	officeUTC, _ := officeReport.ClockBySlug("utc")
	workBucket, _ := domain.BucketAtUTC(time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC).UnixMilli())
	weekendWorkBucket, _ := domain.BucketAtUTC(time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC).UnixMilli())
	if officeUTC.PreferenceSeries[3].Raw[workBucket] <= officeUTC.PreferenceSeries[2].Raw[workBucket] {
		t.Fatalf("expected weekday office inferred preference to favor trans 3 at 10:00")
	}
	if officeUTC.PreferenceSeries[2].Raw[weekendWorkBucket] <= officeUTC.PreferenceSeries[3].Raw[weekendWorkBucket] {
		t.Fatalf("expected weekend office inferred preference to favor trans 2 at 10:00")
	}
}

// TestBuildRawReportReturnsStoredCounters verifies the raw report path exposes exact stored values without normalization.
func TestBuildRawReportReturnsStoredCounters(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert control: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	setHoldValue(t, ctx, db, key, 2, 0, domain.ClockUTC, 0, 300000)
	setTransitionValue(t, ctx, db, key, 2, 0, 1, domain.ClockUTC, 0, 2)

	report, err := BuildRawReport(ctx, db, control, "weekday", 12)
	if err != nil {
		t.Fatalf("build raw report: %v", err)
	}
	utc, err := report.ClockBySlug("utc")
	if err != nil {
		t.Fatalf("lookup utc clock: %v", err)
	}
	if got := utc.HoldingMillis[0].Buckets[0]; got != 300000 {
		t.Fatalf("expected raw holding millis 300000, got %d", got)
	}
	if got := utc.TransitionCounts[0].Buckets[0]; got != 2 {
		t.Fatalf("expected raw transition count 2, got %d", got)
	}
}

// TestSmoothSeriesNoneReturnsOriginal verifies disabling smoothing leaves the weekly series untouched.
func TestSmoothSeriesNoneReturnsOriginal(t *testing.T) {
	original := []float64{0, 3, 0, 0}
	got := smoothSeries(original, ReportOptions{SmoothingKind: SmoothingNone})
	if len(got) != len(original) {
		t.Fatalf("expected %d values, got %d", len(original), len(got))
	}
	for i := range original {
		if got[i] != original[i] {
			t.Fatalf("expected smoothing=none to keep value %d, got %f want %f", i, got[i], original[i])
		}
	}
}

// TestSmoothCyclicWrapsAcrossWeek verifies gaussian smoothing borrows mass from the opposite week edge.
func TestSmoothCyclicWrapsAcrossWeek(t *testing.T) {
	series := make([]float64, domain.BucketsPerWeek)
	series[0] = 1
	smoothed := smoothCyclic(series, 2, 1.0)
	if smoothed[domain.BucketsPerWeek-1] <= 0 {
		t.Fatalf("expected wrapped smoothing at final bucket, got %f", smoothed[domain.BucketsPerWeek-1])
	}
	if smoothed[1] <= 0 {
		t.Fatalf("expected smoothing spill into neighboring bucket, got %f", smoothed[1])
	}
}

// TestInferPreferenceWithoutDampingReturnsZeroRates verifies zero damping plus no transitions preserves zero transition rates.
func TestInferPreferenceWithoutDampingReturnsZeroRates(t *testing.T) {
	opts := ReportOptions{
		SmoothingKind:          SmoothingNone,
		HoldingDampingMillis:   0,
		TransitionDampingCount: 0,
	}
	preference, rates, fallback := inferPreference([]float64{300000, 0}, [][]float64{
		{0, 0},
		{0, 0},
	}, opts)
	if !fallback {
		t.Fatalf("expected fallback when no transitions exist with zero damping")
	}
	if preference[0] != 1 || preference[1] != 0 {
		t.Fatalf("expected occupancy-derived fallback, got %+v", preference)
	}
	if rates[0][1] != 0 || rates[1][0] != 0 {
		t.Fatalf("expected zero rates, got %+v", rates)
	}
}

// TestBuildDerivedReportFromRawReturnsCustomIntermediates verifies parameterized derived reports can expose smoothed intermediates.
func TestBuildDerivedReportFromRawReturnsCustomIntermediates(t *testing.T) {
	db := testutil.OpenTestDB(t, storage.Open, storage.InitSchema)
	ctx := context.Background()
	control := storage.Control{
		ControlID:   "mode",
		ControlType: storage.ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("upsert control: %v", err)
	}
	key := storage.AggregateKey{ControlID: "mode", ModelID: "weekday", QuarterIndex: 12}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	setHoldValue(t, ctx, db, key, 2, 0, domain.ClockUTC, 0, 300000)
	setHoldValue(t, ctx, db, key, 2, 0, domain.ClockUTC, domain.BucketsPerWeek-1, 300000)

	raw, err := BuildRawReport(ctx, db, control, "weekday", 12)
	if err != nil {
		t.Fatalf("build raw report: %v", err)
	}
	derived, err := BuildDerivedReportFromRaw(raw, ReportOptions{
		SmoothingKind:          SmoothingGaussian,
		KernelRadius:           2,
		KernelSigma:            1.0,
		HoldingDampingMillis:   defaultHoldingDampingMillis,
		TransitionDampingCount: defaultTransitionDampingCount,
		Include: IncludeOptions{
			Diagnostics: true,
			Smoothed:    true,
		},
	})
	if err != nil {
		t.Fatalf("build derived report: %v", err)
	}
	utc, err := derived.ClockBySlug("utc")
	if err != nil {
		t.Fatalf("lookup utc clock: %v", err)
	}
	if utc.Intermediates == nil || len(utc.Intermediates.SmoothedHoldingMillis) == 0 {
		t.Fatalf("expected smoothed intermediates")
	}
	if utc.Intermediates.SmoothedHoldingMillis[0].Buckets[1] <= 0 {
		t.Fatalf("expected smoothing spill into adjacent bucket, got %f", utc.Intermediates.SmoothedHoldingMillis[0].Buckets[1])
	}
	if utc.Intermediates.SmoothedHoldingMillis[0].Buckets[domain.BucketsPerWeek-2] <= 0 {
		t.Fatalf("expected smoothing wraparound at week edge, got %f", utc.Intermediates.SmoothedHoldingMillis[0].Buckets[domain.BucketsPerWeek-2])
	}
}
