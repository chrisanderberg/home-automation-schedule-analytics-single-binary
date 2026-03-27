package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

const (
	defaultSmoothingRadiusBuckets = 6
	defaultSmoothingSigmaBuckets  = 3.0
	defaultHoldingDampingMillis   = 5 * 60 * 1000
	defaultTransitionDampingCount = 0.05
	powerTolerance                = 1e-10
	powerIterations               = 512
)

const (
	SmoothingGaussian = "gaussian"
	SmoothingNone     = "none"
)

var ErrUnknownClock = errors.New("unknown clock")

// RawStateBuckets carries one state's stored per-bucket holding counters.
type RawStateBuckets struct {
	State   int      `json:"state"`
	Label   string   `json:"label"`
	Buckets []uint64 `json:"buckets"`
}

// RawTransitionBuckets carries one stored per-bucket transition counter series.
type RawTransitionBuckets struct {
	FromState int      `json:"fromState"`
	FromLabel string   `json:"fromLabel"`
	ToState   int      `json:"toState"`
	ToLabel   string   `json:"toLabel"`
	Buckets   []uint64 `json:"buckets"`
}

// RawClockReport exposes one clock's stored holding and transition counters.
type RawClockReport struct {
	Clock            int                    `json:"clock"`
	ClockSlug        string                 `json:"clockSlug"`
	ClockLabel       string                 `json:"clockLabel"`
	HoldingMillis    []RawStateBuckets      `json:"holdingMillis"`
	TransitionCounts []RawTransitionBuckets `json:"transitionCounts"`
}

// RawReport exposes one aggregate exactly as stored, without smoothing or damping.
type RawReport struct {
	ControlID    string           `json:"controlId"`
	ModelID      string           `json:"modelId"`
	QuarterIndex int              `json:"quarterIndex"`
	QuarterLabel string           `json:"quarterLabel"`
	NumStates    int              `json:"numStates"`
	StateLabels  []string         `json:"stateLabels"`
	Clocks       []RawClockReport `json:"clocks"`
}

// Series carries one state's normalized weekly series for a report view.
type Series struct {
	State   int       `json:"state"`
	Label   string    `json:"label"`
	Mean    float64   `json:"mean"`
	Buckets []float64 `json:"buckets"`
}

// TransitionSeries carries one transition-derived weekly float64 series.
type TransitionSeries struct {
	FromState int       `json:"fromState"`
	FromLabel string    `json:"fromLabel"`
	ToState   int       `json:"toState"`
	ToLabel   string    `json:"toLabel"`
	Mean      float64   `json:"mean"`
	Buckets   []float64 `json:"buckets"`
}

// Diagnostics captures the raw totals and fallback counts behind one clock report.
type Diagnostics struct {
	TotalHoldingMillis float64 `json:"totalHoldingMillis"`
	TransitionCount    float64 `json:"transitionCount"`
	FallbackBuckets    int     `json:"fallbackBuckets"`
}

// IntermediateData exposes optional raw, smoothed, and rate series for harness validation.
type IntermediateData struct {
	RawHoldingMillis        []RawStateBuckets      `json:"rawHoldingMillis,omitempty"`
	RawTransitionCounts     []RawTransitionBuckets `json:"rawTransitionCounts,omitempty"`
	SmoothedHoldingMillis   []Series               `json:"smoothedHoldingMillis,omitempty"`
	SmoothedTransitionCount []TransitionSeries     `json:"smoothedTransitionCounts,omitempty"`
	TransitionRates         []TransitionSeries     `json:"transitionRates,omitempty"`
}

// SmoothingParameters records the smoothing configuration applied to a derived report.
type SmoothingParameters struct {
	Kind         string  `json:"kind"`
	KernelRadius int     `json:"kernelRadius"`
	KernelSigma  float64 `json:"kernelSigma"`
}

// DampingParameters records the damping configuration applied to a derived report.
type DampingParameters struct {
	HoldingMillis   float64 `json:"holdingMillis"`
	TransitionCount float64 `json:"transitionCount"`
}

