package ingest

type Config struct {
	TimeZone  string  `json:"timeZone"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type HoldingInput struct {
	ControlID   string `json:"controlId"`
	ModelID     string `json:"modelId"`
	State       int    `json:"state"`
	StartTimeMs int64  `json:"startTimeMs"`
	EndTimeMs   int64  `json:"endTimeMs"`
}

type TransitionInput struct {
	ControlID   string `json:"controlId"`
	ModelID     string `json:"modelId"`
	FromState   int    `json:"fromState"`
	ToState     int    `json:"toState"`
	TimestampMs int64  `json:"timestampMs"`
}
