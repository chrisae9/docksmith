package compose

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chis/docksmith/internal/docker"
)

// mockDockerClient implements docker.Client for testing
type mockDockerClient struct {
	containers []docker.Container
	err        error
}

func (m *mockDockerClient) ListContainers(ctx context.Context) ([]docker.Container, error) {
	return m.containers, m.err
}

func (m *mockDockerClient) IsLocalImage(ctx context.Context, imageName string) (bool, error) {
	return false, nil
}

func (m *mockDockerClient) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	return "", nil
}

func (m *mockDockerClient) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	return "", nil
}

func (m *mockDockerClient) Close() error {
	return nil
}

// TestNewRecreator tests creation of Recreator
func TestNewRecreator(t *testing.T) {
	mock := &mockDockerClient{}
	recreator := NewRecreator(mock)

	assert.NotNil(t, recreator)
	assert.Equal(t, mock, recreator.dockerClient)
}

// TestRecreateWithCompose_Validation tests validation in RecreateWithCompose
func TestRecreateWithCompose_Validation(t *testing.T) {
	mock := &mockDockerClient{}
	recreator := NewRecreator(mock)
	ctx := context.Background()

	t.Run("fails when host compose path is empty", func(t *testing.T) {
		container := &docker.Container{
			Name: "test-container",
			Labels: map[string]string{
				"com.docker.compose.service": "test-service",
			},
		}

		err := recreator.RecreateWithCompose(ctx, container, "", "/container/path/docker-compose.yml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no compose file path available")
	})

	t.Run("fails when container compose path is empty", func(t *testing.T) {
		container := &docker.Container{
			Name: "test-container",
			Labels: map[string]string{
				"com.docker.compose.service": "test-service",
			},
		}

		err := recreator.RecreateWithCompose(ctx, container, "/host/path/docker-compose.yml", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no compose file path available")
	})

	t.Run("fails when container has no service label", func(t *testing.T) {
		container := &docker.Container{
			Name:   "test-container",
			Labels: map[string]string{},
		}

		err := recreator.RecreateWithCompose(ctx, container, "/host/path/docker-compose.yml", "/container/path/docker-compose.yml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no com.docker.compose.service label")
	})

	t.Run("fails when service label is empty", func(t *testing.T) {
		container := &docker.Container{
			Name: "test-container",
			Labels: map[string]string{
				"com.docker.compose.service": "",
			},
		}

		err := recreator.RecreateWithCompose(ctx, container, "/host/path/docker-compose.yml", "/container/path/docker-compose.yml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no com.docker.compose.service label")
	})
}

// TestRecreateMultipleServices_Validation tests validation in RecreateMultipleServices
func TestRecreateMultipleServices_Validation(t *testing.T) {
	mock := &mockDockerClient{}
	recreator := NewRecreator(mock)
	ctx := context.Background()

	t.Run("fails when host compose path is empty", func(t *testing.T) {
		err := recreator.RecreateMultipleServices(ctx, "", "/container/path/docker-compose.yml", []string{"service1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no compose file path provided")
	})

	t.Run("fails when container compose path is empty", func(t *testing.T) {
		err := recreator.RecreateMultipleServices(ctx, "/host/path/docker-compose.yml", "", []string{"service1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no compose file path provided")
	})

	t.Run("fails when no services specified", func(t *testing.T) {
		err := recreator.RecreateMultipleServices(ctx, "/host/path/docker-compose.yml", "/container/path/docker-compose.yml", []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no services specified")
	})

	t.Run("fails when services is nil", func(t *testing.T) {
		err := recreator.RecreateMultipleServices(ctx, "/host/path/docker-compose.yml", "/container/path/docker-compose.yml", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no services specified")
	})
}

// TestFindNetworkModeDependents tests finding network_mode dependents
func TestFindNetworkModeDependents(t *testing.T) {
	ctx := context.Background()

	t.Run("finds containers with network_mode pointing to target", func(t *testing.T) {
		mock := &mockDockerClient{
			containers: []docker.Container{
				{
					Name: "vpn-container",
					Labels: map[string]string{
						"com.docker.compose.service": "vpn",
					},
				},
				{
					Name: "app-container",
					Labels: map[string]string{
						"com.docker.compose.service":      "app",
						"com.docker.compose.network_mode": "service:vpn-container",
					},
				},
				{
					Name: "web-container",
					Labels: map[string]string{
						"com.docker.compose.service":      "web",
						"com.docker.compose.network_mode": "service:vpn-container",
					},
				},
				{
					Name: "db-container",
					Labels: map[string]string{
						"com.docker.compose.service": "db",
					},
				},
			},
		}

		recreator := NewRecreator(mock)
		dependents, err := recreator.FindNetworkModeDependents(ctx, "vpn-container")
		require.NoError(t, err)

		assert.Len(t, dependents, 2)
		assert.Contains(t, dependents, "app")
		assert.Contains(t, dependents, "web")
	})

	t.Run("returns empty when no dependents", func(t *testing.T) {
		mock := &mockDockerClient{
			containers: []docker.Container{
				{
					Name: "standalone",
					Labels: map[string]string{
						"com.docker.compose.service": "standalone",
					},
				},
			},
		}

		recreator := NewRecreator(mock)
		dependents, err := recreator.FindNetworkModeDependents(ctx, "target-container")
		require.NoError(t, err)

		assert.Empty(t, dependents)
	})

	t.Run("returns error when listing containers fails", func(t *testing.T) {
		mock := &mockDockerClient{
			err: assert.AnError,
		}

		recreator := NewRecreator(mock)
		_, err := recreator.FindNetworkModeDependents(ctx, "any-container")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list containers")
	})

	t.Run("handles containers without service label", func(t *testing.T) {
		mock := &mockDockerClient{
			containers: []docker.Container{
				{
					Name: "unlabeled",
					Labels: map[string]string{
						"com.docker.compose.network_mode": "service:target",
						// Missing com.docker.compose.service
					},
				},
			},
		}

		recreator := NewRecreator(mock)
		dependents, err := recreator.FindNetworkModeDependents(ctx, "target")
		require.NoError(t, err)

		// Should not include containers without service label
		assert.Empty(t, dependents)
	})
}

// TestWaitForHealthy_Validation tests WaitForHealthy timeout behavior
// Note: Full integration testing would require actual Docker containers
func TestWaitForHealthy_ContextCancellation(t *testing.T) {
	mock := &mockDockerClient{}
	recreator := NewRecreator(mock)

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Should return quickly due to cancelled context
		err := recreator.WaitForHealthy(ctx, "test-container", 0)
		assert.Error(t, err)
	})
}

func TestIsNetworkNamespaceRace(t *testing.T) {
	incident := `Error response from daemon: cannot join network namespace of container: Container 42fb503e6b6f is restarting, wait until the container is running`
	assert.True(t, isNetworkNamespaceRace([]byte(incident)))
	assert.False(t, isNetworkNamespaceRace([]byte("Error response from daemon: pull access denied")))
}

func TestRecreateWithCompose_RetriesNetworkNamespaceRace(t *testing.T) {
	var upCalls int
	orig := dockerCommand
	t.Cleanup(func() { dockerCommand = orig })
	dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "compose" {
			upCalls++
			if upCalls == 1 {
				return []byte("cannot join network namespace of container: Container x is restarting, wait until the container is running"), errors.New("exit status 1")
			}
			return []byte("ok"), nil
		}
		return []byte(""), nil
	}

	r := NewRecreator(&mockDockerClient{})
	c := &docker.Container{Name: "qbittorrent", Labels: map[string]string{
		"com.docker.compose.service": "qbittorrent",
	}}
	require.NoError(t, r.RecreateWithCompose(context.Background(), c, "/host/docker-compose.yaml", "/container/docker-compose.yaml"))
	assert.Equal(t, 2, upCalls)
}

func TestRecreateWithCompose_SkipsWaitWithoutNetworkMode(t *testing.T) {
	calls := 0
	origCmd := dockerCommand
	t.Cleanup(func() { dockerCommand = origCmd })
	dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }

	mock := &countingDockerClient{listCalls: &calls}
	r := NewRecreator(mock)
	c := &docker.Container{Name: "app", Labels: map[string]string{"com.docker.compose.service": "app"}}
	require.NoError(t, r.RecreateWithCompose(context.Background(), c, "/host/docker-compose.yaml", "/container/docker-compose.yaml"))
	assert.Zero(t, calls, "must not list containers when the target has no network_mode label")
}

func TestRecreateWithCompose_WaitsForProvider(t *testing.T) {
	origCmd, origPoll := dockerCommand, networkModePollInterval
	t.Cleanup(func() { dockerCommand = origCmd; networkModePollInterval = origPoll })
	dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }
	networkModePollInterval = time.Millisecond

	mock := &sequencedDockerClient{states: []string{"restarting", "restarting", "running"}}
	r := NewRecreator(mock)
	c := &docker.Container{Name: "qbittorrent", Labels: map[string]string{
		"com.docker.compose.service":      "qbittorrent",
		"com.docker.compose.project":      "engager",
		"com.docker.compose.network_mode": "service:gluetun",
	}}
	require.NoError(t, r.RecreateWithCompose(context.Background(), c, "/host/docker-compose.yaml", "/container/docker-compose.yaml"))
	assert.GreaterOrEqual(t, mock.calls, 3)
}