// ReportParameters records the transformation knobs applied to a derived report.
type ReportParameters struct {
	Smoothing SmoothingParameters `json:"smoothing"`
	Damping   DampingParameters   `json:"damping"`
}

// IncludeOptions selects optional intermediate sections for derived reports.
type IncludeOptions struct {
	Raw         bool
	Smoothed    bool
	Rates       bool
	Diagnostics bool
}

// ReportOptions controls smoothing, damping, and optional intermediate output.
type ReportOptions struct {
	SmoothingKind          string
	KernelRadius           int
	KernelSigma            float64
	HoldingDampingMillis   float64
	TransitionDampingCount float64
	Include                IncludeOptions
}

// DerivedClockReport groups occupancy and inferred-preference series for one clock system.
type DerivedClockReport struct {
	Clock            int               `json:"clock"`
	ClockSlug        string            `json:"clockSlug"`
	ClockLabel       string            `json:"clockLabel"`
	OccupancySeries  []Series          `json:"occupancySeries"`
	PreferenceSeries []Series          `json:"preferenceSeries"`
	Diagnostics      *Diagnostics      `json:"diagnostics,omitempty"`
	Intermediates    *IntermediateData `json:"intermediates,omitempty"`
}

// DerivedReport describes one derived `(control, model, quarter)` analytics slice.
type DerivedReport struct {
	ControlID    string               `json:"controlId"`
	ModelID      string               `json:"modelId"`
	QuarterIndex int                  `json:"quarterIndex"`
	QuarterLabel string               `json:"quarterLabel"`
	NumStates    int                  `json:"numStates"`
	StateLabels  []string             `json:"stateLabels"`
	Parameters   ReportParameters     `json:"parameters"`
	Clocks       []DerivedClockReport `json:"clocks"`
}

// StateSeries carries one state's normalized weekly series for the legacy report view.
type StateSeries struct {
	State int       `json:"state"`
	Label string    `json:"label"`
	Mean  float64   `json:"mean"`
	Raw   []float64 `json:"raw"`
}

// ClockReport is the legacy JSON/UI shape kept for backward compatibility.
type ClockReport struct {
	Clock            int           `json:"clock"`
	ClockSlug        string        `json:"clockSlug"`
	ClockLabel       string        `json:"clockLabel"`
	OccupancySeries  []StateSeries `json:"occupancySeries"`
	PreferenceSeries []StateSeries `json:"preferenceSeries"`
	Diagnostics      Diagnostics   `json:"diagnostics"`
}

// Report is the legacy `(control, model, quarter)` analytics response shape.
type Report struct {
	ControlID    string        `json:"controlId"`
	ModelID      string        `json:"modelId"`
	QuarterIndex int           `json:"quarterIndex"`
	QuarterLabel string        `json:"quarterLabel"`
	Clocks       []ClockReport `json:"clocks"`
}

// DefaultReportOptions returns the smoothing and damping parameters used by the existing report path.
func DefaultReportOptions() ReportOptions {
	return ReportOptions{
		SmoothingKind:          SmoothingGaussian,
		KernelRadius:           defaultSmoothingRadiusBuckets,
		KernelSigma:            defaultSmoothingSigmaBuckets,
		HoldingDampingMillis:   defaultHoldingDampingMillis,
		TransitionDampingCount: defaultTransitionDampingCount,
		Include: IncludeOptions{
			Diagnostics: true,
		},
	}
}

