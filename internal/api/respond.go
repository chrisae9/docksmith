package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/update"
)

// extendWriteDeadline refreshes the response write budget. A long synchronous
// handler can outlive http.Server.WriteTimeout, which would make the response
// unwritable even though the work succeeded.
func extendWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(responseWriteGrace))
}

// RespondError writes an error response with the specified HTTP status code.
// This is the unified error response function - prefer using this over specific status functions.
func RespondError(w http.ResponseWriter, statusCode int, err error) {
	extendWriteDeadline(w)
	w.Header().Set("Content-Type", "application/json")
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
	extendWriteDeadline(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	output.WriteJSONErrorWithData(w, err, data)
}

// RespondSuccess writes a 200 OK response with data
func RespondSuccess(w http.ResponseWriter, data any) {
	extendWriteDeadline(w)
	w.Header().Set("Content-Type", "application/json")
	output.WriteJSONData(w, data)
}

// RespondNoContent writes a 204 No Content response
func RespondNoContent(w http.ResponseWriter) {
	extendWriteDeadline(w)
	w.WriteHeader(http.StatusNoContent)
}

// RespondOrchestratorError maps orchestrator errors to appropriate HTTP status codes.
func RespondOrchestratorError(w http.ResponseWriter, err error) {
	var notFoundErr *update.NotFoundError
	var badReqErr *update.BadRequestError
	switch {
	case errors.As(err, &notFoundErr):
		RespondNotFound(w, err)
	case errors.As(err, &badReqErr):
		RespondBadRequest(w, err)
	default:
		RespondInternalError(w, err)
	}
}
