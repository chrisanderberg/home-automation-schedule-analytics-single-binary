package demodata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

const (
	DefaultModelID      = "weekday-v1"
	DefaultQuarterIndex = 224
)

// HoldingSeed describes one demo holding interval to ingest.
type HoldingSeed struct {
	ControlID string
	ModelID   string
	State     int
	Start     time.Time
	End       time.Time
}

// TransitionSeed describes one demo user transition to ingest.
type TransitionSeed struct {
	ControlID string
	ModelID   string
	FromState int
	ToState   int
	At        time.Time
}

// SeedDemoData populates deterministic demo controls, models, and aggregate-driving events.
func SeedDemoData(ctx context.Context, db *sql.DB, cfg ingest.Config) error {
	controls := []storage.Control{
		{
			ControlID:   "living-room-scene",
			ControlType: storage.ControlTypeRadioButtons,
			NumStates:   3,
			StateLabels: []string{"off", "ambient", "bright"},
		},
		{
			ControlID:   "bedroom-mode",
			ControlType: storage.ControlTypeRadioButtons,
			NumStates:   3,
			StateLabels: []string{"sleep", "reading", "bright"},
		},
		{
			ControlID:   "office-dimmer",
			ControlType: storage.ControlTypeSliders,
			NumStates:   6,
			StateLabels: []string{"min", "trans 1", "trans 2", "trans 3", "trans 4", "max"},
		},
	}
	for _, control := range controls {
		if err := storage.SaveControl(ctx, db, "", control); err != nil && !errors.Is(err, storage.ErrConflict) {
			return fmt.Errorf("save control %s: %w", control.ControlID, err)
		}
		if err := storage.SaveModel(ctx, db, control.ControlID, "", storage.Model{ModelID: DefaultModelID}); err != nil && !errors.Is(err, storage.ErrConflict) {
			return fmt.Errorf("save model %s/%s: %w", control.ControlID, DefaultModelID, err)
		}
	}

	holdings, transitions := BuildDemoEvents()
	for _, h := range holdings {
		if err := ingest.IngestHolding(ctx, db, cfg, ingest.HoldingInput{
			ControlID:   h.ControlID,
			ModelID:     h.ModelID,
			State:       h.State,
			StartTimeMs: h.Start.UnixMilli(),
			EndTimeMs:   h.End.UnixMilli(),
		}); err != nil {
			return fmt.Errorf("ingest holding %s %s-%s: %w", h.ControlID, h.Start.Format(time.RFC3339), h.End.Format(time.RFC3339), err)
		}
	}

	for _, tr := range transitions {
		if err := ingest.IngestTransition(ctx, db, cfg, ingest.TransitionInput{
			ControlID:   tr.ControlID,
			ModelID:     tr.ModelID,
			FromState:   tr.FromState,
			ToState:     tr.ToState,
			TimestampMs: tr.At.UnixMilli(),
		}); err != nil {
			return fmt.Errorf("ingest transition %s at %s: %w", tr.ControlID, tr.At.Format(time.RFC3339), err)
		}
	}

	return nil
}