func TestRecreateWithCompose_FailsWhenProviderNeverRunning(t *testing.T) {
	origCmd, origPoll, origTimeout := dockerCommand, networkModePollInterval, networkModeWaitTimeout
	t.Cleanup(func() {
		dockerCommand = origCmd
		networkModePollInterval = origPoll
		networkModeWaitTimeout = origTimeout
	})
	dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }
	networkModePollInterval = time.Millisecond
	networkModeWaitTimeout = 5 * time.Millisecond

	mock := &sequencedDockerClient{states: []string{"restarting", "restarting", "restarting"}}
	r := NewRecreator(mock)
	c := &docker.Container{Name: "qbittorrent", Labels: map[string]string{
		"com.docker.compose.service":      "qbittorrent",
		"com.docker.compose.project":      "engager",
		"com.docker.compose.network_mode": "service:gluetun",
	}}
	err := r.RecreateWithCompose(context.Background(), c, "/host/docker-compose.yaml", "/container/docker-compose.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gluetun")
}

type countingDockerClient struct{ listCalls *int }

func (m *countingDockerClient) ListContainers(ctx context.Context) ([]docker.Container, error) {
	*m.listCalls++
	return nil, nil
}
func (m *countingDockerClient) IsLocalImage(ctx context.Context, imageName string) (bool, error) {
	return false, nil
}
func (m *countingDockerClient) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	return "", nil
}
func (m *countingDockerClient) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	return "", nil
}
func (m *countingDockerClient) Close() error { return nil }

// sequencedDockerClient returns a gluetun provider whose state walks through
// states on each ListContainers call (last value sticks).
type sequencedDockerClient struct {
	states []string
	calls  int
}

func (m *sequencedDockerClient) ListContainers(ctx context.Context) ([]docker.Container, error) {
	state := m.states[len(m.states)-1]
	if m.calls < len(m.states) {
		state = m.states[m.calls]
	}
	m.calls++
	return []docker.Container{{
		Name:   "gluetun",
		State:  state,
		Labels: map[string]string{"com.docker.compose.service": "gluetun", "com.docker.compose.project": "engager"},
	}}, nil
}
func (m *sequencedDockerClient) IsLocalImage(ctx context.Context, imageName string) (bool, error) {
	return false, nil
}
func (m *sequencedDockerClient) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	return "", nil
}
func (m *sequencedDockerClient) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	return "", nil
}
func (m *sequencedDockerClient) Close() error { return nil }
