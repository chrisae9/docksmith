package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise Docksmith's mappings against both legacy and current Engine wire
// formats, with real HTTP requests and automatic API negotiation.
func TestSDKServiceContract(t *testing.T) {
	for _, version := range []string{"1.40", "1.47", "1.52", "1.56"} {
		t.Run(version, func(t *testing.T) {
			var diskReads atomic.Int32
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", version)
					return
				}
				prefix := "/v" + version
				if !strings.HasPrefix(r.URL.Path, prefix+"/") {
					t.Errorf("unexpected API path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				path := strings.TrimPrefix(r.URL.Path, prefix)
				switch path {
				case "/containers/json":
					assert.Equal(t, "1", r.URL.Query().Get("all"))
					if f := r.URL.Query().Get("filters"); f != "" {
						assert.Contains(t, f, "^/app$")
					}
					_, _ = w.Write([]byte(`[{"Id":"app-id","Names":["/app"],"Image":"example/app:1","ImageID":"sha256:image","State":"running","Status":"Up (healthy)","Labels":{},"Mounts":[{"Type":"volume","Name":"data"}]}]`))
				case "/images/json":
					_, _ = w.Write([]byte(`[{"Id":"sha256:image","RepoTags":["example/app:1"],"Size":123}]`))
				case "/images/example/app:1/json":
					_, _ = w.Write([]byte(`{"Id":"sha256:image","RepoDigests":["example/app@sha256:digest"],"Config":{"Labels":{"org.opencontainers.image.version":"1.2.3"}}}`))
				case "/networks":
					_, _ = w.Write([]byte(`[{"Id":"net-id","Name":"bridge","Driver":"bridge","Scope":"local","Created":"2026-01-01T00:00:00Z"}]`))
				case "/networks/net-id":
					_, _ = w.Write([]byte(`{"Containers":{"app-id":{"Name":"app"}}}`))
				case "/volumes":
					_, _ = w.Write([]byte(`{"Volumes":[{"Name":"data","Driver":"local","Mountpoint":"/data"}]}`))
				case "/system/df":
					diskReads.Add(1)
					assert.Equal(t, []string{"volume"}, r.URL.Query()["type"])
					assert.Equal(t, "1", r.URL.Query().Get("verbose"))
					if version == "1.40" || version == "1.47" {
						_, _ = w.Write([]byte(`{"Volumes":[{"Name":"data","UsageData":{"Size":42}}]}`))
					} else {
						_, _ = w.Write([]byte(`{"VolumeUsage":{"Items":[{"Name":"data","UsageData":{"Size":42}}]}}`))
					}
				default:
					t.Errorf("unexpected request %s %s", r.Method, path)
					http.NotFound(w, r)
				}
			}))
			defer daemon.Close()
			cli, err := client.New(client.WithHost("tcp://" + strings.TrimPrefix(daemon.URL, "http://")))
			require.NoError(t, err)
			defer cli.Close()
			service := &Service{cli: cli}
			ctx := context.Background()
			containers, err := service.ListContainers(ctx)
			require.NoError(t, err)
			require.Len(t, containers, 1)
			require.Equal(t, "running", containers[0].State)
			one, err := service.GetContainerByName(ctx, "app")
			require.NoError(t, err)
			require.Equal(t, "app-id", one.ID)
			explorer, err := service.ListContainersForExplorer(ctx)
			require.NoError(t, err)
			require.Equal(t, "healthy", explorer[0].HealthStatus)
			images, err := service.ListImages(ctx)
			require.NoError(t, err)
			require.True(t, images[0].InUse)
			local, err := service.IsLocalImage(ctx, "example/app:1")
			require.NoError(t, err)
			require.False(t, local)
			label, err := service.GetImageVersion(ctx, "example/app:1")
			require.NoError(t, err)
			require.Equal(t, "1.2.3", label)
			digest, err := service.GetImageDigest(ctx, "example/app:1")
			require.NoError(t, err)
			require.Equal(t, "sha256:digest", digest)
			networks, err := service.ListNetworks(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"app"}, networks[0].Containers)
			volumes, err := service.ListVolumesWithContainers(ctx, nil, false)
			require.NoError(t, err)
			require.EqualValues(t, -1, volumes[0].Size)
			require.Zero(t, diskReads.Load())
			volumes, err = service.ListVolumes(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 42, volumes[0].Size)
			require.Equal(t, []string{"app"}, volumes[0].Containers)
			require.EqualValues(t, 1, diskReads.Load())
			require.Equal(t, version, cli.ClientVersion())
		})
	}
}