// BuildDemoEvents returns the deterministic weekly schedule and corrective transitions used for demo/test data.
func BuildDemoEvents() ([]HoldingSeed, []TransitionSeed) {
	baseMonday := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	var holdings []HoldingSeed
	var transitions []TransitionSeed

	for week := 0; week < 14; week++ {
		weekStart := baseMonday.AddDate(0, 0, 7*week)
		for day := 0; day < 7; day++ {
			dayStart := weekStart.AddDate(0, 0, day)
			weekday := day < 5

			holdings = append(holdings,
				newHolding("living-room-scene", stateForLivingRoom(weekday, 0), dayStart, 0, 0, 6, 0),
				newHolding("living-room-scene", 1, dayStart, 6, 0, 18, 0),
				newHolding("living-room-scene", stateForLivingRoom(weekday, 2), dayStart, 18, 0, 22, 30),
				newHolding("living-room-scene", 0, dayStart, 22, 30, 24, 0),

				newHolding("bedroom-mode", 0, dayStart, 0, 0, 6, 30),
				newHolding("bedroom-mode", 2, dayStart, 6, 30, 7, 30),
				newHolding("bedroom-mode", stateForBedroom(weekday), dayStart, 20, 30, 22, 30),
				newHolding("bedroom-mode", 0, dayStart, 22, 30, 24, 0),

				newHolding("office-dimmer", 0, dayStart, 0, 0, 7, 0),
				newHolding("office-dimmer", officeWorkState(weekday), dayStart, 7, 0, 9, 0),
				newHolding("office-dimmer", officeWorkState(weekday)+1, dayStart, 9, 0, 17, 0),
				newHolding("office-dimmer", eveningOfficeState(weekday), dayStart, 17, 0, 21, 0),
				newHolding("office-dimmer", 0, dayStart, 21, 0, 24, 0),
			)

			transitions = append(transitions,
				newTransition("living-room-scene", 0, 1, dayStart, 6, 0),
				newTransition("living-room-scene", stateForLivingRoom(weekday, 2), 0, dayStart, 22, 30),

				newTransition("bedroom-mode", 0, 2, dayStart, 6, 30),
				newTransition("bedroom-mode", 2, 0, dayStart, 7, 30),
				newTransition("bedroom-mode", 0, stateForBedroom(weekday), dayStart, 20, 30),
				newTransition("bedroom-mode", stateForBedroom(weekday), 0, dayStart, 22, 30),

				newTransition("office-dimmer", 0, officeWorkState(weekday), dayStart, 7, 0),
				newTransition("office-dimmer", officeWorkState(weekday), officeWorkState(weekday)+1, dayStart, 9, 0),
				newTransition("office-dimmer", officeWorkState(weekday)+1, eveningOfficeState(weekday), dayStart, 17, 0),
				newTransition("office-dimmer", eveningOfficeState(weekday), 0, dayStart, 21, 0),
			)
			transitions = appendIfTransitionChanged(
				transitions,
				"living-room-scene",
				1,
				stateForLivingRoom(weekday, 2),
				dayStart,
				18,
				0,
			)
		}
	}

	return holdings, transitions
}

// stateForLivingRoom returns the living-room state pattern for one part of the day.
func stateForLivingRoom(weekday bool, slot int) int {
	switch slot {
	case 0:
		return 0
	case 2:
		if weekday {
			return 2
		}
		return 1
	default:
		return 1
	}
}

// stateForBedroom returns the bedroom state used for the current day type.
func stateForBedroom(weekday bool) int {
	if weekday {
		return 1
	}
	return 2
}

// officeWorkState returns the office state used during working hours.
func officeWorkState(weekday bool) int {
	if weekday {
		return 2
	}
	return 1
}

// eveningOfficeState returns the office state used during evening hours.
func eveningOfficeState(weekday bool) int {
	if weekday {
		return 1
	}
	return 3
}

// newHolding builds one holding seed from a day anchor and local clock times.
func newHolding(controlID string, state int, dayStart time.Time, startHour int, startMinute int, endHour int, endMinute int) HoldingSeed {
	start := dayStart.Add(time.Duration(startHour)*time.Hour + time.Duration(startMinute)*time.Minute)
	end := dayStart.Add(time.Duration(endHour)*time.Hour + time.Duration(endMinute)*time.Minute)
	return HoldingSeed{
		ControlID: controlID,
		ModelID:   DefaultModelID,
		State:     state,
		Start:     start,
		End:       end,
	}
}

// newTransition builds one transition seed from a day anchor and local clock time.
func newTransition(controlID string, fromState int, toState int, dayStart time.Time, hour int, minute int) TransitionSeed {
	return TransitionSeed{
		ControlID: controlID,
		ModelID:   DefaultModelID,
		FromState: fromState,
		ToState:   toState,
		At:        dayStart.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute),
	}
}

// appendIfTransitionChanged records a transition only when the state actually changes.
func appendIfTransitionChanged(
	transitions []TransitionSeed,
	controlID string,
	fromState int,
	toState int,
	dayStart time.Time,
	hour int,
	minute int,
) []TransitionSeed {
	if fromState == toState {
		return transitions
	}
	return append(transitions, newTransition(controlID, fromState, toState, dayStart, hour, minute))
}