// BuildRawReport loads one aggregate and exposes its stored counters without any derived processing.
func BuildRawReport(ctx context.Context, db *sql.DB, control storage.Control, modelID string, quarterIndex int) (RawReport, error) {
	key := storage.AggregateKey{
		ControlID:    control.ControlID,
		ModelID:      modelID,
		QuarterIndex: quarterIndex,
	}
	data, err := storage.GetAggregate(ctx, db, key, control.NumStates)
	if err != nil {
		return RawReport{}, err
	}

	decoded, err := decodeAggregate(data, control)
	if err != nil {
		return RawReport{}, err
	}

	report := RawReport{
		ControlID:    control.ControlID,
		ModelID:      modelID,
		QuarterIndex: quarterIndex,
		QuarterLabel: quarterLabel(quarterIndex),
		NumStates:    control.NumStates,
		StateLabels:  append([]string(nil), control.StateLabels...),
	}
	for clock := 0; clock < domain.Clocks; clock++ {
		report.Clocks = append(report.Clocks, buildRawClockReport(control, clock, decoded))
	}
	return report, nil
}

// BuildDerivedReport computes a parameterized analytical report from one stored aggregate.
func BuildDerivedReport(ctx context.Context, db *sql.DB, control storage.Control, modelID string, quarterIndex int, opts ReportOptions) (DerivedReport, error) {
	raw, err := BuildRawReport(ctx, db, control, modelID, quarterIndex)
	if err != nil {
		return DerivedReport{}, err
	}
	return BuildDerivedReportFromRaw(raw, opts)
}

// BuildReport computes the legacy occupancy and inferred-preference report shape.
func BuildReport(ctx context.Context, db *sql.DB, control storage.Control, modelID string, quarterIndex int) (Report, error) {
	derived, err := BuildDerivedReport(ctx, db, control, modelID, quarterIndex, DefaultReportOptions())
	if err != nil {
		return Report{}, err
	}
	return legacyReportFromDerived(derived), nil
}

// BuildDerivedReportFromRaw computes a derived report from raw stored counters.
func BuildDerivedReportFromRaw(raw RawReport, opts ReportOptions) (DerivedReport, error) {
	if err := validateReportOptions(opts); err != nil {
		return DerivedReport{}, err
	}

	report := DerivedReport{
		ControlID:    raw.ControlID,
		ModelID:      raw.ModelID,
		QuarterIndex: raw.QuarterIndex,
		QuarterLabel: raw.QuarterLabel,
		NumStates:    raw.NumStates,
		StateLabels:  append([]string(nil), raw.StateLabels...),
		Parameters: ReportParameters{
			Smoothing: SmoothingParameters{
				Kind:         opts.SmoothingKind,
				KernelRadius: opts.KernelRadius,
				KernelSigma:  opts.KernelSigma,
			},
			Damping: DampingParameters{
				HoldingMillis:   opts.HoldingDampingMillis,
				TransitionCount: opts.TransitionDampingCount,
			},
		},
	}

	for _, rawClock := range raw.Clocks {
		report.Clocks = append(report.Clocks, buildDerivedClockReport(rawClock, opts))
	}
	return report, nil
}

// ClockBySlug returns the matching clock section from a raw report.
func (r RawReport) ClockBySlug(slug string) (RawClockReport, error) {
	if slug == "" {
		if len(r.Clocks) == 0 {
			return RawClockReport{}, ErrUnknownClock
		}
		return r.Clocks[0], nil
	}
	for _, clock := range r.Clocks {
		if clock.ClockSlug == slug {
			return clock, nil
		}
	}
	return RawClockReport{}, ErrUnknownClock
}

// ClockBySlug returns the matching clock section from a derived report.
func (r DerivedReport) ClockBySlug(slug string) (DerivedClockReport, error) {
	if slug == "" {
		if len(r.Clocks) == 0 {
			return DerivedClockReport{}, ErrUnknownClock
		}
		return r.Clocks[0], nil
	}
	for _, clock := range r.Clocks {
		if clock.ClockSlug == slug {
			return clock, nil
		}
	}
	return DerivedClockReport{}, ErrUnknownClock
}

// ClockBySlug returns the matching report clock section.
func (r Report) ClockBySlug(slug string) (ClockReport, error) {
	if slug == "" {
		if len(r.Clocks) == 0 {
			return ClockReport{}, ErrUnknownClock
		}
		return r.Clocks[0], nil
	}
	for _, clock := range r.Clocks {
		if clock.ClockSlug == slug {
			return clock, nil
		}
	}
	return ClockReport{}, ErrUnknownClock
}

