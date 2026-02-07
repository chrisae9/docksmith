package compose

import (
	"os"
	"regexp"
	"strings"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ContainsEnvVar checks if a string contains Docker Compose environment variable syntax.
func ContainsEnvVar(s string) bool {
	return strings.Contains(s, "${")
}

// ExtractEnvVarName extracts the first environment variable name from a compose interpolation string.
// For "${OPENCLAW_IMAGE:-openclaw:latest}" returns "OPENCLAW_IMAGE".
// Returns empty string if no env var found.
func ExtractEnvVarName(s string) string {
	match := envVarPattern.FindStringSubmatch(s)
	if match == nil || len(match) < 2 {
		return ""
	}
	inner := match[1]
	// Strip the default/error suffixes
	for _, delim := range []string{":-", "-", ":?", "?", ":+"} {
		if idx := strings.Index(inner, delim); idx != -1 {
			return inner[:idx]
		}
	}
	return inner
}

// LoadDotEnv reads a .env file and returns a map of key=value pairs.
// Returns an empty map if the file doesn't exist or can't be read.
func LoadDotEnv(dir string) map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(dir + "/.env")
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			// Remove surrounding quotes
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			result[key] = val
		}
	}
	return result
}

// ReplaceTagInEnvVar updates the image tag inside a Docker Compose env var expression.
// For example: "${OPENCLAW_IMAGE:-openclaw:latest}" with newTag "v2.0"
// returns "${OPENCLAW_IMAGE:-openclaw:v2.0}".
// Returns the updated string and true if successful, or the original and false if not.
func ReplaceTagInEnvVar(s string, newTag string) (string, bool) {
	match := envVarPattern.FindStringSubmatchIndex(s)
	if match == nil {
		return s, false
	}

	// Extract the full match and inner content
	fullMatch := s[match[0]:match[1]]
	inner := s[match[2]:match[3]]

	// Find the default value delimiter (:-  or  -)
	var varName, defaultVal, delimiter string
	if idx := strings.Index(inner, ":-"); idx != -1 {
		varName = inner[:idx]
		defaultVal = inner[idx+2:]
		delimiter = ":-"
	} else if idx := strings.Index(inner, "-"); idx != -1 {
		varName = inner[:idx]
		defaultVal = inner[idx+1:]
		delimiter = "-"
	} else {
		// No default value — can't update tag
		return s, false
	}

	_ = varName // used for clarity

	// Split the default value to find image:tag
	// Use LastIndex to handle registries with ports (e.g., registry:5000/image:tag)
	lastColon := strings.LastIndex(defaultVal, ":")
	if lastColon != -1 {
		// Replace the tag portion
		defaultVal = defaultVal[:lastColon+1] + newTag
	} else {
		// No tag in default — append one
		defaultVal = defaultVal + ":" + newTag
	}

	// Reconstruct: ${VAR:-image:newtag}
	newExpr := "${" + inner[:strings.Index(inner, delimiter)] + delimiter + defaultVal + "}"

	// Replace in the original string (preserving any prefix/suffix around the env var)
	result := s[:match[0]] + newExpr + s[match[1]:]

	// Verify the full match was replaced (sanity check)
	_ = fullMatch

	return result, true
}

// ResolveEnvVars resolves Docker Compose environment variable syntax in a string.
// Supports ${VAR}, ${VAR:-default}, and ${VAR-default} patterns.
// Tries the process environment first, then falls back to the default value.
// Unresolvable variables are left as-is.
func ResolveEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1]

		// ${VAR:-default} — use default if unset or empty
		if idx := strings.Index(inner, ":-"); idx != -1 {
			varName := inner[:idx]
			defaultVal := inner[idx+2:]
			if val, ok := os.LookupEnv(varName); ok && val != "" {
				return val
			}
			return defaultVal
		}

		// ${VAR-default} — use default if unset
		if idx := strings.Index(inner, "-"); idx != -1 {
			varName := inner[:idx]
			defaultVal := inner[idx+1:]
			if _, ok := os.LookupEnv(varName); ok {
				return os.Getenv(varName)
			}
			return defaultVal
		}

		// ${VAR} — simple env var
		if val, ok := os.LookupEnv(inner); ok {
			return val
		}

		return match
	})
}
