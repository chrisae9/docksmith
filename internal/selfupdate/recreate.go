package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

const (
	// helperDelay lets docksmith flush its SSE progress event and finish any
	// in-flight HTTP response before the helper stops its container.
	helperDelay = 2 * time.Second

	// dockerSocketPath is where the Docker socket is mounted, both in docksmith
	// itself and in the helper container.
	dockerSocketPath = "/var/run/docker.sock"

	composeUpForceRecreate = "up -d --force-recreate"
	composeRestart         = "restart"
)

// runHelperCommand runs the docker CLI that starts the helper container.
// It is a variable so tests can inspect the generated command.
var runHelperCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// RecreateSelf recreates the docksmith container from a short-lived helper
// container and returns as soon as that helper is running.
//
// Docksmith cannot recreate its own container with docker compose directly: the
// compose process runs inside the container being stopped, so it is killed
// together with docksmith's PID 1 before it can create the replacement. The
// helper runs outside that lifecycle and mounts the Docker socket and the host
// compose directory, so docker compose resolves relative paths exactly as
// docksmith would.
func RecreateSelf(ctx context.Context, imageRef, hostComposeFilePath, serviceName string) error {
	return runComposeHelper(ctx, imageRef, hostComposeFilePath, serviceName, composeUpForceRecreate, "recreate")
}

// RestartSelf restarts the docksmith container from a short-lived helper
// container, for restarts that do not need a recreate.
func RestartSelf(ctx context.Context, imageRef, hostComposeFilePath, serviceName string) error {
	return runComposeHelper(ctx, imageRef, hostComposeFilePath, serviceName, composeRestart, "restart")
}

// runComposeHelper starts a detached, auto-removed container from the docksmith
// image that runs the given compose subcommand after a short delay.
func runComposeHelper(ctx context.Context, imageRef, hostComposeFilePath, serviceName, composeCommand, verb string) error {
	if imageRef == "" {
		return fmt.Errorf("self-%s: no image reference for helper container", verb)
	}
	if hostComposeFilePath == "" {
		return fmt.Errorf("self-%s: no compose file path for helper container", verb)
	}
	// The helper mounts the directory on the host, so a relative path would be
	// resolved against the wrong filesystem.
	if !path.IsAbs(hostComposeFilePath) {
		return fmt.Errorf("self-%s: compose file path %q is not absolute", verb, hostComposeFilePath)
	}
	if serviceName == "" {
		return fmt.Errorf("self-%s: no compose service name for helper container", verb)
	}

	// Compose paths are POSIX paths on the Docker host; path.Dir keeps them
	// slash-separated regardless of the platform running the tests.
	hostComposeDir := path.Dir(hostComposeFilePath)

	// --rm removes the helper automatically when its process exits, including
	// when compose fails. The label makes a stuck helper easy to identify.
	args := []string{
		"run", "-d", "--rm",
		"--label", "docksmith.self-" + verb + "=true",
		"-v", dockerSocketPath + ":" + dockerSocketPath,
		"-v", hostComposeDir + ":" + hostComposeDir + ":ro",
		"-w", hostComposeDir,
		"--entrypoint", "sh",
	}
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		args = append(args, "-e", "DOCKER_HOST="+dockerHost)
	}
	args = append(args, imageRef, "-c", helperScript(hostComposeFilePath, serviceName, composeCommand))

	output, err := runHelperCommand(ctx, args...)
	if err != nil {
		return fmt.Errorf("self-%s helper failed to start: %w (output: %s)",
			verb, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// helperScript builds the shell command the helper runs. The sleep lives inside
// the helper so it survives docksmith shutting down.
func helperScript(hostComposeFilePath, serviceName, composeCommand string) string {
	hostComposeDir := path.Dir(hostComposeFilePath)
	return fmt.Sprintf("sleep %d; docker compose --project-directory %s -f %s %s %s",
		int(helperDelay.Seconds()),
		shellQuote(hostComposeDir),
		shellQuote(hostComposeFilePath),
		composeCommand,
		shellQuote(serviceName),
	)
}

// shellQuote wraps s in single quotes so it is passed through sh unchanged.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
