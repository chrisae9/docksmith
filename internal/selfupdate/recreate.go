package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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

// readComposeFile and composeFileExists are variables so tests can resolve
// virtual host paths.
var (
	readComposeFile   = os.ReadFile
	composeFileExists = func(name string) bool {
		_, err := os.Stat(name)
		return err == nil
	}
)

// RecreateSelf recreates the docksmith container from a short-lived helper
// container and returns as soon as that helper is running.
//
// Docksmith cannot recreate its own container with docker compose directly: the
// compose process runs inside the container being stopped, so it is killed
// together with docksmith's PID 1 before it can create the replacement. The
// helper runs outside that lifecycle and mounts the Docker socket, the host
// compose directory, and every directory holding a file the project references
// from outside it (include, env_file, extends), so docker compose resolves
// relative paths exactly as docksmith would.
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
	}
	// Compose resolves env_file, include, and extends paths against the project
	// directory, so every referenced file outside it must be reachable in the
	// helper at the same host path.
	for _, dir := range append([]string{hostComposeDir}, composeInputDirs(hostComposeFilePath)...) {
		args = append(args, "-v", dir+":"+dir+":ro")
	}
	args = append(args,
		"-w", hostComposeDir,
		"--entrypoint", "sh",
	)
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

// composeInputDirs returns the host directories that hold files the Compose
// project reads from outside the compose file's own directory: recursively
// included compose files, env_file entries, and extends targets. References
// that do not exist are skipped, because Docker would otherwise create them on
// the host as empty root-owned directories.
func composeInputDirs(composeFilePath string) []string {
	composeDir := path.Dir(composeFilePath)
	dirs := make([]string, 0, 2)
	seenDirs := make(map[string]bool)
	seenFiles := make(map[string]bool)

	// addReference mounts the directory holding ref, resolving relative
	// references against both the referencing file and the project directory:
	// Compose resolves most paths against the project directory but include
	// paths against the file that includes them.
	addReference := func(ref, baseDir string) {
		for _, candidate := range resolveComposeReference(ref, baseDir, composeDir) {
			if !composeFileExists(candidate) {
				continue
			}
			dir := path.Dir(candidate)
			if dir != composeDir && !seenDirs[dir] {
				seenDirs[dir] = true
				dirs = append(dirs, dir)
			}
			return
		}
	}

	var walk func(file string)
	walk = func(file string) {
		if seenFiles[file] {
			return
		}
		seenFiles[file] = true

		data, err := readComposeFile(file)
		if err != nil {
			return
		}
		var doc composeDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return
		}

		baseDir := path.Dir(file)
		for _, ref := range composeReferencePaths(&doc.Include) {
			addReference(ref, baseDir)
			for _, candidate := range resolveComposeReference(ref, baseDir, composeDir) {
				if composeFileExists(candidate) {
					walk(candidate)
					break
				}
			}
		}
		for _, service := range doc.Services {
			for _, ref := range composeReferencePaths(&service.EnvFile) {
				addReference(ref, baseDir)
			}
			if ref := composeExtendsFile(&service.Extends); ref != "" {
				addReference(ref, baseDir)
			}
		}
	}
	walk(composeFilePath)

	return dirs
}

// resolveComposeReference returns the candidate paths for a relative reference,
// trying the referencing file's directory first and the project directory
// second.
func resolveComposeReference(ref, baseDir, projectDir string) []string {
	if ref == "" {
		return nil
	}
	if path.IsAbs(ref) {
		return []string{path.Clean(ref)}
	}
	first := path.Clean(path.Join(baseDir, ref))
	candidates := []string{first}
	if second := path.Clean(path.Join(projectDir, ref)); second != first {
		candidates = append(candidates, second)
	}
	return candidates
}

// composeDocument is the subset of a compose file needed to find external
// inputs.
type composeDocument struct {
	Include  yaml.Node                      `yaml:"include"`
	Services map[string]composeServiceInput `yaml:"services"`
}

type composeServiceInput struct {
	EnvFile yaml.Node `yaml:"env_file"`
	Extends yaml.Node `yaml:"extends"`
}

// composeReferencePaths reads path references from a scalar, a sequence, or a
// mapping with a "path" key (env_file and include both allow all three).
func composeReferencePaths(node *yaml.Node) []string {
	switch node.Kind {
	case yaml.SequenceNode:
		refs := make([]string, 0, len(node.Content))
		for i := range node.Content {
			refs = append(refs, composeReferenceFromNode(node.Content[i])...)
		}
		return refs
	case yaml.ScalarNode, yaml.MappingNode:
		return composeReferenceFromNode(node)
	}
	return nil
}

func composeReferenceFromNode(node *yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return nil
		}
		return []string{node.Value}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "path" {
				return composeReferenceFromNode(node.Content[i+1])
			}
		}
	}
	return nil
}

// composeExtendsFile returns the file a service extends, or "" when extends
// names a service in the same project.
func composeExtendsFile(node *yaml.Node) string {
	if node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "file" {
			return node.Content[i+1].Value
		}
	}
	return ""
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