type aggregateSeries struct {
	holds [][][]uint64
	trans [][][][]uint64
}

func validateReportOptions(opts ReportOptions) error {
	switch opts.SmoothingKind {
	case SmoothingGaussian:
		if opts.KernelRadius < 0 {
			return fmt.Errorf("kernel radius must be non-negative")
		}
		if opts.KernelSigma <= 0 {
			return fmt.Errorf("kernel sigma must be positive")
		}
	case SmoothingNone:
		if opts.KernelRadius != 0 || opts.KernelSigma != 0 {
			return fmt.Errorf("kernel parameters are only valid with gaussian smoothing")
		}
	default:
		return fmt.Errorf("unknown smoothing kind")
	}
	if opts.HoldingDampingMillis < 0 {
		return fmt.Errorf("holding damping must be non-negative")
	}
	if opts.TransitionDampingCount < 0 {
		return fmt.Errorf("transition damping must be non-negative")
	}
	return nil
}

func buildRawClockReport(control storage.Control, clock int, decoded aggregateSeries) RawClockReport {
	report := RawClockReport{
		Clock:      clock,
		ClockSlug:  clockSlug(clock),
		ClockLabel: clockLabel(clock),
	}
	for state := 0; state < control.NumStates; state++ {
		report.HoldingMillis = append(report.HoldingMillis, RawStateBuckets{
			State:   state,
			Label:   stateLabel(control.StateLabels, state),
			Buckets: append([]uint64(nil), decoded.holds[clock][state]...),
		})
	}
	for fromState := 0; fromState < control.NumStates; fromState++ {
		for toState := 0; toState < control.NumStates; toState++ {
			if fromState == toState {
				continue
			}
			report.TransitionCounts = append(report.TransitionCounts, RawTransitionBuckets{
				FromState: fromState,
				FromLabel: stateLabel(control.StateLabels, fromState),
				ToState:   toState,
				ToLabel:   stateLabel(control.StateLabels, toState),
				Buckets:   append([]uint64(nil), decoded.trans[clock][fromState][toState]...),
			})
		}
	}
	return report
}

