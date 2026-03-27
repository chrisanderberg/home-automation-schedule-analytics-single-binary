package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"home-automation-schedule-analytics-single-bin/internal/storage"
	"home-automation-schedule-analytics-single-bin/internal/view"
)

const (
	minControlStates = 2
	maxControlStates = 10
)

var defaultSliderLabels = []string{"min", "trans 1", "trans 2", "trans 3", "trans 4", "max"}
var defaultRadioButtonLabels = []string{"on", "off"}

// controlInput is the normalized control form payload shared by API and page handlers.
type controlInput struct {
	ControlID   string
	ControlType string
	NumStates   int
	StateLabels []string
}

// clampStateCount limits free-form state counts to the supported range.
func clampStateCount(raw int) int {
	if raw < 0 {
		return 0
	}
	if raw > maxControlStates {
		return maxControlStates
	}
	return raw
}

// validateControlInput converts a control form payload into a validated storage model.
func validateControlInput(input controlInput) (storage.Control, string) {
	controlID := strings.TrimSpace(input.ControlID)
	if controlID == "" {
		return storage.Control{}, "invalid controlId"
	}
	if controlID == "new" {
		return storage.Control{}, "invalid controlId"
	}
	if input.NumStates < minControlStates || input.NumStates > maxControlStates {
		return storage.Control{}, "invalid numStates"
	}

	controlType := strings.TrimSpace(input.ControlType)
	switch controlType {
	case "discrete":
		controlType = string(storage.ControlTypeRadioButtons)
	case "slider", "continuous":
		controlType = string(storage.ControlTypeSliders)
	}
	// Validation normalizes legacy aliases up front so the rest of the system
	// only has to reason about the canonical control-type vocabulary.
	if controlType != string(storage.ControlTypeRadioButtons) && controlType != string(storage.ControlTypeSliders) {
		return storage.Control{}, "invalid controlType"
	}
	if controlType == string(storage.ControlTypeSliders) && input.NumStates != 6 {
		return storage.Control{}, "sliders must use exactly 6 states"
	}

	labels := make([]string, len(input.StateLabels))
	allBlank := true
	for i, label := range input.StateLabels {
		labels[i] = strings.TrimSpace(label)
		if labels[i] != "" {
			allBlank = false
		}
	}
	if len(labels) > 0 && len(labels) != input.NumStates {
		return storage.Control{}, "stateLabels must have exactly numStates elements when provided"
	}
	if allBlank {
		labels = nil
	}

	return storage.Control{
		ControlID:   controlID,
		ControlType: storage.ControlType(controlType),
		NumStates:   input.NumStates,
		StateLabels: labels,
	}, ""
}

// parseControlForm reads and validates the control form submission.
func parseControlForm(r *http.Request) (storage.Control, view.ControlFormData, string) {
	if err := r.ParseForm(); err != nil {
		return storage.Control{}, view.ControlFormData{}, "invalid form submission"
	}

	controlType := strings.TrimSpace(r.FormValue("controlType"))
	numStates, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("numStates")))
	numStates = clampStateCount(numStates)
	if controlType == string(storage.ControlTypeSliders) {
		numStates = len(defaultSliderLabels)
	}
	form := view.ControlFormData{
		ControlID:   strings.TrimSpace(r.FormValue("controlId")),
		ControlType: controlType,
		NumStates:   numStates,
		StateLabels: make([]string, numStates),
	}

	rawLabels := r.Form["stateLabel"]
	for i := 0; i < numStates; i++ {
		if i < len(rawLabels) {
			form.StateLabels[i] = strings.TrimSpace(rawLabels[i])
		}
	}

	if form.ControlType == string(storage.ControlTypeSliders) {
		// Slider controls always expose the fixed six-state analytical shape, so
		// blank or missing labels are completed before validation runs.
		if len(form.StateLabels) < len(defaultSliderLabels) {
			form.StateLabels = append(form.StateLabels, defaultSliderLabels[len(form.StateLabels):]...)
		}
		for i := 0; i < len(form.StateLabels) && i < len(defaultSliderLabels); i++ {
			if strings.TrimSpace(form.StateLabels[i]) == "" {
				form.StateLabels[i] = defaultSliderLabels[i]
			}
		}
	}
	if form.ControlType == string(storage.ControlTypeRadioButtons) {
		// Radio-button controls get non-blank defaults so the form stays readable
		// even when the user has not named every state yet.
		applyDefaultRadioButtonLabels(form.StateLabels)
	}

	control, errMsg := validateControlInput(controlInput{
		ControlID:   form.ControlID,
		ControlType: form.ControlType,
		NumStates:   form.NumStates,
		StateLabels: form.StateLabels,
	})
	if errMsg != "" {
		return storage.Control{}, form, errMsg
	}

	form.ControlID = control.ControlID
	form.ControlType = string(control.ControlType)
	form.NumStates = control.NumStates
	form.StateLabels = control.StateLabels
	if form.StateLabels == nil {
		// The view layer always receives a concrete slice so templates can index
		// state labels without carrying nil checks through the markup.
		form.StateLabels = make([]string, form.NumStates)
	}
	return control, form, ""
}

