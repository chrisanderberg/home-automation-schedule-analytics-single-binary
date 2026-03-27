package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/analytics"
	"home-automation-schedule-analytics-single-bin/internal/analyticstest"
)

// TestAnalyticsGoldenFixtures verifies the raw and report endpoints against deterministic fixture scenarios.
func TestAnalyticsGoldenFixtures(t *testing.T) {
	fixtures := analyticstest.MustLoadFixtures(t, filepath.Join("..", "..", "testdata", "analytics"))
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			db := openTestDB(t)
			analyticstest.SeedFixture(t, db, fixture)

			rawURL := analyticsRawFixtureURL(fixture)
			rawRecorder := getRecorder(t, HandleAnalyticsRaw(db), rawURL)
			requireStatus(t, rawRecorder.Code, rawRecorder.Body.String(), 200)

			var rawResponse struct {
				Clock analytics.RawClockReport `json:"clock"`
			}
			rawResponse = analyticstest.DecodeJSONResponse[struct {
				Clock analytics.RawClockReport `json:"clock"`
			}](t, rawRecorder.Body.Bytes())
			requireFixtureRawMatchesSeed(t, fixture, rawResponse.Clock)

			for _, request := range fixture.ReportRequests {
				request := request
				t.Run(request.Name, func(t *testing.T) {
					reportURL := analyticsReportFixtureURL(fixture, request.Query)
					reportRecorder := getRecorder(t, HandleAnalyticsReport(db), reportURL)
					requireStatus(t, reportRecorder.Code, reportRecorder.Body.String(), 200)

					response := analyticstest.DecodeJSONResponse[struct {
						Parameters analytics.ReportParameters   `json:"parameters"`
						Clock      analytics.DerivedClockReport `json:"clock"`
					}](t, reportRecorder.Body.Bytes())
					requireFixtureReportExpectations(t, request.Expect, response.Clock)
					requireFixtureReportParametersEchoed(t, request.Query, response.Parameters)
				})
			}
		})
	}
}

func analyticsRawFixtureURL(fixture analyticstest.Fixture) string {
	return fmt.Sprintf(
		"/api/v1/analytics/raw?controlId=%s&modelId=%s&quarter=%d&clock=utc",
		url.QueryEscape(fixture.Control.ControlID),
		url.QueryEscape(fixture.Aggregate.ModelID),
		fixture.Aggregate.QuarterIndex,
	)
}

func analyticsReportFixtureURL(fixture analyticstest.Fixture, query analyticstest.QueryFixture) string {
	values := url.Values{}
	values.Set("controlId", fixture.Control.ControlID)
	values.Set("modelId", fixture.Aggregate.ModelID)
	values.Set("quarter", fmt.Sprintf("%d", fixture.Aggregate.QuarterIndex))
	if query.Clock != "" {
		values.Set("clock", query.Clock)
	}
	if query.Smoothing != "" {
		values.Set("smoothing", query.Smoothing)
	}
	if query.KernelRadius != "" {
		values.Set("kernelRadius", query.KernelRadius)
	}
	if query.KernelSigma != "" {
		values.Set("kernelSigma", query.KernelSigma)
	}
	if query.HoldingDampingMillis != "" {
		values.Set("holdingDampingMillis", query.HoldingDampingMillis)
	}
	if query.TransitionDampingCount != "" {
		values.Set("transitionDampingCount", query.TransitionDampingCount)
	}
	for _, include := range query.Include {
		values.Add("include", include)
	}
	return "/api/v1/analytics/report?" + values.Encode()
}

func requireFixtureRawMatchesSeed(t *testing.T, fixture analyticstest.Fixture, clock analytics.RawClockReport) {
	t.Helper()
	for _, holding := range fixture.Aggregate.Holdings {
		if holding.Clock != "" && holding.Clock != clock.ClockSlug {
			continue
		}
		if got := clock.HoldingMillis[holding.State].Buckets[holding.Bucket]; got != holding.Value {
			t.Fatalf("fixture %s holding mismatch state=%d bucket=%d: got %d want %d", fixture.Name, holding.State, holding.Bucket, got, holding.Value)
		}
	}
	for _, transition := range fixture.Aggregate.Transitions {
		if transition.Clock != "" && transition.Clock != clock.ClockSlug {
			continue
		}
		found := false
		for _, series := range clock.TransitionCounts {
			if series.FromState == transition.FromState && series.ToState == transition.ToState {
				found = true
				if got := series.Buckets[transition.Bucket]; got != transition.Value {
					t.Fatalf("fixture %s transition mismatch from=%d to=%d bucket=%d: got %d want %d", fixture.Name, transition.FromState, transition.ToState, transition.Bucket, got, transition.Value)
				}
			}
		}
		if !found {
			t.Fatalf("fixture %s missing transition series from=%d to=%d", fixture.Name, transition.FromState, transition.ToState)
		}
	}
}

