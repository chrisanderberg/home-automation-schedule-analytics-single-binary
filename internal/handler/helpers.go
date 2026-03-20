package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var errMultipleJSONValues = errors.New("request body must only contain a single JSON value")

// writeJSON sends a JSON response with the requested status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError sends the repository's standard JSON error envelope.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeStrictJSON decodes exactly one JSON value and rejects unknown fields.
func decodeStrictJSON(r io.Reader, v any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errMultipleJSONValues
	}
	return nil
}