func buildDerivedClockReport(rawClock RawClockReport, opts ReportOptions) DerivedClockReport {
	numStates := len(rawClock.HoldingMillis)
	occupancyByState := make([][]float64, numStates)
	preferenceByState := make([][]float64, numStates)
	smoothedHolds := make([][]float64, numStates)
	smoothedTransitions := make([][][]float64, numStates)
	ratesByTransition := make([][][]float64, numStates)
	for state := 0; state < numStates; state++ {
		occupancyByState[state] = make([]float64, domain.BucketsPerWeek)
		preferenceByState[state] = make([]float64, domain.BucketsPerWeek)
		smoothedHolds[state] = smoothSeries(uint64sToFloat64(rawClock.HoldingMillis[state].Buckets), opts)
		smoothedTransitions[state] = make([][]float64, numStates)
		ratesByTransition[state] = make([][]float64, numStates)
	}

	for _, transition := range rawClock.TransitionCounts {
		smoothedTransitions[transition.FromState][transition.ToState] = smoothSeries(uint64sToFloat64(transition.Buckets), opts)
		ratesByTransition[transition.FromState][transition.ToState] = make([]float64, domain.BucketsPerWeek)
	}
	rawTransitionBuckets := rawTransitionBucketsByPair(rawClock)

	diagnostics := Diagnostics{}
	for bucket := 0; bucket < domain.BucketsPerWeek; bucket++ {
		rawOccupancy := make([]float64, numStates)
		smoothedHoldBucket := make([]float64, numStates)
		smoothedTransitionBucket := make([][]float64, numStates)
		for fromState := 0; fromState < numStates; fromState++ {
			smoothedTransitionBucket[fromState] = make([]float64, numStates)
			rawOccupancy[fromState] = float64(rawClock.HoldingMillis[fromState].Buckets[bucket])
			smoothedHoldBucket[fromState] = smoothedHolds[fromState][bucket]
			diagnostics.TotalHoldingMillis += rawOccupancy[fromState]
			for toState := 0; toState < numStates; toState++ {
				if fromState == toState || smoothedTransitions[fromState][toState] == nil {
					continue
				}
				smoothedTransitionBucket[fromState][toState] = smoothedTransitions[fromState][toState][bucket]
				diagnostics.TransitionCount += float64(rawTransitionBuckets[fromState][toState][bucket])
			}
		}

		occupancyDistribution := normalizeDistribution(rawOccupancy)
		preferenceDistribution, rates, fallback := inferPreference(smoothedHoldBucket, smoothedTransitionBucket, opts)
		if fallback {
			diagnostics.FallbackBuckets++
		}
		for state := 0; state < numStates; state++ {
			occupancyByState[state][bucket] = occupancyDistribution[state]
			preferenceByState[state][bucket] = preferenceDistribution[state]
			for toState := 0; toState < numStates; toState++ {
				if state == toState || rates[state] == nil || ratesByTransition[state][toState] == nil {
					continue
				}
				ratesByTransition[state][toState][bucket] = rates[state][toState]
			}
		}
	}

	clockReport := DerivedClockReport{
		Clock:            rawClock.Clock,
		ClockSlug:        rawClock.ClockSlug,
		ClockLabel:       rawClock.ClockLabel,
		OccupancySeries:  make([]Series, 0, numStates),
		PreferenceSeries: make([]Series, 0, numStates),
	}
	if opts.Include.Diagnostics {
		copyDiag := diagnostics
		clockReport.Diagnostics = &copyDiag
	}
	for state := 0; state < numStates; state++ {
		label := rawClock.HoldingMillis[state].Label
		clockReport.OccupancySeries = append(clockReport.OccupancySeries, Series{
			State:   state,
			Label:   label,
			Mean:    meanSeries(occupancyByState[state]),
			Buckets: occupancyByState[state],
		})
		clockReport.PreferenceSeries = append(clockReport.PreferenceSeries, Series{
			State:   state,
			Label:   label,
			Mean:    meanSeries(preferenceByState[state]),
			Buckets: preferenceByState[state],
		})
	}

	intermediates := buildIntermediateData(rawClock, opts, smoothedHolds, smoothedTransitions, ratesByTransition)
	if intermediates != nil {
		clockReport.Intermediates = intermediates
	}

	return clockReport
}

func buildIntermediateData(rawClock RawClockReport, opts ReportOptions, smoothedHolds [][]float64, smoothedTransitions [][][]float64, ratesByTransition [][][]float64) *IntermediateData {
	if !opts.Include.Raw && !opts.Include.Smoothed && !opts.Include.Rates {
		return nil
	}
	data := &IntermediateData{}
	if opts.Include.Raw {
		data.RawHoldingMillis = cloneRawStateSeries(rawClock.HoldingMillis)
		data.RawTransitionCounts = cloneRawTransitionSeries(rawClock.TransitionCounts)
	}
	if opts.Include.Smoothed {
		for state, holding := range rawClock.HoldingMillis {
			data.SmoothedHoldingMillis = append(data.SmoothedHoldingMillis, Series{
				State:   holding.State,
				Label:   holding.Label,
				Mean:    meanSeries(smoothedHolds[state]),
				Buckets: append([]float64(nil), smoothedHolds[state]...),
			})
		}
		for _, transition := range rawClock.TransitionCounts {
			data.SmoothedTransitionCount = append(data.SmoothedTransitionCount, TransitionSeries{
				FromState: transition.FromState,
				FromLabel: transition.FromLabel,
				ToState:   transition.ToState,
				ToLabel:   transition.ToLabel,
				Mean:      meanSeries(smoothedTransitions[transition.FromState][transition.ToState]),
				Buckets:   append([]float64(nil), smoothedTransitions[transition.FromState][transition.ToState]...),
			})
		}
	}
	if opts.Include.Rates {
		for _, transition := range rawClock.TransitionCounts {
			data.TransitionRates = append(data.TransitionRates, TransitionSeries{
				FromState: transition.FromState,
				FromLabel: transition.FromLabel,
				ToState:   transition.ToState,
				ToLabel:   transition.ToLabel,
				Mean:      meanSeries(ratesByTransition[transition.FromState][transition.ToState]),
				Buckets:   append([]float64(nil), ratesByTransition[transition.FromState][transition.ToState]...),
			})
		}
	}
	return data
}