func requireFixtureReportExpectations(t *testing.T, expect analyticstest.ReportExpectations, clock analytics.DerivedClockReport) {
	t.Helper()
	if expect.OccupancyBucketSumsToOne {
		analyticstest.RequireNormalizedSeries(t, collectStateSeries(clock.OccupancySeries), 1e-6)
	}
	if expect.PreferenceBucketSumsToOne {
		analyticstest.RequireNormalizedSeries(t, collectStateSeries(clock.PreferenceSeries), 1e-6)
	}
	if expect.FallbackBuckets != nil {
		if clock.Diagnostics == nil || clock.Diagnostics.FallbackBuckets != *expect.FallbackBuckets {
			t.Fatalf("expected fallback buckets %d, got %+v", *expect.FallbackBuckets, clock.Diagnostics)
		}
	}
	for _, exact := range expect.ExactSeries {
		got := fixtureSeriesValue(t, clock, exact)
		tol := exact.Tolerance
		if tol == 0 {
			tol = 1e-6
		}
		analyticstest.RequireApproxEqual(t, got, exact.Want, tol, exact.Section)
	}
	for _, locator := range expect.PositiveSeries {
		if got := fixtureSeriesValue(t, clock, analyticstest.SeriesExpectation{
			Section: locator.Section, State: locator.State, FromState: locator.FromState, ToState: locator.ToState, Bucket: locator.Bucket,
		}); got <= 0 {
			t.Fatalf("expected positive series value for %+v, got %f", locator, got)
		}
	}
	for _, locator := range expect.ZeroSeries {
		if got := fixtureSeriesValue(t, clock, analyticstest.SeriesExpectation{
			Section: locator.Section, State: locator.State, FromState: locator.FromState, ToState: locator.ToState, Bucket: locator.Bucket,
		}); got != 0 {
			t.Fatalf("expected zero series value for %+v, got %f", locator, got)
		}
	}
}

func requireFixtureReportParametersEchoed(t *testing.T, query analyticstest.QueryFixture, params analytics.ReportParameters) {
	t.Helper()
	if query.Smoothing != "" && params.Smoothing.Kind != query.Smoothing {
		t.Fatalf("expected smoothing %q, got %+v", query.Smoothing, params.Smoothing)
	}
	if query.HoldingDampingMillis == "none" && params.Damping.HoldingMillis != 0 {
		t.Fatalf("expected zero holding damping, got %+v", params.Damping)
	}
	if query.TransitionDampingCount == "none" && params.Damping.TransitionCount != 0 {
		t.Fatalf("expected zero transition damping, got %+v", params.Damping)
	}
}

func collectStateSeries(series []analytics.Series) [][]float64 {
	values := make([][]float64, 0, len(series))
	for _, stateSeries := range series {
		values = append(values, stateSeries.Buckets)
	}
	return values
}

func fixtureSeriesValue(t *testing.T, clock analytics.DerivedClockReport, locator analyticstest.SeriesExpectation) float64 {
	t.Helper()
	switch locator.Section {
	case "occupancy":
		return clock.OccupancySeries[locator.State].Buckets[locator.Bucket]
	case "preference":
		return clock.PreferenceSeries[locator.State].Buckets[locator.Bucket]
	case "rate":
		if clock.Intermediates == nil {
			t.Fatalf("missing intermediates for rate assertion")
		}
		for _, series := range clock.Intermediates.TransitionRates {
			if series.FromState == locator.FromState && series.ToState == locator.ToState {
				return series.Buckets[locator.Bucket]
			}
		}
	case "smoothedHolding":
		if clock.Intermediates == nil {
			t.Fatalf("missing intermediates for smoothed holding assertion")
		}
		return clock.Intermediates.SmoothedHoldingMillis[locator.State].Buckets[locator.Bucket]
	case "smoothedTransition":
		if clock.Intermediates == nil {
			t.Fatalf("missing intermediates for smoothed transition assertion")
		}
		for _, series := range clock.Intermediates.SmoothedTransitionCount {
			if series.FromState == locator.FromState && series.ToState == locator.ToState {
				return series.Buckets[locator.Bucket]
			}
		}
	}
	t.Fatalf("unsupported or missing fixture series locator %+v", locator)
	return 0
}

func getRecorder(t *testing.T, handler func(http.ResponseWriter, *http.Request), target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func requireStatus(t *testing.T, got int, body string, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected %d, got %d body=%q", want, got, body)
	}
}
