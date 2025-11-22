package api

import (
	"net/http"

	"github.com/chis/docksmith/internal/output"
)

// RespondBadRequest writes a 400 Bad Request error response
func RespondBadRequest(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	output.WriteJSONError(w, err)
}

// RespondNotFound writes a 404 Not Found error response
func RespondNotFound(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusNotFound)
	output.WriteJSONError(w, err)
}

// RespondInternalError writes a 500 Internal Server Error response
func RespondInternalError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	output.WriteJSONError(w, err)
}

// RespondSuccess writes a 200 OK response with data
func RespondSuccess(w http.ResponseWriter, data any) {
	output.WriteJSONData(w, data)
}

// RespondNoContent writes a 204 No Content response
func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
