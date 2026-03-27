package storage

// ControlType identifies which UI contract a control follows.
type ControlType string

const (
	ControlTypeRadioButtons ControlType = "radio buttons"
	ControlTypeSliders      ControlType = "sliders"
)

// Control stores the persisted metadata for one configurable control.
type Control struct {
	ControlID   string
	ControlType ControlType
	NumStates   int
	StateLabels []string
}

// Model stores optional metadata for one model identifier on a control.
type Model struct {
	ControlID string
	ModelID   string
}

// AggregateKey identifies one aggregate blob by control, model, and quarter.
type AggregateKey struct {
	ControlID    string
	ModelID      string
	QuarterIndex int
}
