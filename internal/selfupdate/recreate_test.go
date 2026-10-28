package selfupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
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
	)
	if err != nil {
		t.Fatalf("RecreateSelf returned error: %v", err)
	}

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
	)
	if err != nil {
		t.Fatalf("RestartSelf returned error: %v", err)
	}

	args := strings.Join(*got, " ")
	if !strings.Contains(args, "docker compose --project-directory '/srv/stacks/app' -f '/srv/stacks/app/docker-compose.yaml' restart 'docksmith'") {
		t.Errorf("helper command %q does not run compose restart", args)
	}
	if strings.Contains(args, "--force-recreate") {
		t.Errorf("restart helper should not force-recreate: %q", args)
	}
}

func TestRecreateSelfPropagatesHelperFailure(t *testing.T) {
	captureHelper(t, []byte("unable to find image"), errors.New("exit status 125"))

	err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/app/docker-compose.yaml", "docksmith")
	if err == nil {
		t.Fatal("expected error when helper container cannot start")
	}
	if !strings.Contains(err.Error(), "unable to find image") {
		t.Errorf("error %q should include helper output", err)
	}
}

func TestRecreateSelfRequiresImageAndComposeFile(t *testing.T) {
	captureHelper(t, nil, nil)

	if err := RecreateSelf(context.Background(), "", "/srv/app/docker-compose.yaml", "docksmith"); err == nil {
		t.Error("expected error for empty image")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "", "docksmith"); err == nil {
		t.Error("expected error for empty compose file path")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/app/docker-compose.yaml", ""); err == nil {
		t.Error("expected error for empty service name")
	}
	if err := RecreateSelf(context.Background(), "docksmith:latest", "docker-compose.yaml", "docksmith"); err == nil {
		t.Error("expected error for relative compose file path")
	}
}

func TestHelperScriptQuotesShellMetacharacters(t *testing.T) {
	script := helperScript("/srv/my stacks/a'b/docker-compose.yaml", "dock smith", composeUpForceRecreate)

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
