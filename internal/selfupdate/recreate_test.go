package selfupdate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHelper swaps runHelperCommand for the duration of the test and returns
// the args the production code tried to run.
func captureHelper(t *testing.T, output []byte, err error) *[]string {
	t.Helper()

	var got []string
	orig := runHelperCommand
	t.Cleanup(func() { runHelperCommand = orig })

	runHelperCommand = func(_ context.Context, args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return output, err
	}
	return &got
}

func TestRecreateSelfRunsComposeInDetachedHelper(t *testing.T) {
	got := captureHelper(t, []byte("helper-id\n"), nil)

	err := RecreateSelf(
		context.Background(),
		"ghcr.io/chrisae9/docksmith:latest",
		"/srv/stacks/app/docker-compose.yaml",
		"docksmith",
		HelperOptions{},
	)
	require.NoError(t, err)

	args := strings.Join(*got, " ")
	for _, want := range []string{
		"run -d --rm",
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v /srv/stacks/app:/srv/stacks/app:ro",
		"--entrypoint sh",
		"ghcr.io/chrisae9/docksmith:latest",
		"docker compose --project-directory '/srv/stacks/app' -f '/srv/stacks/app/docker-compose.yaml' up -d --force-recreate 'docksmith'",
		"sleep 2",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("helper command %q missing %q", args, want)
		}
	}
}

func TestRestartSelfRunsComposeRestartInDetachedHelper(t *testing.T) {
	got := captureHelper(t, []byte("helper-id\n"), nil)

	err := RestartSelf(
		context.Background(),
		"ghcr.io/chrisae9/docksmith:latest",
		"/srv/stacks/app/docker-compose.yaml",
		"docksmith",
		HelperOptions{},
	)
	require.NoError(t, err)

	args := strings.Join(*got, " ")
	if !strings.Contains(args, "docker compose --project-directory '/srv/stacks/app' -f '/srv/stacks/app/docker-compose.yaml' restart 'docksmith'") {
		t.Errorf("helper command %q does not run compose restart", args)
	}
	if strings.Contains(args, "--force-recreate") {
		t.Errorf("restart helper should not force-recreate: %q", args)
	}
}

// Docksmith's bind mappings are reproduced read-only at their container
// destinations, and mirrored at their host paths when the two differ.
func TestRecreateSelfMountsDocksmithBindings(t *testing.T) {
	got := captureHelper(t, nil, nil)

	err := RecreateSelf(
		context.Background(),
		"docksmith:latest",
		"/srv/stacks/app/docker-compose.yaml",
		"docksmith",
		HelperOptions{Mounts: map[string]string{
			"/srv/stacks/":  "/stacks/",
			"/srv/secrets/": "/srv/secrets/",
		}},
	)
	require.NoError(t, err)

	args := strings.Join(*got, " ")
	for _, want := range []string{
		"-v /srv/stacks:/stacks:ro",
		"-v /srv/stacks:/srv/stacks:ro",
		"-v /srv/secrets:/srv/secrets:ro",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("helper command %q missing %q", args, want)
		}
	}
}

