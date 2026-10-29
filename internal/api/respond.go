package api

import (
	"errors"
	"net/http"
	"strconv"
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

// writeJSONBody writes a pre-marshalled JSON body with an explicit
// Content-Length so clients know exactly how many bytes to expect.
func writeJSONBody(w http.ResponseWriter, status int, body []byte) {
	extendWriteDeadline(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// fallbackJSONBody is written when the response cannot be marshalled.
var fallbackJSONBody = []byte(`{"success":false,"error":"failed to encode JSON"}`)

// RespondError writes an error response with the specified HTTP status code.
// This is the unified error response function - prefer using this over specific status functions.
func RespondError(w http.ResponseWriter, statusCode int, err error) {
	body, marshalErr := output.MarshalJSONError(err)
	if marshalErr != nil {
		writeJSONBody(w, statusCode, fallbackJSONBody)
		return
	}
	writeJSONBody(w, statusCode, body)
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
	body, marshalErr := output.MarshalJSONErrorWithData(err, data)
	if marshalErr != nil {
		writeJSONBody(w, statusCode, fallbackJSONBody)
		return
	}
	writeJSONBody(w, statusCode, body)
}

// RespondSuccess writes a 200 OK response with data
func RespondSuccess(w http.ResponseWriter, data any) {
	body, err := output.MarshalJSONData(data)
	if err != nil {
		writeJSONBody(w, http.StatusInternalServerError, fallbackJSONBody)
		return
	}
	writeJSONBody(w, http.StatusOK, body)
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
