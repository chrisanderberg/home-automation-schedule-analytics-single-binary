package control

import "fmt"

type Type string

const (
	TypeDiscrete Type = "discrete"
	TypeSlider   Type = "slider"
)

type Control struct {
	ID        string
	Type      Type
	NumStates int
}

func (c Control) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("control ID is required")
	}
	switch c.Type {
	case TypeDiscrete:
		if c.NumStates < 2 || c.NumStates > 10 {
			return fmt.Errorf("discrete controls must have 2-10 states")
		}
	case TypeSlider:
		if c.NumStates != 6 {
			return fmt.Errorf("slider controls must have 6 states")
		}
	default:
		return fmt.Errorf("unknown control type %q", c.Type)
	}
	return nil
}
