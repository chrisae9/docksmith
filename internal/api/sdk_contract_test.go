package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSDKContainerHandlers(t *testing.T) {
	for _, version := range []string{"1.40", "1.52"} {
		t.Run(version, func(t *testing.T) {
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", version)
					return
				}
				assert.True(t, strings.HasPrefix(r.URL.Path, "/v"+version+"/"))
				path := strings.TrimPrefix(r.URL.Path, "/v"+version)
				switch path {
				case "/containers/json":
					_, _ = w.Write([]byte(`[{"Id":"app-id","Names":["/app"],"Image":"example/app:1","State":"running"}]`))
				case "/containers/app-id/json":
					_, _ = w.Write([]byte(`{"Id":"app-id","Name":"/app","Config":{"Image":"example/app:1","ExposedPorts":{"80/tcp":{}}},"State":{"Status":"running","Running":true,"Health":{"Status":"healthy"}},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]},"Networks":{"bridge":{"IPAddress":"172.17.0.2","Gateway":"172.17.0.1","MacAddress":"02:42:ac:11:00:02"},"empty":{}}}}`))
				case "/containers/app-id/stats":
					assert.Equal(t, "false", r.URL.Query().Get("stream"))
					assert.Empty(t, r.URL.Query().Get("one-shot"))
					_, _ = w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":2}},"precpu_stats":{"cpu_usage":{"total_usage":1}}}`))
				case "/containers/app-id/logs":
					assert.Equal(t, "1", r.URL.Query().Get("stdout"))
					assert.Equal(t, "1", r.URL.Query().Get("stderr"))
					_, _ = w.Write([]byte("hello\n"))
				case "/containers/app-id/start":
					assert.Equal(t, http.MethodPost, r.Method)
					w.WriteHeader(http.StatusNoContent)
				case "/containers/app-id/stop", "/containers/app-id/restart":
					assert.Equal(t, "9", r.URL.Query().Get("t"))
					w.WriteHeader(http.StatusNoContent)
				case "/containers/app-id":
					assert.Equal(t, http.MethodDelete, r.Method)
					assert.Equal(t, "1", r.URL.Query().Get("force"))
					assert.Equal(t, "1", r.URL.Query().Get("v"))
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, path)
					http.NotFound(w, r)
				}
			}))
			defer daemon.Close()
			t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(daemon.URL, "http://"))
			t.Setenv("DOCKER_API_VERSION", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			service, err := docker.NewService()
			require.NoError(t, err)
			defer service.Close()
			server := &Server{dockerService: service}
			call := func(method, path string, handler http.HandlerFunc) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				req.SetPathValue("name", "app")
				rec := httptest.NewRecorder()
				handler(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				return rec
			}
			inspect := call("GET", "/api/containers/app/inspect", server.handleContainerInspect)
			var envelope struct {
				Data ContainerInspectResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(inspect.Body.Bytes(), &envelope))
			got := envelope.Data
			require.Equal(t, "app-id", got.ID)
			require.True(t, got.Config.ExposedPorts["80/tcp"])
			require.Equal(t, "running", got.State.Status)
			require.Equal(t, "healthy", got.State.Health)
			require.Equal(t, "172.17.0.2", got.NetworkSettings.IPAddress)
			require.Equal(t, "172.17.0.1", got.NetworkSettings.Gateway)
			require.Equal(t, "02:42:ac:11:00:02", got.NetworkSettings.MacAddress)
			require.Empty(t, got.NetworkSettings.Networks["empty"].IPAddress)
			require.Empty(t, got.NetworkSettings.Networks["empty"].MacAddress)
			require.Equal(t, "0.0.0.0", got.NetworkSettings.Ports["80/tcp"][0].HostIP)
			call("GET", "/api/containers/app/stats", server.handleContainerStats)
			logs := call("GET", "/api/containers/app/logs", server.handleContainerLogs)
			require.Contains(t, logs.Body.String(), "hello")
			call("POST", "/api/containers/app/start", server.handleContainerStart)
			call("POST", "/api/containers/app/stop?timeout=9", server.handleContainerStop)
			call("POST", "/api/containers/app/restart?timeout=9", server.handleContainerRestart)
			call("DELETE", "/api/containers/app?force=true&volumes=true", server.handleContainerRemove)
		})
	}
}

func TestSDKContainerOperationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		handler       func(*Server) http.HandlerFunc
		want          int
	}{
		{"start", "already started", func(s *Server) http.HandlerFunc { return s.handleContainerStart }, 200},
		{"stop", "container is not running", func(s *Server) http.HandlerFunc { return s.handleContainerStop }, 200},
		{"restart", "daemon failure", func(s *Server) http.HandlerFunc { return s.handleContainerRestart }, 500},
		{"remove", "daemon failure", func(s *Server) http.HandlerFunc { return s.handleContainerRemove }, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.47")
					return
				}
				if strings.HasSuffix(r.URL.Path, "/containers/json") {
					_, _ = w.Write([]byte(`[{"Id":"app-id","Names":["/app"],"State":"exited"}]`))
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": tc.message})
			}))
			defer daemon.Close()
			t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(daemon.URL, "http://"))
			t.Setenv("DOCKER_API_VERSION", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			service, err := docker.NewService()
			require.NoError(t, err)
			defer service.Close()
			server := &Server{dockerService: service}
			req := httptest.NewRequest("POST", "/api/containers/app", nil)
			req.SetPathValue("name", "app")
			rec := httptest.NewRecorder()
			tc.handler(server)(rec, req)
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.want == 500 {
				require.Contains(t, rec.Body.String(), tc.message)
			}
		})
	}
}

func TestSDKEnvironmentVersionAndHostname(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.47/info", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Name":"docker-host"}`))
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(daemon.URL, "http://"))
	t.Setenv("DOCKER_API_VERSION", "1.47")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	service, err := docker.NewService()
	require.NoError(t, err)
	defer service.Close()
	require.Equal(t, "docker-host", resolveHostname(service))
}
