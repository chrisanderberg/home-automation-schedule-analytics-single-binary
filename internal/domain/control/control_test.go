package control_test

import (
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain/control"
)

func TestControlValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		control control.Control
		wantErr bool
	}{
		{
			name: "valid discrete",
			control: control.Control{
				ID:        "lamp",
				Type:      control.TypeDiscrete,
				NumStates: 3,
			},
		},
		{
			name: "valid slider",
			control: control.Control{
				ID:        "dimmer",
				Type:      control.TypeSlider,
				NumStates: 6,
			},
		},
		{
			name: "missing id",
			control: control.Control{
				Type:      control.TypeDiscrete,
				NumStates: 2,
			},
			wantErr: true,
		},
		{
			name: "invalid discrete range",
			control: control.Control{
				ID:        "lamp",
				Type:      control.TypeDiscrete,
				NumStates: 11,
			},
			wantErr: true,
		},
		{
			name: "invalid slider states",
			control: control.Control{
				ID:        "dimmer",
				Type:      control.TypeSlider,
				NumStates: 5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.control.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
