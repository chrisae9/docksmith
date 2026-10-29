package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGzipMiddleware_CompressesJSON(t *testing.T) {
	body := []byte(`{"payload":"` + strings.Repeat("x", 2000) + `"}`)
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")
	assert.Empty(t, rec.Header().Get("Content-Length"))

	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestGzipMiddleware_SkipsEventStream(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: ping\ndata: 1\n\n"))
		w.(http.Flusher).Flush()
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "event: ping\ndata: 1\n\n", rec.Body.String())
}

func TestGzipMiddleware_SkipsWhenNotAcceptedOrTooSmall(t *testing.T) {
	cases := []struct {
		name        string
		encoding    string
		contentType string
		size        int
	}{
		{"no accept-encoding", "", "text/plain", 2000},
		{"gzip refused with q=0", "gzip;q=0", "text/plain", 2000},
		{"body below minimum size", "gzip", "text/plain", 100},
		// text/plain must never be compressed, regardless of size or acceptance.
		{"text/plain never compressed", "gzip", "text/plain", 2000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Repeat("x", tc.size))
			h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				_, _ = w.Write(body)
			}))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			if tc.encoding != "" {
				req.Header.Set("Accept-Encoding", tc.encoding)
			}
			h.ServeHTTP(rec, req)
			assert.Empty(t, rec.Header().Get("Content-Encoding"), tc.name)
			assert.Equal(t, body, rec.Body.Bytes(), tc.name)
		})
	}
}

func TestGzipMiddleware_CompressesWithoutContentLength(t *testing.T) {
	// No explicit WriteHeader and no Content-Length: the implicit 200 must
	// still be compressed.
	body := []byte(strings.Repeat("y", 2000))
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")

	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestGzipMiddleware_SkipsNon200AndPreEncoded(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
	}{
		{
			name:   "non-200 status",
			status: http.StatusNotFound,
		},
		{
			name:   "already encoded",
			status: http.StatusOK,
			header: map[string]string{"Content-Encoding": "br"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Repeat("z", 2000))
			h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write(body)
			}))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			h.ServeHTTP(rec, req)

			assert.Equal(t, tc.header["Content-Encoding"], rec.Header().Get("Content-Encoding"))
			assert.Equal(t, body, rec.Body.Bytes())
		})
	}
}

func TestGzipMiddleware_DoesNotDuplicateVary(t *testing.T) {
	body := []byte(strings.Repeat("v", 2000))
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin, Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)

	assert.Equal(t, []string{"Origin, Accept-Encoding"}, rec.Header().Values("Vary"))
}

func TestGzipMiddleware_PassesThroughWithoutGzip(t *testing.T) {
	body := []byte(`{"ok":true}`)
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Empty(t, rec.Header().Get("Vary"))
	assert.Equal(t, body, rec.Body.Bytes())
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"GZIP", true},
		{"deflate, gzip;q=0.5", true},
		{"*", true},
		{"deflate", false},
		{"gzip;q=0", false},
		{"gzip; q = 0", false},
		{"gzip;q=0.0", false},
		{"gzip;q=1.0, deflate", true},
		{"*;q=0", false},
		{"identity", false},
		// An explicit gzip refusal wins over the wildcard.
		{"gzip;q=0, *", false},
		{"*;q=0, gzip", true},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, acceptsGzip(tc.header), tc.header)
	}
}

func TestGzipResponseWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &gzipResponseWriter{ResponseWriter: rec}
	assert.Same(t, rec, rw.Unwrap())
}

func TestGzipResponseWriter_FlushPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &gzipResponseWriter{ResponseWriter: rec}
	_, _ = rw.Write([]byte("stream"))
	rw.Flush()

	assert.True(t, rec.Flushed)
	assert.Equal(t, "stream", rec.Body.String())
}

func TestGzipResponseWriter_FlushCompressed(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &gzipResponseWriter{ResponseWriter: rec}
	rw.Header().Set("Content-Type", "application/json")
	_, _ = rw.Write([]byte(strings.Repeat("f", 2000)))
	rw.Flush()
	require.NoError(t, rw.Close())

	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("f", 2000), string(got))
}
