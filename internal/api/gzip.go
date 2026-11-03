package api

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// gzipMinSize is the smallest response we bother compressing. Below it the
// gzip framing overhead outweighs the savings.
const gzipMinSize = 1024

// compressibleMediaTypes lists the response types worth compressing. These are
// the API's JSON payloads and the static text assets served to the UI; binary
// assets (images, fonts) are already compressed.
var compressibleMediaTypes = map[string]bool{
	"application/json":       true,
	"text/html":              true,
	"text/css":               true,
	"text/javascript":        true,
	"application/javascript": true,
	"image/svg+xml":          true,
}

// gzipMiddleware compresses eligible responses when the client accepts gzip.
// It leaves the response untouched otherwise, including event streams.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer func() { _ = gw.Close() }()
		next.ServeHTTP(gw, r)
	})
}

// acceptsGzip reports whether an Accept-Encoding header value allows gzip.
// An explicit "gzip" entry decides the answer; otherwise a "*" entry applies.
// A quality value of zero refuses the coding.
func acceptsGzip(header string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		switch strings.ToLower(strings.TrimSpace(coding)) {
		case "gzip":
			return !isZeroQuality(params)
		case "*":
			star = !isZeroQuality(params)
		}
	}
	return star
}

// isZeroQuality reports whether an Accept-Encoding entry's parameters contain
// an explicit q value of zero.
func isZeroQuality(params string) bool {
	q, ok := qualityValue(params)
	return ok && q == 0
}

// qualityValue extracts the q parameter from an Accept-Encoding entry's
// parameters. The second result is false when no usable q value is present,
// in which case the coding is treated as fully acceptable.
func qualityValue(params string) (float64, bool) {
	for _, param := range strings.Split(params, ";") {
		name, value, ok := strings.Cut(param, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
		return q, true
	}
	return 0, false
}

// isCompressibleMediaType reports whether a Content-Type (with any parameters
// stripped) is worth compressing.
func isCompressibleMediaType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, _ := strings.Cut(contentType, ";")
	return compressibleMediaTypes[strings.ToLower(strings.TrimSpace(mediaType))]
}

// addVaryAcceptEncoding records that the response varies by Accept-Encoding,
// without duplicating an entry that is already present.
func addVaryAcceptEncoding(h http.Header) {
	for _, value := range h.Values("Vary") {
		for _, item := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(item)) {
			case "accept-encoding", "*":
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// gzipResponseWriter decides lazily at the first WriteHeader whether to
// compress. It implements Unwrap so http.ResponseController can still reach
// the underlying writer (write deadlines in the SSE handler).
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	compressing bool
	decided     bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.decided {
		return
	}
	w.decided = true
	w.decide(code)
	w.ResponseWriter.WriteHeader(code)
}

// decide applies the compression rules once, before headers are sent.
func (w *gzipResponseWriter) decide(code int) {
	header := w.Header()
	addVaryAcceptEncoding(header)

	if code != http.StatusOK {
		return
	}
	// Never touch an already-encoded response.
	if header.Get("Content-Encoding") != "" {
		return
	}
	if !isCompressibleMediaType(header.Get("Content-Type")) {
		return
	}
	// An absent Content-Length is fine: the writer uses chunked encoding and we
	// cannot know the size up front.
	if cl := header.Get("Content-Length"); cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil || n < gzipMinSize {
			return
		}
	}

	header.Del("Content-Length")
	header.Set("Content-Encoding", "gzip")
	w.gz = gzip.NewWriter(w.ResponseWriter)
	w.compressing = true
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.compressing {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// Flush flushes buffered compressed bytes, then the underlying writer.
func (w *gzipResponseWriter) Flush() {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.compressing {
		_ = w.gz.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Close finishes the gzip stream, if one was started.
func (w *gzipResponseWriter) Close() error {
	if !w.compressing {
		return nil
	}
	return w.gz.Close()
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
