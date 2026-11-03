package selfupdate

import (
	"context"
	"errors"
	"os"
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

// withComposeFS swaps the compose-file reader and existence probe for the
// duration of the test, so virtual host paths can be used on any platform.
func withComposeFS(t *testing.T, files map[string]string) {
	t.Helper()
	origRead, origExists := readComposeFile, composeFileExists
	t.Cleanup(func() {
		readComposeFile, composeFileExists = origRead, origExists
	})
	readComposeFile = func(name string) ([]byte, error) {
		if content, ok := files[name]; ok {
			return []byte(content), nil
		}
		return nil, os.ErrNotExist
	}
	composeFileExists = func(name string) bool {
		_, ok := files[name]
		return ok
	}
}

// A compose file that reads ../shared.env needs the parent directory mounted
// in the helper, not just the compose directory.
func TestRecreateSelfMountsExternalEnvFile(t *testing.T) {
	got := captureHelper(t, nil, nil)
	withComposeFS(t, map[string]string{
		"/srv/stacks/docksmith/compose.yaml": "services:\n  docksmith:\n    env_file: ../shared.env\n",
		"/srv/stacks/shared.env":             "FOO=bar\n",
	})

	if err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/stacks/docksmith/compose.yaml", "docksmith"); err != nil {
		t.Fatalf("RecreateSelf returned error: %v", err)
	}

	args := strings.Join(*got, " ")
	if !strings.Contains(args, "-v /srv/stacks:/srv/stacks:ro") {
		t.Errorf("helper command %q must mount the env_file's directory", args)
	}
	if !strings.Contains(args, "-v /srv/stacks/docksmith:/srv/stacks/docksmith:ro") {
		t.Errorf("helper command %q must still mount the compose directory", args)
	}
}

// Included compose files and inputs they reference outside the compose
// directory must be reachable too.
func TestRecreateSelfMountsIncludedComposeInputs(t *testing.T) {
	got := captureHelper(t, nil, nil)
	withComposeFS(t, map[string]string{
		"/srv/stacks/docksmith/compose.yaml": "include:\n  - ../other/compose.yaml\nservices:\n  docksmith: {}\n",
		"/srv/stacks/other/compose.yaml":     "services:\n  db:\n    env_file: ../secrets/db.env\n",
		"/srv/stacks/secrets/db.env":         "DB_PASSWORD=secret\n",
	})

	if err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/stacks/docksmith/compose.yaml", "docksmith"); err != nil {
		t.Fatalf("RecreateSelf returned error: %v", err)
	}

	args := strings.Join(*got, " ")
	for _, want := range []string{
		"-v /srv/stacks/other:/srv/stacks/other:ro",
		"-v /srv/stacks/secrets:/srv/stacks/secrets:ro",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("helper command %q missing %q", args, want)
		}
	}
}

// An extends target outside the compose directory must be mounted as well.
func TestRecreateSelfMountsExternalExtendsFile(t *testing.T) {
	got := captureHelper(t, nil, nil)
	withComposeFS(t, map[string]string{
		"/srv/stacks/docksmith/compose.yaml": "services:\n  docksmith:\n    extends:\n      file: ../base/common.yaml\n      service: docksmith\n",
		"/srv/stacks/base/common.yaml":       "services:\n  docksmith: {}\n",
	})

	if err := RecreateSelf(context.Background(), "docksmith:latest", "/srv/stacks/docksmith/compose.yaml", "docksmith"); err != nil {
		t.Fatalf("RecreateSelf returned error: %v", err)
	}

	args := strings.Join(*got, " ")
	if !strings.Contains(args, "-v /srv/stacks/base:/srv/stacks/base:ro") {
		t.Errorf("helper command %q must mount the extends directory", args)
	}
}

// References that do not exist are skipped: mounting a missing host path would
// make Docker create it as an empty root-owned directory.
func TestComposeInputDirsSkipsMissingReferences(t *testing.T) {
	withComposeFS(t, map[string]string{
		"/srv/stacks/docksmith/compose.yaml": "services:\n  docksmith:\n    env_file: ../../does-not-exist.env\n",
	})

	if got := composeInputDirs("/srv/stacks/docksmith/compose.yaml"); len(got) != 0 {
		t.Errorf("composeInputDirs() = %v, want none for a missing reference", got)
	}
}