func legacyReportFromDerived(derived DerivedReport) Report {
	report := Report{
		ControlID:    derived.ControlID,
		ModelID:      derived.ModelID,
		QuarterIndex: derived.QuarterIndex,
		QuarterLabel: derived.QuarterLabel,
	}
	for _, clock := range derived.Clocks {
		legacyClock := ClockReport{
			Clock:      clock.Clock,
			ClockSlug:  clock.ClockSlug,
			ClockLabel: clock.ClockLabel,
		}
		if clock.Diagnostics != nil {
			legacyClock.Diagnostics = *clock.Diagnostics
		}
		for _, series := range clock.OccupancySeries {
			legacyClock.OccupancySeries = append(legacyClock.OccupancySeries, StateSeries{
				State: series.State,
				Label: series.Label,
				Mean:  series.Mean,
				Raw:   append([]float64(nil), series.Buckets...),
			})
		}
		for _, series := range clock.PreferenceSeries {
			legacyClock.PreferenceSeries = append(legacyClock.PreferenceSeries, StateSeries{
				State: series.State,
				Label: series.Label,
				Mean:  series.Mean,
				Raw:   append([]float64(nil), series.Buckets...),
			})
		}
		report.Clocks = append(report.Clocks, legacyClock)
	}
	return report
}

// inferPreference estimates a stationary distribution and reports whether it fell back.
func inferPreference(smoothedHolding []float64, smoothedTransitions [][]float64, opts ReportOptions) ([]float64, [][]float64, bool) {
	fallbackDistribution := normalizeDistribution(smoothedHolding)
	if distributionIsDegenerate(fallbackDistribution) {
		fallbackDistribution = uniformDistribution(len(smoothedHolding))
	}

	totalHolding := 0.0
	totalTransitions := 0.0
	for fromState := range smoothedHolding {
		totalHolding += smoothedHolding[fromState]
		for toState := range smoothedTransitions[fromState] {
			totalTransitions += smoothedTransitions[fromState][toState]
		}
	}
	if totalHolding == 0 && totalTransitions == 0 {
		return fallbackDistribution, zeroRateMatrix(len(smoothedHolding)), true
	}

	rates := make([][]float64, len(smoothedHolding))
	lambda := 0.0
	for fromState := range smoothedHolding {
		rates[fromState] = make([]float64, len(smoothedHolding))
		outRate := 0.0
		denominator := smoothedHolding[fromState] + opts.HoldingDampingMillis
		if denominator <= 0 {
			return fallbackDistribution, zeroRateMatrix(len(smoothedHolding)), true
		}
		for toState := range smoothedHolding {
			if fromState == toState {
				continue
			}
			rate := (smoothedTransitions[fromState][toState] + opts.TransitionDampingCount) / denominator
			if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
				return fallbackDistribution, zeroRateMatrix(len(smoothedHolding)), true
			}
			rates[fromState][toState] = rate
			outRate += rate
		}
		if outRate > lambda {
			lambda = outRate
		}
	}
	if lambda <= 0 {
		return fallbackDistribution, rates, true
	}

	current := append([]float64(nil), fallbackDistribution...)
	next := make([]float64, len(current))
	for iteration := 0; iteration < powerIterations; iteration++ {
		for i := range next {
			next[i] = 0
		}
		for fromState, weight := range current {
			rowSum := 0.0
			for toState := range current {
				if fromState == toState {
					continue
				}
				p := rates[fromState][toState] / lambda
				rowSum += p
				next[toState] += weight * p
			}
			next[fromState] += weight * (1 - rowSum)
		}
		next = normalizeDistribution(next)
		if distributionIsDegenerate(next) {
			return fallbackDistribution, rates, true
		}
		maxDelta := 0.0
		for i := range current {
			delta := math.Abs(next[i] - current[i])
			if delta > maxDelta {
				maxDelta = delta
			}
			current[i] = next[i]
		}
		if maxDelta < powerTolerance {
			return current, rates, false
		}
	}
	return fallbackDistribution, rates, true
}

