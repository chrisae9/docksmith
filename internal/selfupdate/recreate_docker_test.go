package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dockerHelperImage returns an image that can run `docker compose`, or "" when
// real Docker/Compose coverage cannot run in this environment. Tests that get
// "" skip rather than fail.
func dockerHelperImage(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		return ""
	}
	if err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		return ""
	}

	image := os.Getenv("DOCKS_TEST_HELPER_IMAGE")
	if image == "" {
		image = "docker:27-cli"
	}
	if err := exec.Command("docker", "pull", "-q", image).Run(); err != nil {
		return ""
	}
	// The compose plugin must be present in the image for the helper to work.
	if err := exec.Command("docker", "run", "--rm", "--entrypoint", "sh", image,
		"-c", "docker compose version >/dev/null").Run(); err != nil {
		return ""
	}
	return image
}

func writeComposeFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A project whose inputs live outside the compose file's directory — external
// env_file entries, includes with their own env_file, extends targets, and an
// interpolated include path — must pass the real Compose preflight with the
// mounts the helper would use.
func TestPreflightAcceptsExternalComposeInputs(t *testing.T) {
	image := dockerHelperImage(t)
	if image == "" {
		t.Skip("docker with the compose plugin is not available")
	}
	t.Setenv("DOCKS_TEST_INCLUDE_DIR", "")

	dir := writeComposeFixture(t, map[string]string{
		"docksmith/compose.yaml": `include:
  - path: ../other/compose.yaml
    env_file: ../secrets/include.env
  - "${DOCKS_TEST_INCLUDE_DIR:-../other}/compose.yaml"
services:
  docksmith:
    image: alpine:3.20
    env_file: ../shared.env
    extends:
      file: ../base/common.yaml
      service: base
`,
		"other/compose.yaml":  "services:\n  other:\n    image: alpine:3.20\n",
		"base/common.yaml":    "services:\n  base:\n    image: alpine:3.20\n    env_file: ../secrets/shared.env\n",
		"secrets/include.env": "INCLUDE_SECRET=1\n",
		"secrets/shared.env":  "SHARED_SECRET=1\n",
		"shared.env":          "ROOT_SECRET=1\n",
	})

	opts := HelperOptions{Mounts: map[string]string{dir + "/": dir + "/"}}
	if err := Preflight(context.Background(), image, filepath.Join(dir, "docksmith", "compose.yaml"), "docksmith", opts); err != nil {
		t.Fatalf("preflight rejected a valid project: %v", err)
	}
}

// A genuinely unavailable input must fail the preflight, which is what stops the
// operation before it is recorded as pending_restart.
func TestPreflightRejectsMissingExternalInput(t *testing.T) {
	image := dockerHelperImage(t)
	if image == "" {
		t.Skip("docker with the compose plugin is not available")
	}

	dir := writeComposeFixture(t, map[string]string{
		"compose.yaml": `services:
  docksmith:
    image: alpine:3.20
    env_file: ../missing/secrets.env
`,
	})

	opts := HelperOptions{Mounts: map[string]string{dir + "/": dir + "/"}}
	err := Preflight(context.Background(), image, filepath.Join(dir, "compose.yaml"), "docksmith", opts)
	if err == nil {
		t.Fatal("preflight accepted a project with a missing env_file")
	}
}

// The detached helper must survive the source container stopping and finish the
// recreate from outside it.
func TestRecreateSelfRunsDetachedHelper(t *testing.T) {
	image := dockerHelperImage(t)
	if image == "" {
		t.Skip("docker with the compose plugin is not available")
	}
	if err := exec.Command("docker", "pull", "-q", "alpine:3.20").Run(); err != nil {
		t.Skip("alpine:3.20 is not available")
	}

	const containerName = "docksmith-selfupdate-helper-test"
	exec.Command("docker", "rm", "-f", containerName).Run()
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", containerName).Run() })

	dir := writeComposeFixture(t, map[string]string{
		"compose.yaml": `services:
  docksmith:
    image: alpine:3.20
    container_name: ` + containerName + `
    command: ["sleep", "30"]
`,
	})

	opts := HelperOptions{Mounts: map[string]string{dir + "/": dir + "/"}}
	if err := RecreateSelf(context.Background(), image, filepath.Join(dir, "compose.yaml"), "docksmith", opts); err != nil {
		t.Fatalf("RecreateSelf failed to start the helper: %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("detached helper did not recreate the service")
}