func TestSDKServicePruneAndRemove(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/v1.47")
		if r.Method == http.MethodDelete {
			switch path {
			case "/volumes/data":
				assert.Equal(t, "1", r.URL.Query().Get("force"))
			case "/images/image":
				assert.Equal(t, "1", r.URL.Query().Get("force"))
				assert.Empty(t, r.URL.Query().Get("noprune"))
				_, _ = w.Write([]byte(`[{"Deleted":"image"}]`))
				return
			case "/networks/net":
			default:
				t.Errorf("unexpected delete %s", path)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		assert.Equal(t, http.MethodPost, r.Method)
		if path == "/images/prune" {
			var filters map[string]map[string]bool
			assert.NoError(t, json.Unmarshal([]byte(r.URL.Query().Get("filters")), &filters))
			assert.True(t, filters["dangling"]["true"])
		}
		_, _ = w.Write([]byte(`{"ContainersDeleted":["ctr"],"ImagesDeleted":[{"Deleted":"img"}],"NetworksDeleted":["net"],"VolumesDeleted":["vol"],"SpaceReclaimed":7}`))
	}))
	defer daemon.Close()
	cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(daemon.URL, "http://")), client.WithAPIVersion("1.47"))
	require.NoError(t, err)
	defer cli.Close()
	s := &Service{cli: cli}
	ctx := context.Background()
	require.NoError(t, s.RemoveNetwork(ctx, "net"))
	require.NoError(t, s.RemoveVolume(ctx, "data", true))
	deleted, err := s.RemoveImage(ctx, "image", true)
	require.NoError(t, err)
	require.Equal(t, "image", deleted[0].Deleted)
	report, err := s.PruneContainers(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ctr"}, report.ItemsDeleted)
	require.EqualValues(t, 7, report.SpaceReclaimed)
	report, err = s.PruneImages(ctx, false)
	require.NoError(t, err)
	require.Equal(t, []string{"img"}, report.ItemsDeleted)
	report, err = s.PruneNetworks(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"net"}, report.ItemsDeleted)
	report, err = s.PruneVolumes(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"vol"}, report.ItemsDeleted)
}

func TestSDKPathMountDiscovery(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "self", true: "fallback"}[fallback], func(t *testing.T) {
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				path := strings.TrimPrefix(r.URL.Path, "/v1.47")
				if path == "/containers/json" {
					_, _ = w.Write([]byte(`[{"Id":"self-id","Names":["/docksmith"]}]`))
					return
				}
				if fallback && path != "/containers/self-id/json" {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"not found"}`))
					return
				}
				_, _ = w.Write([]byte(`{"Mounts":[{"Type":"bind","Source":"/srv/stacks","Destination":"/stacks"}]}`))
			}))
			defer daemon.Close()
			cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(daemon.URL, "http://")), client.WithAPIVersion("1.47"))
			require.NoError(t, err)
			defer cli.Close()
			translator := &PathTranslator{client: cli, mappings: make(map[string]string), inDocker: true}
			require.NoError(t, translator.discoverMounts())
			require.Equal(t, "/stacks/app/compose.yaml", translator.TranslateToContainer("/srv/stacks/app/compose.yaml"))
		})
	}
}
