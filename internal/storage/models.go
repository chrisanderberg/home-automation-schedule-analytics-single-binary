package storage

type ControlType string

const (
	ControlTypeRadioButtons ControlType = "radio buttons"
	ControlTypeSliders      ControlType = "sliders"
)

type Control struct {
	ControlID   string
	ControlType ControlType
	NumStates   int
	StateLabels []string
}

type Model struct {
	ControlID string
	ModelID   string
}

type AggregateKey struct {
	ControlID    string
	ModelID      string
	QuarterIndex int
}
