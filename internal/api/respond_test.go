package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseWriteGraceDoesNotShortenServerDeadline(t *testing.T) {
	assert.Greater(t, responseWriteGrace, serverWriteTimeout)
}

func TestRespondSuccess_ExtendsWriteDeadline(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		RespondSuccess(w, map[string]string{"ok": "yes"})
	}))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"ok"`)
}

func TestRespondSuccess_SetsContentLengthAndCompactBody(t *testing.T) {
	w := httptest.NewRecorder()
	RespondSuccess(w, map[string]string{"ok": "yes"})

	assert.Equal(t, strconv.Itoa(w.Body.Len()), w.Header().Get("Content-Length"))
	assert.NotContains(t, w.Body.String(), "\n")

	var resp struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "yes", resp.Data["ok"])
}
