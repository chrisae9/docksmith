package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSDKOrchestratorHealth(t *testing.T) {
	for _, health := range []string{"healthy", "unhealthy", "none"} {
		t.Run(health, func(t *testing.T) {
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1.47/containers/app/json", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if health == "none" {
					_, _ = w.Write([]byte(`{"State":{"Running":true}}`))
					return
				}
				_, _ = w.Write([]byte(`{"State":{"Running":true,"Health":{"Status":"` + health + `"}}}`))
			}))
			defer daemon.Close()
			cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(daemon.URL, "http://")), client.WithAPIVersion("1.47"))
			require.NoError(t, err)
			defer cli.Close()
			orchestrator := &UpdateOrchestrator{dockerSDK: cli}
			err = orchestrator.waitForHealthy(context.Background(), "app", time.Second)
			if health == "unhealthy" {
				require.ErrorContains(t, err, "unhealthy")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSDKOrchestratorPull(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "progress", true: "missing_tag"}[missing], func(t *testing.T) {
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1.47/images/create", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "docker.io/example/app", r.URL.Query().Get("fromImage"))
				assert.Equal(t, "1", r.URL.Query().Get("tag"))
				w.Header().Set("Content-Type", "application/json")
				if missing {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"manifest unknown"}`))
					return
				}
				_, _ = w.Write([]byte("{\"status\":\"Downloading\",\"id\":\"layer\",\"progressDetail\":{\"current\":1,\"total\":2}}\n{\"status\":\"Pull complete\",\"id\":\"layer\"}\n"))
			}))
			defer daemon.Close()
			cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(daemon.URL, "http://")), client.WithAPIVersion("1.47"))
			require.NoError(t, err)
			defer cli.Close()
			orchestrator := &UpdateOrchestrator{dockerSDK: cli}
			progress := make(chan PullProgress, 10)
			err = orchestrator.pullImage(context.Background(), "example/app:1", progress)
			if missing {
				require.ErrorContains(t, err, "image tag does not exist")
				return
			}
			require.NoError(t, err)
			require.Len(t, progress, 2)
			require.Equal(t, 25, (<-progress).Percent)
			require.Equal(t, 100, (<-progress).Percent)
		})
	}
}
