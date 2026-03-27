package analyticstest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

type Fixture struct {
	Name             string           `json:"name"`
	Control          ControlFixture   `json:"control"`
	RegisteredModels []string         `json:"registeredModels,omitempty"`
	Aggregate        AggregateFixture `json:"aggregate"`
	ReportRequests   []ReportRequest  `json:"reportRequests,omitempty"`
}

type ControlFixture struct {
	ControlID   string   `json:"controlId"`
	ControlType string   `json:"controlType"`
	NumStates   int      `json:"numStates"`
	StateLabels []string `json:"stateLabels"`
}

type AggregateFixture struct {
	ModelID      string              `json:"modelId"`
	QuarterIndex int                 `json:"quarterIndex"`
	Holdings     []HoldingFixture    `json:"holdings,omitempty"`
	Transitions  []TransitionFixture `json:"transitions,omitempty"`
}

type HoldingFixture struct {
	Clock  string `json:"clock"`
	State  int    `json:"state"`
	Bucket int    `json:"bucket"`
	Value  uint64 `json:"value"`
}

type TransitionFixture struct {
	Clock     string `json:"clock"`
	FromState int    `json:"fromState"`
	ToState   int    `json:"toState"`
	Bucket    int    `json:"bucket"`
	Value     uint64 `json:"value"`
}

type ReportRequest struct {
	Name   string             `json:"name"`
	Query  QueryFixture       `json:"query"`
	Expect ReportExpectations `json:"expect"`
}

type QueryFixture struct {
	Clock                  string   `json:"clock,omitempty"`
	Smoothing              string   `json:"smoothing,omitempty"`
	KernelRadius           string   `json:"kernelRadius,omitempty"`
	KernelSigma            string   `json:"kernelSigma,omitempty"`
	HoldingDampingMillis   string   `json:"holdingDampingMillis,omitempty"`
	TransitionDampingCount string   `json:"transitionDampingCount,omitempty"`
	Include                []string `json:"include,omitempty"`
}

type ReportExpectations struct {
	OccupancyBucketSumsToOne  bool                `json:"occupancyBucketSumsToOne,omitempty"`
	PreferenceBucketSumsToOne bool                `json:"preferenceBucketSumsToOne,omitempty"`
	FallbackBuckets           *int                `json:"fallbackBuckets,omitempty"`
	ExactSeries               []SeriesExpectation `json:"exactSeries,omitempty"`
	PositiveSeries            []SeriesLocator     `json:"positiveSeries,omitempty"`
	ZeroSeries                []SeriesLocator     `json:"zeroSeries,omitempty"`
}

type SeriesExpectation struct {
	Section   string  `json:"section"`
	State     int     `json:"state,omitempty"`
	FromState int     `json:"fromState,omitempty"`
	ToState   int     `json:"toState,omitempty"`
	Bucket    int     `json:"bucket"`
	Want      float64 `json:"want"`
	Tolerance float64 `json:"tolerance,omitempty"`
}

type SeriesLocator struct {
	Section   string `json:"section"`
	State     int    `json:"state,omitempty"`
	FromState int    `json:"fromState,omitempty"`
	ToState   int    `json:"toState,omitempty"`
	Bucket    int    `json:"bucket"`
}

func LoadFixture(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, err
	}
	var fixture Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

func LoadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		fixture, err := LoadFixture(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, fixture)
	}
	slices.SortFunc(fixtures, func(a, b Fixture) int {
		return strings.Compare(a.Name, b.Name)
	})
	return fixtures, nil
}

func MustLoadFixtures(t *testing.T, dir string) []Fixture {
	t.Helper()
	fixtures, err := LoadFixtures(dir)
	if err != nil {
		t.Fatalf("load analytics fixtures: %v", err)
	}
	return fixtures
}

