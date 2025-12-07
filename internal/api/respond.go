package api

import (
	"net/http"

	"github.com/chis/docksmith/internal/output"
)

// RespondError writes an error response with the specified HTTP status code.
// This is the unified error response function - prefer using this over specific status functions.
func RespondError(w http.ResponseWriter, statusCode int, err error) {
	w.WriteHeader(statusCode)
	output.WriteJSONError(w, err)
}

// RespondBadRequest writes a 400 Bad Request error response
func RespondBadRequest(w http.ResponseWriter, err error) {
	RespondError(w, http.StatusBadRequest, err)
}

// RespondNotFound writes a 404 Not Found error response
func RespondNotFound(w http.ResponseWriter, err error) {
	RespondError(w, http.StatusNotFound, err)
}

// RespondInternalError writes a 500 Internal Server Error response
func RespondInternalError(w http.ResponseWriter, err error) {
	RespondError(w, http.StatusInternalServerError, err)
}

// RespondErrorWithData writes an error response that includes data (e.g., for partial failures)
func RespondErrorWithData(w http.ResponseWriter, statusCode int, err error, data any) {
	w.WriteHeader(statusCode)
	output.WriteJSONErrorWithData(w, err, data)
}

// RespondSuccess writes a 200 OK response with data
func RespondSuccess(w http.ResponseWriter, data any) {
	output.WriteJSONData(w, data)
}

// RespondNoContent writes a 204 No Content response
func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
