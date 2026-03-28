package view

// AnalyticsOptionsFormData carries the URL-backed report-parameter controls.
type AnalyticsOptionsFormData struct {
	Action                 string
	ModelID                string
	Quarter                string
	Clock                  string
	Mode                   string
	Smoothing              string
	KernelRadius           string
	KernelSigma            string
	HoldingDampingMillis   string
	TransitionDampingCount string
	IncludeRaw             bool
	IncludeSmoothed        bool
	IncludeRates           bool
	Error                  string
}

// AnalyticsViewData packages the selected analytics slice for template rendering.
type AnalyticsViewData struct {
	HasData          bool
	ModelID          string
	QuarterLabel     string
	ClockLabel       string
	States           []AnalyticsStateData
	Diagnostics      AnalyticsDiagnostics
	OccupancyChart   ChartData
	PreferenceChart  ChartData
	ChartLegendItems []ChartLegendItem
}

// AnalyticsStateData carries one state's occupancy and preference series.
type AnalyticsStateData struct {
	OccupancyMean  string
	PreferenceMean string
	Label          string
}

// AnalyticsDiagnostics carries the diagnostics shown beside the analytics charts.
type AnalyticsDiagnostics struct {
	TotalHolding    string
	TransitionCount string
	FallbackBuckets int
}

// RawAnalyticsViewData packages the selected raw analytics slice for template rendering.
type RawAnalyticsViewData struct {
	HasData              bool
	ModelID              string
	QuarterLabel         string
	ClockLabel           string
	TotalHolding         string
	TransitionCount      string
	TotalHoldingChart    ChartData
	TotalTransitionChart ChartData
	HoldingStates        []RawAnalyticsStateData
	TransitionSeries     []RawAnalyticsTransitionData
}

// RawAnalyticsStateData carries one state's raw holding series.
type RawAnalyticsStateData struct {
	Label        string
	TotalHolding string
	HoldingChart ChartData
}

// RawAnalyticsTransitionData carries one transition's raw counter series.
type RawAnalyticsTransitionData struct {
	Label           string
	TransitionTotal string
	TransitionChart ChartData
}

// ChartData carries one serialized chart payload for the frontend renderer.
type ChartData struct {
	Kind    string
	Payload string
	Summary string
}

// ChartLegendItem describes one legend swatch for stacked state charts.
type ChartLegendItem struct {
	Label string
	Color string
}