func SeedFixture(t *testing.T, db *sql.DB, fixture Fixture) {
	t.Helper()
	ctx := context.Background()
	control := storage.Control{
		ControlID:   fixture.Control.ControlID,
		ControlType: storage.ControlType(fixture.Control.ControlType),
		NumStates:   fixture.Control.NumStates,
		StateLabels: append([]string(nil), fixture.Control.StateLabels...),
	}
	if err := storage.UpsertControl(ctx, db, control); err != nil {
		t.Fatalf("seed control for fixture %s: %v", fixture.Name, err)
	}

	registeredModels := append([]string(nil), fixture.RegisteredModels...)
	if fixture.Aggregate.ModelID != "" && !slices.Contains(registeredModels, fixture.Aggregate.ModelID) {
		registeredModels = append(registeredModels, fixture.Aggregate.ModelID)
	}
	for _, modelID := range registeredModels {
		if err := storage.SaveModel(ctx, db, control.ControlID, "", storage.Model{ModelID: modelID}); err != nil && err != storage.ErrConflict {
			t.Fatalf("seed model %s for fixture %s: %v", modelID, fixture.Name, err)
		}
	}

	key := storage.AggregateKey{ControlID: control.ControlID, ModelID: fixture.Aggregate.ModelID, QuarterIndex: fixture.Aggregate.QuarterIndex}
	if _, err := storage.GetOrCreateAggregate(ctx, db, key, control.NumStates); err != nil {
		t.Fatalf("seed aggregate for fixture %s: %v", fixture.Name, err)
	}
	if err := storage.UpdateAggregate(ctx, db, key, control.NumStates, func(blob []byte) error {
		packed, err := domain.NewBlob(control.NumStates)
		if err != nil {
			return err
		}
		copy(packed.Data(), blob)
		for _, holding := range fixture.Aggregate.Holdings {
			clock, err := fixtureClock(holding.Clock)
			if err != nil {
				return err
			}
			idx, err := domain.HoldIndex(holding.State, clock, holding.Bucket, control.NumStates)
			if err != nil {
				return err
			}
			if err := packed.SetU64(idx, holding.Value); err != nil {
				return err
			}
		}
		for _, transition := range fixture.Aggregate.Transitions {
			clock, err := fixtureClock(transition.Clock)
			if err != nil {
				return err
			}
			idx, err := domain.TransIndex(transition.FromState, transition.ToState, clock, transition.Bucket, control.NumStates)
			if err != nil {
				return err
			}
			if err := packed.SetU64(idx, transition.Value); err != nil {
				return err
			}
		}
		copy(blob, packed.Data())
		return nil
	}); err != nil {
		t.Fatalf("update aggregate for fixture %s: %v", fixture.Name, err)
	}
}

func DecodeJSONResponse[T any](t *testing.T, body []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode json response: %v body=%q", err, string(body))
	}
	return value
}

func RequireNormalizedSeries(t *testing.T, series [][]float64, tolerance float64) {
	t.Helper()
	if len(series) == 0 {
		t.Fatalf("expected non-empty series")
	}
	for bucket := range series[0] {
		sum := 0.0
		for _, stateSeries := range series {
			sum += stateSeries[bucket]
		}
		if diff := sum - 1; diff < -tolerance || diff > tolerance {
			t.Fatalf("expected normalized bucket %d, got %f", bucket, sum)
		}
	}
}

func RequireApproxEqual(t *testing.T, got, want, tolerance float64, label string) {
	t.Helper()
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		t.Fatalf("%s: got %f want %f tolerance %f", label, got, want, tolerance)
	}
}

func fixtureClock(slug string) (int, error) {
	switch slug {
	case "", "utc":
		return domain.ClockUTC, nil
	case "local":
		return domain.ClockLocal, nil
	case "mean-solar":
		return domain.ClockMeanSolar, nil
	case "apparent-solar":
		return domain.ClockApparentSolar, nil
	case "unequal-hours":
		return domain.ClockUnequalHours, nil
	default:
		return 0, fmt.Errorf("unknown fixture clock %q", slug)
	}
}
