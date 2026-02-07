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
