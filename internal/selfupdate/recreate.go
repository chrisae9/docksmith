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
// included compose files, env_file entries, and extends targets. References are
// interpolated with the project environment before they are resolved, and
// references that do not exist are skipped — Docker would otherwise create them
// on the host as empty root-owned directories.
func composeInputDirs(composeFilePath string) []string {
	rootDir := path.Dir(composeFilePath)
	rootEnv := composeInterpolationEnv(rootDir)

	dirs := make([]string, 0, 2)
	seenDirs := make(map[string]bool)
	seenFiles := make(map[string]bool)

	// addReference mounts the directory holding ref. Relative references are
	// resolved against both the referencing file and the project directory:
	// Compose resolves most paths against the project directory but include
	// paths against the file that includes them.
	addReference := func(ref, baseDir, projectDir string, env map[string]string) {
		ref = interpolateComposeValue(ref, env)
		if ref == "" {
			return
		}
		for _, candidate := range resolveComposeReference(ref, baseDir, projectDir) {
			if !composeFileExists(candidate) {
				continue
			}
			dir := path.Dir(candidate)
			if dir != rootDir && !seenDirs[dir] {
				seenDirs[dir] = true
				dirs = append(dirs, dir)
			}
			return
		}
	}

	// walk parses a referenced file in the context of the project directory and
	// interpolation environment that apply to it.
	var walk func(file, projectDir string, env map[string]string)
	walk = func(file, projectDir string, env map[string]string) {
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
		// walkReference mounts the path a reference resolves to and parses that
		// file too, so references it makes in turn — extends chains and nested
		// includes — are discovered.
		walkReference := func(ref, projectDir string, env map[string]string) {
			addReference(ref, baseDir, projectDir, env)
			for _, candidate := range resolveComposeReference(interpolateComposeValue(ref, env), baseDir, projectDir) {
				if composeFileExists(candidate) {
					walk(candidate, projectDir, env)
					break
				}
			}
		}

		for _, include := range composeIncludes(&doc.Include) {
			includeEnv := env
			includeProjectDir := projectDir
			// A long-form include may declare its own interpolation env_file and
			// project directory for the files it loads.
			if len(include.envFiles) > 0 {
				includeEnv = copyComposeEnv(env)
				for _, ref := range include.envFiles {
					interpolated := interpolateComposeValue(ref, env)
					addReference(interpolated, baseDir, projectDir, env)
					for _, candidate := range resolveComposeReference(interpolated, baseDir, projectDir) {
						if composeFileExists(candidate) {
							loadComposeEnvFile(candidate, includeEnv)
							break
						}
					}
				}
			}
			if include.projectDirectory != "" {
				// project_directory resolves against the declaring file and is not
				// existence-checked: Compose fails the load when it is missing.
				interpolated := interpolateComposeValue(include.projectDirectory, env)
				if path.IsAbs(interpolated) {
					includeProjectDir = path.Clean(interpolated)
				} else {
					includeProjectDir = path.Clean(path.Join(baseDir, interpolated))
				}
			}
			for _, ref := range include.paths {
				walkReference(ref, includeProjectDir, includeEnv)
			}
		}
		for _, service := range doc.Services {
			for _, ref := range composeReferencePaths(&service.EnvFile) {
				addReference(ref, baseDir, projectDir, env)
			}
			if ref := composeExtendsFile(&service.Extends); ref != "" {
				walkReference(ref, projectDir, env)
			}
		}
	}
	walk(composeFilePath, rootDir, rootEnv)

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
				// Long-form include entries allow path to be a list.
				return composeReferencePaths(node.Content[i+1])
			}
		}
	}
	return nil
}

// composeInclude is one include entry: compose files to load plus optional
// interpolation env files and a project directory for the included files.
type composeInclude struct {
	paths            []string
	envFiles         []string
	projectDirectory string
}

