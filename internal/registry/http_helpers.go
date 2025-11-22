package registry

import (
	"fmt"
	"io"
	"net/http"
)

// handleHTTPError reads the response body and returns a formatted error
// for non-200 HTTP responses from registry APIs.
func handleHTTPError(resp *http.Response, operation string) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s: registry returned %d: %s", operation, resp.StatusCode, string(body))
}