// decodeAggregate expands a persisted aggregate blob into per-clock, per-state stored counters.
func decodeAggregate(data []byte, control storage.Control) (aggregateSeries, error) {
	blob, err := domain.NewBlob(control.NumStates)
	if err != nil {
		return aggregateSeries{}, err
	}
	copy(blob.Data(), data)

	result := aggregateSeries{
		holds: make([][][]uint64, domain.Clocks),
		trans: make([][][][]uint64, domain.Clocks),
	}
	for clock := 0; clock < domain.Clocks; clock++ {
		result.holds[clock] = make([][]uint64, control.NumStates)
		result.trans[clock] = make([][][]uint64, control.NumStates)
		for state := 0; state < control.NumStates; state++ {
			result.holds[clock][state] = make([]uint64, domain.BucketsPerWeek)
			result.trans[clock][state] = make([][]uint64, control.NumStates)
			for bucket := 0; bucket < domain.BucketsPerWeek; bucket++ {
				idx, err := domain.HoldIndex(state, clock, bucket, control.NumStates)
				if err != nil {
					return aggregateSeries{}, err
				}
				value, err := blob.GetU64(idx)
				if err != nil {
					return aggregateSeries{}, err
				}
				result.holds[clock][state][bucket] = value
			}
			for toState := 0; toState < control.NumStates; toState++ {
				result.trans[clock][state][toState] = make([]uint64, domain.BucketsPerWeek)
				if state == toState {
					continue
				}
				for bucket := 0; bucket < domain.BucketsPerWeek; bucket++ {
					idx, err := domain.TransIndex(state, toState, clock, bucket, control.NumStates)
					if err != nil {
						return aggregateSeries{}, err
					}
					value, err := blob.GetU64(idx)
					if err != nil {
						return aggregateSeries{}, err
					}
					result.trans[clock][state][toState][bucket] = value
				}
			}
		}
	}
	return result, nil
}

func smoothSeries(series []float64, opts ReportOptions) []float64 {
	if opts.SmoothingKind == SmoothingNone {
		return append([]float64(nil), series...)
	}
	return smoothCyclic(series, opts.KernelRadius, opts.KernelSigma)
}

// smoothCyclic applies Gaussian-style cyclic smoothing across the weekly series.
func smoothCyclic(series []float64, radius int, sigma float64) []float64 {
	result := make([]float64, len(series))
	if len(series) == 0 {
		return result
	}
	if radius == 0 {
		return append([]float64(nil), series...)
	}

	weights := make([]float64, 2*radius+1)
	totalWeight := 0.0
	for offset := -radius; offset <= radius; offset++ {
		weight := math.Exp(-0.5 * math.Pow(float64(offset)/sigma, 2))
		weights[offset+radius] = weight
		totalWeight += weight
	}

	for idx := range series {
		smoothed := 0.0
		for offset := -radius; offset <= radius; offset++ {
			source := (idx + offset + len(series)) % len(series)
			smoothed += series[source] * weights[offset+radius]
		}
		result[idx] = smoothed / totalWeight
	}
	return result
}

func uint64sToFloat64(values []uint64) []float64 {
	result := make([]float64, len(values))
	for i, value := range values {
		result[i] = float64(value)
	}
	return result
}

