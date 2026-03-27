package ingest

// Config carries the ingest-time geography and timezone defaults.
type Config struct {
	TimeZone  string  `json:"timeZone"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// HoldingInput is the JSON payload for one holding-interval ingest event.
type HoldingInput struct {
	ControlID   string `json:"controlId"`
	ModelID     string `json:"modelId"`
	State       int    `json:"state"`
	StartTimeMs int64  `json:"startTimeMs"`
	EndTimeMs   int64  `json:"endTimeMs"`
}

// TransitionInput is the JSON payload for one user transition ingest event.
type TransitionInput struct {
	ControlID   string `json:"controlId"`
	ModelID     string `json:"modelId"`
	FromState   int    `json:"fromState"`
	ToState     int    `json:"toState"`
	TimestampMs int64  `json:"timestampMs"`
}