// newControlFormData projects stored control metadata into form defaults.
func newControlFormData(control storage.Control) view.ControlFormData {
	numStates := control.NumStates
	if numStates < minControlStates || numStates > maxControlStates {
		numStates = minControlStates
	}
	form := view.ControlFormData{
		ControlID:   control.ControlID,
		ControlType: string(control.ControlType),
		NumStates:   numStates,
	}
	if form.ControlType == "" {
		form.ControlType = string(storage.ControlTypeRadioButtons)
	}
	form.StateLabels = make([]string, form.NumStates)
	copy(form.StateLabels, control.StateLabels)
	if form.ControlType == string(storage.ControlTypeSliders) && allLabelsBlank(form.StateLabels) {
		// Persisted slider metadata may omit labels, but the edit form always
		// rehydrates the fixed default label set expected by the UI.
		form.NumStates = len(defaultSliderLabels)
		form.StateLabels = append([]string(nil), defaultSliderLabels...)
	}
	if form.ControlType == string(storage.ControlTypeRadioButtons) {
		applyDefaultRadioButtonLabels(form.StateLabels)
	}
	return form
}

// controlFormPageData packages one control form render with route and locking state.
func controlFormPageData(form view.ControlFormData, existingControlID string, hasAggregates bool, errMsg string) view.ControlFormPageData {
	data := view.ControlFormPageData{
		Form:            form,
		ExistingControl: existingControlID,
		LockedStructure: hasAggregates,
	}
	if errMsg != "" {
		data.Errors = []string{errMsg}
	}
	if existingControlID == "" {
		data.Title = "New Control"
		data.Heading = "New control"
		data.Action = "/controls/new"
		data.SubmitLabel = "Create control"
		data.CancelURL = "/"
		return data
	}

	data.Title = "Edit " + existingControlID
	data.Heading = "Edit control"
	data.Action = "/controls/" + url.PathEscape(existingControlID)
	data.SubmitLabel = "Save control"
	data.CancelURL = "/controls/" + url.PathEscape(existingControlID)
	if hasAggregates {
		data.StructureHint = "Control type and state count are locked because aggregate data already exists for this control."
	}
	return data
}

// parseModelForm reads and validates the model row form submission.
func parseModelForm(r *http.Request) (storage.Model, view.ModelFormData, string) {
	if err := r.ParseForm(); err != nil {
		return storage.Model{}, view.ModelFormData{}, "invalid form submission"
	}
	modelID := strings.TrimSpace(r.FormValue("modelId"))
	form := view.ModelFormData{
		ModelID: modelID,
	}
	if modelID == "" {
		return storage.Model{}, form, "invalid modelId"
	}
	if modelID == "new" {
		return storage.Model{}, form, "invalid modelId"
	}
	return storage.Model{
		ControlID: r.PathValue("controlID"),
		ModelID:   modelID,
	}, form, ""
}

// renderControlFormPage writes the standalone control form page.
func renderControlFormPage(w http.ResponseWriter, r *http.Request, data view.ControlFormPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := view.ControlFormPage(data).Render(r.Context(), w); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// loadExistingControl fetches the control targeted by the current route and whether it has aggregates.
func loadExistingControl(r *http.Request, db *sql.DB) (storage.Control, bool, error) {
	controlID := r.PathValue("controlID")
	control, err := storage.GetControl(r.Context(), db, controlID)
	if err != nil {
		return storage.Control{}, false, err
	}
	keys, err := storage.ListAggregateKeys(r.Context(), db, controlID)
	if err != nil {
		return storage.Control{}, false, err
	}
	return control, len(keys) > 0, nil
}

// rejectStructuralChange reports a user-facing error when a locked control shape changes.
func rejectStructuralChange(existing storage.Control, next storage.Control) string {
	if existing.NumStates != next.NumStates {
		return "cannot change state count after aggregate data has been recorded"
	}
	if existing.ControlType != next.ControlType {
		return "cannot change control type after aggregate data has been recorded"
	}
	return ""
}

// mapControlSaveError translates storage save failures into form error text.
func mapControlSaveError(err error) string {
	switch {
	case errors.Is(err, storage.ErrConflict):
		return "control ID already exists"
	case errors.Is(err, storage.ErrStructureLocked):
		return "cannot change control structure after aggregate data has been recorded"
	default:
		return "failed to save control"
	}
}

// mapModelSaveError translates model save failures into form error text.
func mapModelSaveError(err error) string {
	switch {
	case errors.Is(err, storage.ErrValidation):
		return "model ID is required"
	case errors.Is(err, storage.ErrConflict):
		return "model ID already exists"
	case errors.Is(err, storage.ErrNotFound):
		return "model ID was not found"
	default:
		return "failed to save model"
	}
}

// allLabelsBlank reports whether a label set contains any user-provided text.
func allLabelsBlank(labels []string) bool {
	for _, label := range labels {
		if strings.TrimSpace(label) != "" {
			return false
		}
	}
	return true
}

// applyDefaultRadioButtonLabels fills the default radio-button labels required by the UI.
func applyDefaultRadioButtonLabels(labels []string) {
	if len(labels) == 0 {
		return
	}
	if strings.TrimSpace(labels[0]) == "" {
		labels[0] = defaultRadioButtonLabels[0]
	}
	if len(labels) > 1 && strings.TrimSpace(labels[1]) == "" {
		labels[1] = defaultRadioButtonLabels[1]
	}
	for i := 2; i < len(labels); i++ {
		if strings.TrimSpace(labels[i]) == "" {
			labels[i] = "state " + strconv.Itoa(i+1)
		}
	}
}