// Process interpolation variables are forwarded, and HelperOptions wins.
func TestRecreateSelfForwardsInterpolationEnvironment(t *testing.T) {
	t.Setenv("DOCKS_IT_ENV_TEST", "from-process")
	got := captureHelper(t, nil, nil)

	err := RecreateSelf(
		context.Background(),
		"docksmith:latest",
		"/srv/stacks/app/docker-compose.yaml",
		"docksmith",
		HelperOptions{Environment: []string{"DOCKS_IT_ENV_TEST=from-opts", "DOCKS_IT_ONLY_OPTS=1"}},
	)
	require.NoError(t, err)

	args := strings.Join(*got, " ")
	for _, want := range []string{
		"-e DOCKS_IT_ENV_TEST=from-opts",
		"-e DOCKS_IT_ONLY_OPTS=1",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("helper command %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "DOCKS_IT_ENV_TEST=from-process") {
		t.Errorf("HelperOptions environment should override the process: %q", args)
	}
}

func TestPreflightRunsComposeConfigCheck(t *testing.T) {
	got := captureHelper(t, nil, nil)

	err := Preflight(
		context.Background(),
		"docksmith:latest",
		"/srv/stacks/app/docker-compose.yaml",
		"docksmith",
		HelperOptions{Mounts: map[string]string{"/srv/stacks/": "/stacks/"}},
	)
	require.NoError(t, err)

	args := strings.Join(*got, " ")
	if !strings.HasPrefix(args, "run --rm ") {
		t.Errorf("preflight must run attached and disposable: %q", args)
	}
	if strings.Contains(args, "run -d") || strings.Contains(args, "sleep") {
		t.Errorf("preflight must not detach or delay: %q", args)
	}
	if !strings.Contains(args, "docker compose --project-directory '/srv/stacks/app' -f '/srv/stacks/app/docker-compose.yaml' config --quiet") {
		t.Errorf("preflight command %q does not validate the project", args)
	}
	if !strings.Contains(args, "-v /srv/stacks:/stacks:ro") {
		t.Errorf("preflight command %q missing the helper mounts", args)
	}
}

func TestPreflightPropagatesConfigurationFailure(t *testing.T) {
	captureHelper(t, []byte(`service "docksmith" refers to undefined volume`), errors.New("exit status 1"))

	err := Preflight(context.Background(), "docksmith:latest", "/srv/stacks/app/compose.yaml", "docksmith", HelperOptions{})
	require.Error(t, err)
	if !strings.Contains(err.Error(), "self-preflight") {
		t.Errorf("error %q should identify the preflight", err)
	}
	if !strings.Contains(err.Error(), "undefined volume") {
		t.Errorf("error %q should include compose output", err)
	}
}

func TestPreflightRequiresComposeFile(t *testing.T) {
	captureHelper(t, nil, nil)

	if err := Preflight(context.Background(), "docksmith:latest", "", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for empty compose file path")
	}
	if err := Preflight(context.Background(), "", "/srv/app/docker-compose.yaml", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for empty image")
	}
	if err := Preflight(context.Background(), "docksmith:latest", "docker-compose.yaml", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for relative compose file path")
	}
}

func TestRecreateSelfPropagatesHelperFailure(t *testing.T) {
	captureHelper(t, []byte("unable to find image"), errors.New("exit status 125"))

	err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/app/docker-compose.yaml", "docksmith", HelperOptions{})
	require.Error(t, err)
	if !strings.Contains(err.Error(), "unable to find image") {
		t.Errorf("error %q should include helper output", err)
	}
}

func TestRecreateSelfRequiresImageAndComposeFile(t *testing.T) {
	captureHelper(t, nil, nil)

	if err := RecreateSelf(context.Background(), "", "/srv/app/docker-compose.yaml", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for empty image")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for empty compose file path")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/app/docker-compose.yaml", "", HelperOptions{}); err == nil {
		t.Error("expected error for empty service name")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "docker-compose.yaml", "docksmith", HelperOptions{}); err == nil {
		t.Error("expected error for relative compose file path")
	}
}

func TestHelperMountsMirrorRemappedBinding(t *testing.T) {
	got := helperMounts(map[string]string{"/srv/stacks/": "/stacks/"})

	assert.ElementsMatch(t, []helperMount{
		{Source: "/srv/stacks", Destination: "/srv/stacks"},
		{Source: "/srv/stacks", Destination: "/stacks"},
	}, got)
}

func TestHelperMountsDoNotMirrorIdentityBinding(t *testing.T) {
	got := helperMounts(map[string]string{"/srv/stacks/": "/srv/stacks/"})

	assert.Equal(t, []helperMount{{Source: "/srv/stacks", Destination: "/srv/stacks"}}, got)
}

// The compose directory is not mounted twice when a binding already provides
// that destination.
func TestHelperVolumeArgsDeduplicateDestinations(t *testing.T) {
	got := helperVolumeArgs(map[string]string{"/srv/stacks/": "/stacks/"}, "/stacks")

	assert.Equal(t, []string{
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", "/srv/stacks:/srv/stacks:ro",
		"-v", "/srv/stacks:/stacks:ro",
	}, got)
}

func TestHelperMountsKeepNestedBindings(t *testing.T) {
	got := helperMounts(map[string]string{
		"/srv/":         "/srv/",
		"/srv/secrets/": "/secrets/",
	})

	assert.ElementsMatch(t, []helperMount{
		{Source: "/srv", Destination: "/srv"},
		{Source: "/srv/secrets", Destination: "/secrets"},
		{Source: "/srv/secrets", Destination: "/srv/secrets"},
	}, got)
}

func TestHelperScriptQuotesShellMetacharacters(t *testing.T) {
	script := helperScript("/srv/my stacks/a'b/docker-compose.yaml", "dock smith", composeUpForceRecreate, true)

	want := `-f '/srv/my stacks/a'\''b/docker-compose.yaml'`
	if !strings.Contains(script, want) {
		t.Errorf("script %q should quote the compose path as %q", script, want)
	}
	if !strings.Contains(script, `--project-directory '/srv/my stacks/a'\''b'`) {
		t.Errorf("script %q should quote the project directory", script)
	}
	if !strings.Contains(script, `force-recreate 'dock smith'`) {
		t.Errorf("script %q should quote the service name", script)
	}
}