// composeIncludes parses the include key, accepting the short form (paths) and
// the long form (entries with path, env_file, and project_directory keys),
// where path may itself be a list.
func composeIncludes(node *yaml.Node) []composeInclude {
	var includes []composeInclude
	var visit func(*yaml.Node)
	visit = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.SequenceNode:
			for i := range n.Content {
				visit(n.Content[i])
			}
		case yaml.ScalarNode:
			if n.Value != "" {
				includes = append(includes, composeInclude{paths: []string{n.Value}})
			}
		case yaml.MappingNode:
			var include composeInclude
			for i := 0; i+1 < len(n.Content); i += 2 {
				switch n.Content[i].Value {
				case "path":
					include.paths = composeReferencePaths(n.Content[i+1])
				case "env_file":
					include.envFiles = composeReferencePaths(n.Content[i+1])
				case "project_directory":
					include.projectDirectory = n.Content[i+1].Value
				}
			}
			includes = append(includes, include)
		}
	}
	if node.Kind != 0 {
		visit(node)
	}
	return includes
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

// composeInterpolationEnv returns the variables Compose interpolates with for a
// project directory: the project's .env file, overlaid with the process
// environment.
func composeInterpolationEnv(projectDir string) map[string]string {
	env := make(map[string]string)
	loadComposeEnvFile(path.Join(projectDir, ".env"), env)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	return env
}

// copyComposeEnv copies an interpolation environment so an include can extend
// it without leaking variables into siblings.
func copyComposeEnv(env map[string]string) map[string]string {
	copied := make(map[string]string, len(env))
	for key, value := range env {
		copied[key] = value
	}
	return copied
}

// loadComposeEnvFile parses KEY=VALUE lines from an env file into env.
func loadComposeEnvFile(file string, env map[string]string) {
	data, err := readComposeFile(file)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		env[key] = value
	}
}

// interpolateComposeValue expands Compose-style variable references: $VAR,
// ${VAR}, ${VAR:-default}, ${VAR-default}, ${VAR:+replacement},
// ${VAR+replacement}, ${VAR:?err}, and ${VAR?err}. An unset variable expands to
// the empty string, matching Compose, and "$$" is an escaped literal "$".
func interpolateComposeValue(value string, env map[string]string) string {
	if !strings.Contains(value, "$") {
		return value
	}

	var expanded strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			expanded.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			expanded.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < len(value) && value[i+1] == '{' {
			end := matchingComposeBrace(value, i+1)
			if end < 0 {
				expanded.WriteByte(value[i])
				i++
				continue
			}
			expanded.WriteString(expandComposeReference(value[i+2:end], env))
			i = end + 1
			continue
		}
		j := i + 1
		for j < len(value) && isComposeEnvNameByte(value[j], j == i+1) {
			j++
		}
		if j == i+1 {
			expanded.WriteByte('$')
			i++
			continue
		}
		expanded.WriteString(env[value[i+1:j]])
		i = j
	}
	return expanded.String()
}

// matchingComposeBrace returns the index of the brace closing the one at open,
// accounting for nested ${...} references in defaults.
func matchingComposeBrace(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// expandComposeReference expands the contents of a ${...} reference.
func expandComposeReference(expr string, env map[string]string) string {
	name, op, arg := expr, "", ""
	// Longest operators first so ":-" is not read as "-".
	for _, candidate := range []string{":-", ":+", ":?", "-", "+", "?"} {
		if idx := strings.Index(expr, candidate); idx > 0 {
			name, op, arg = expr[:idx], candidate, expr[idx+len(candidate):]
			break
		}
	}

	value, set := env[name]
	nonEmpty := set && value != ""
	switch op {
	case ":-":
		if nonEmpty {
			return value
		}
		return interpolateComposeValue(arg, env)
	case "-":
		if set {
			return value
		}
		return interpolateComposeValue(arg, env)
	case ":+":
		if nonEmpty {
			return interpolateComposeValue(arg, env)
		}
		return ""
	case "+":
		if set {
			return interpolateComposeValue(arg, env)
		}
		return ""
	case ":?", "?":
		// Compose aborts here; return the value rather than failing discovery so
		// every other input is still mounted.
		return value
	}
	return value
}

// isComposeEnvNameByte reports whether b may appear in a $VAR name.
func isComposeEnvNameByte(b byte, first bool) bool {
	if b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
		return true
	}
	return !first && b >= '0' && b <= '9'
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
