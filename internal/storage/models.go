package storage

type ControlType string

const (
	ControlTypeDiscrete ControlType = "discrete"
	ControlTypeSlider   ControlType = "slider"
)

type Control struct {
	ControlID   string
	ControlType ControlType
	NumStates   int
	StateLabels []string
}

type AggregateKey struct {
	ControlID    string
	ModelID      string
	QuarterIndex int
}
