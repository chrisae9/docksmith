package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
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

	// preflightTimeout bounds the synchronous Compose configuration check.
	preflightTimeout = 60 * time.Second

	composeUpForceRecreate = "up -d --force-recreate"
	composeRestart         = "restart"
	composeConfigCheck     = "config --quiet"
)

// HelperOptions describes how the helper container sees the host filesystem and
// the Compose interpolation environment.
type HelperOptions struct {
	// Mounts is Docksmith's host-source to container-destination bind mapping
	// (docker.PathTranslator.GetMappings), captured before the source container
	// stops. Every source is mounted read-only at its destination; when the two
	// differ the source is also mirrored at its own host path, so paths Compose
	// resolves from the project directory stay valid inside the helper.
	Mounts map[string]string

	// Environment holds additional interpolation variables, as "KEY=VALUE".
	// Process-defined variables are forwarded automatically.
	Environment []string
}

// helperMount is one read-only bind the helper needs.
type helperMount struct {
	Source      string
	Destination string
}

// runHelperCommand runs the docker CLI that starts or preflights the helper
// container. It is a variable so tests can inspect the generated command.
var runHelperCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// RecreateSelf recreates the docksmith container from a short-lived helper
// container and returns as soon as that helper is running.
//
// Docksmith cannot recreate its own container with docker compose directly: the
// compose process runs inside the container being stopped, so it is killed
// together with docksmith's PID 1 before it can create the replacement. The
// helper runs outside that lifecycle, mounts the Docker socket and Docksmith's
// existing bind mounts, and lets the real Compose CLI resolve the project.
func RecreateSelf(ctx context.Context, imageRef, hostComposeFilePath, serviceName string, opts HelperOptions) error {
	return runComposeHelper(ctx, imageRef, hostComposeFilePath, serviceName, opts, composeUpForceRecreate, "recreate", true)
}

// RestartSelf restarts the docksmith container from a short-lived helper
// container, for restarts that do not need a recreate.
func RestartSelf(ctx context.Context, imageRef, hostComposeFilePath, serviceName string, opts HelperOptions) error {
	return runComposeHelper(ctx, imageRef, hostComposeFilePath, serviceName, opts, composeRestart, "restart", true)
}

// Preflight validates that the helper can load and resolve the Compose project
// with exactly the mounts and environment it will use for the real restart.
//
// It runs `docker compose config --quiet` in a disposable container, so missing
// inputs or an invalid project fail while docksmith is still running — before
// the operation is recorded as pending_restart.
func Preflight(ctx context.Context, imageRef, hostComposeFilePath, serviceName string, opts HelperOptions) error {
	if hostComposeFilePath == "" {
		return fmt.Errorf("self-preflight: no compose file path for helper container")
	}
	ctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()

	return runComposeHelper(ctx, imageRef, hostComposeFilePath, serviceName, opts, composeConfigCheck, "preflight", false)
}