func rawTransitionBucketsByPair(clock RawClockReport) [][][]uint64 {
	numStates := len(clock.HoldingMillis)
	pairs := make([][][]uint64, numStates)
	for fromState := 0; fromState < numStates; fromState++ {
		pairs[fromState] = make([][]uint64, numStates)
		for toState := 0; toState < numStates; toState++ {
			pairs[fromState][toState] = make([]uint64, domain.BucketsPerWeek)
		}
	}
	for _, transition := range clock.TransitionCounts {
		pairs[transition.FromState][transition.ToState] = transition.Buckets
	}
	return pairs
}

func cloneRawStateSeries(values []RawStateBuckets) []RawStateBuckets {
	cloned := make([]RawStateBuckets, 0, len(values))
	for _, value := range values {
		cloned = append(cloned, RawStateBuckets{
			State:   value.State,
			Label:   value.Label,
			Buckets: append([]uint64(nil), value.Buckets...),
		})
	}
	return cloned
}

func cloneRawTransitionSeries(values []RawTransitionBuckets) []RawTransitionBuckets {
	cloned := make([]RawTransitionBuckets, 0, len(values))
	for _, value := range values {
		cloned = append(cloned, RawTransitionBuckets{
			FromState: value.FromState,
			FromLabel: value.FromLabel,
			ToState:   value.ToState,
			ToLabel:   value.ToLabel,
			Buckets:   append([]uint64(nil), value.Buckets...),
		})
	}
	return cloned
}

func zeroRateMatrix(n int) [][]float64 {
	result := make([][]float64, n)
	for i := range result {
		result[i] = make([]float64, n)
	}
	return result
}

func stateLabel(labels []string, state int) string {
	if state < len(labels) && labels[state] != "" {
		return labels[state]
	}
	return fmt.Sprintf("state %d", state+1)
}

// normalizeDistribution rescales non-negative values to sum to one when possible.
func normalizeDistribution(values []float64) []float64 {
	result := append([]float64(nil), values...)
	sum := 0.0
	for _, value := range result {
		sum += value
	}
	if sum <= 0 {
		return uniformDistribution(len(values))
	}
	for i := range result {
		result[i] /= sum
	}
	return result
}

// uniformDistribution returns an equal share for each state.
func uniformDistribution(n int) []float64 {
	if n == 0 {
		return nil
	}
	value := 1.0 / float64(n)
	result := make([]float64, n)
	for i := range result {
		result[i] = value
	}
	return result
}

// distributionIsDegenerate reports whether a distribution contains any usable mass.
func distributionIsDegenerate(values []float64) bool {
	if len(values) == 0 {
		return true
	}
	sum := 0.0
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return true
		}
		sum += value
	}
	return math.Abs(sum-1.0) > 1e-6
}

// meanSeries returns the arithmetic mean of a series.
func meanSeries(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

// clockSlug maps a clock index to the API and URL slug used by the UI.
func clockSlug(clock int) string {
	switch clock {
	case domain.ClockUTC:
		return "utc"
	case domain.ClockLocal:
		return "local"
	case domain.ClockMeanSolar:
		return "mean-solar"
	case domain.ClockApparentSolar:
		return "apparent-solar"
	case domain.ClockUnequalHours:
		return "unequal-hours"
	default:
		return "unknown"
	}
}

// clockLabel maps a clock index to the human-readable label shown in the UI.
func clockLabel(clock int) string {
	switch clock {
	case domain.ClockUTC:
		return "UTC"
	case domain.ClockLocal:
		return "Local time"
	case domain.ClockMeanSolar:
		return "Mean solar time"
	case domain.ClockApparentSolar:
		return "Apparent solar time"
	case domain.ClockUnequalHours:
		return "Unequal hours"
	default:
		return "Unknown"
	}
}

// quarterLabel formats a UTC quarter index for display.
func quarterLabel(qi int) string {
	year := 1970 + qi/4
	q := qi%4 + 1
	return fmt.Sprintf("%d Q%d", year, q)
}