// runComposeHelper starts a helper container that either runs the Compose
// command detached (after a short delay) or synchronously for a preflight.
func runComposeHelper(ctx context.Context, imageRef, hostComposeFilePath, serviceName string, opts HelperOptions, composeCommand, verb string, detached bool) error {
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
	if detached && serviceName == "" {
		return fmt.Errorf("self-%s: no compose service name for helper container", verb)
	}

	// Compose paths are POSIX paths on the Docker host; path.Dir keeps them
	// slash-separated regardless of the platform running the tests.
	hostComposeDir := path.Dir(hostComposeFilePath)

	args := []string{"run"}
	if detached {
		// --rm removes the helper automatically when its process exits, including
		// when compose fails. The label makes a stuck helper easy to identify.
		args = append(args, "-d", "--rm", "--label", "docksmith.self-"+verb+"=true")
	} else {
		args = append(args, "--rm")
	}
	args = append(args, helperVolumeArgs(opts.Mounts, hostComposeDir)...)
	args = append(args, helperEnvironment(opts)...)
	args = append(args,
		"-w", hostComposeDir,
		"--entrypoint", "sh",
		imageRef, "-c", helperScript(hostComposeFilePath, serviceName, composeCommand, detached),
	)

	output, err := runHelperCommand(ctx, args...)
	if err != nil {
		return fmt.Errorf("self-%s helper failed: %w (output: %s)",
			verb, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// helperVolumeArgs returns the read-only binds the helper needs: the Docker
// socket, Docksmith's bind destinations, host-path mirrors for remapped
// bindings, and the compose file's own directory.
func helperVolumeArgs(mappings map[string]string, hostComposeDir string) []string {
	args := []string{"-v", dockerSocketPath + ":" + dockerSocketPath}
	seen := map[string]bool{dockerSocketPath: true}
	add := func(source, destination string) {
		source = path.Clean(source)
		destination = path.Clean(destination)
		if source == "" || source == "." || destination == "" || destination == "." {
			return
		}
		if seen[destination] {
			return
		}
		seen[destination] = true
		args = append(args, "-v", source+":"+destination+":ro")
	}

	for _, mount := range helperMounts(mappings) {
		add(mount.Source, mount.Destination)
	}
	add(hostComposeDir, hostComposeDir)
	return args
}

// helperMounts converts Docksmith's host-to-container bind mapping into
// read-only helper mounts: the destination view Docksmith uses, plus a mirror at
// the source's own host path when the two differ. Nested bindings are kept;
// duplicate destinations are dropped deterministically.
func helperMounts(mappings map[string]string) []helperMount {
	type pair struct{ source, destination string }

	pairs := make([]pair, 0, len(mappings)*2)
	for source, destination := range mappings {
		source = path.Clean(source)
		destination = path.Clean(destination)
		pairs = append(pairs, pair{source: source, destination: destination})
		if source != destination {
			pairs = append(pairs, pair{source: source, destination: source})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].destination != pairs[j].destination {
			return pairs[i].destination < pairs[j].destination
		}
		return pairs[i].source < pairs[j].source
	})

	seen := make(map[string]bool, len(pairs))
	mounts := make([]helperMount, 0, len(pairs))
	for _, p := range pairs {
		if p.source == "" || p.source == "." || p.destination == "" || p.destination == "." || seen[p.destination] {
			continue
		}
		seen[p.destination] = true
		mounts = append(mounts, helperMount{Source: p.source, Destination: p.destination})
	}
	return mounts
}

// helperManagedEnv lists variables Docker or the shell set for the helper
// itself; forwarding Docksmith's copies of these would be misleading.
var helperManagedEnv = map[string]bool{
	"HOSTNAME": true,
	"HOME":     true,
	"PWD":      true,
	"SHLVL":    true,
	"TERM":     true,
}

// helperEnvironment forwards the process environment so Compose interpolates
// variables exactly as Docksmith would, with HelperOptions.Environment applied
// on top.
func helperEnvironment(opts HelperOptions) []string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || helperManagedEnv[key] {
			continue
		}
		env[key] = value
	}
	for _, entry := range opts.Environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "-e", key+"="+env[key])
	}
	return args
}

// helperScript builds the shell command the helper runs. The sleep lives inside
// the helper so it survives docksmith shutting down.
func helperScript(hostComposeFilePath, serviceName, composeCommand string, detached bool) string {
	hostComposeDir := path.Dir(hostComposeFilePath)
	compose := fmt.Sprintf("docker compose --project-directory %s -f %s %s",
		shellQuote(hostComposeDir),
		shellQuote(hostComposeFilePath),
		composeCommand,
	)
	if !detached {
		return compose
	}
	return fmt.Sprintf("sleep %d; %s %s", int(helperDelay.Seconds()), compose, shellQuote(serviceName))
}

// shellQuote wraps s in single quotes so it is passed through sh unchanged.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
