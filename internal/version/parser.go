package version

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// Semantic version pattern: matches 1.2.3, v1.2.3, 1.2, only core version components
	// Requires at least major.minor (e.g., "1243" alone is NOT a valid semver)
	// Does NOT match prerelease or build - those are handled separately
	semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.(\d+))?`)

	// Prerelease/suffix pattern - anything after the version
	suffixPattern = regexp.MustCompile(`^v?\d+(?:\.\d+)?(?:\.\d+)?(.*)$`)

	// True prerelease identifiers (for semantic versioning)
	prereleaseIdentifiers = map[string]bool{
		"alpha": true, "beta": true, "rc": true, "dev": true,
		"pre": true, "preview": true, "canary": true,
	}

	// Date-based version patterns - need to check these BEFORE semantic versions
	// since they can look like semantic versions (e.g., 2024.01.15 looks like v2024.1.15)
	datePatterns = []struct {
		pattern *regexp.Regexp
		format  string
	}{
		{regexp.MustCompile(`^(\d{4})\.(\d{1,2})\.(\d{1,2})`), "2006.01.02"},
		{regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})`), "2006-01-02"},
		{regexp.MustCompile(`^(\d{8})$`), "20060102"},
		{regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})`), "20060102"},
	}

	// Commit hash patterns
	hashPattern = regexp.MustCompile(`^([a-f0-9]{7,40}|sha[0-9]+-[a-f0-9]+)`)
)

// Parser extracts version information from Docker image tags.
type Parser struct{}

// NewParser creates a new version parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseImageTag extracts version information from a full image tag.
// Examples:
//   - "nginx:1.21.3" -> version 1.21.3
//   - "nginx:1.21.3-alpine" -> version 1.21.3, suffix "alpine"
//   - "nginx:latest" -> latest, no version
//   - "myapp:2024.01.15" -> date-based version
//   - "myapp:abc123def" -> commit hash version
func (p *Parser) ParseImageTag(imageTag string) *TagInfo {
	info := &TagInfo{
		Full: imageTag,
	}

	// Split image name from tag
	parts := strings.Split(imageTag, ":")
	if len(parts) < 2 {
		// No tag specified, assume latest
		info.IsLatest = true
		return info
	}

	tag := parts[len(parts)-1]

	// Check for "latest" tag (with or without suffix)
	if tag == "latest" || strings.HasPrefix(tag, "latest-") {
		info.IsLatest = true
		// Extract suffix from tags like "latest-full", "latest-alpine"
		if strings.HasPrefix(tag, "latest-") {
			suffix := strings.TrimPrefix(tag, "latest-")
			info.Suffix = p.normalizeSuffix(suffix)
		}
		return info
	}

	// Try to parse as date-based version FIRST
	// Date versions can look like semantic versions (e.g., 2024.01.15)
	// so we need to check them first
	if dateVer := p.extractDateVersion(tag); dateVer != nil {
		info.Version = dateVer
		info.IsVersioned = true
		info.VersionType = "date"
		// Extract suffix from date-based tags too
		for _, dp := range datePatterns {
			if matches := dp.pattern.FindStringSubmatch(tag); matches != nil {
				suffix := strings.TrimPrefix(tag, matches[0])
				suffix = strings.TrimPrefix(suffix, "-")
				info.Suffix = p.normalizeSuffix(suffix)
				break
			}
		}
		return info
	}

	// Try to extract semantic version
	version, suffix := p.extractVersion(tag)
	if version != nil {
		info.Version = version
		info.IsVersioned = true
		info.Suffix = suffix
		info.VersionType = "semantic"
		return info
	}

	// Check if it's a commit hash
	if p.isCommitHash(tag) {
		info.IsVersioned = false // Hashes aren't really "versioned"
		info.VersionType = "hash"
		info.Hash = tag
		return info
	}

	// Check for meta tags
	metaTags := map[string]bool{
		"stable": true, "main": true, "master": true,
		"develop": true, "dev": true, "edge": true,
		"nightly": true, "beta": true, "alpha": true, "rc": true,
	}

	if metaTags[strings.ToLower(tag)] {
		info.VersionType = "meta"
		info.MetaTag = tag
	}

	return info
}

// ParseTag extracts version information from just the tag portion.
// Example: "1.21.3-alpine" -> version 1.21.3, suffix "alpine"
func (p *Parser) ParseTag(tag string) *Version {
	version, _ := p.extractVersion(tag)
	if version != nil {
		return version
	}

	// Try date-based version
	return p.extractDateVersion(tag)
}

// extractVersion attempts to extract a semantic version from a tag string.
// Returns the version and any remaining suffix.
func (p *Parser) extractVersion(tag string) (*Version, string) {
	// First, try to match the core version (1.2.3)
	matches := semverPattern.FindStringSubmatch(tag)
	if matches == nil {
		return nil, tag
	}

	version := &Version{
		Original: tag,
		Type:     "semantic",
	}

	// Parse major version
	if len(matches) > 1 && matches[1] != "" {
		version.Major, _ = strconv.Atoi(matches[1])
	}

	// Parse minor version
	if len(matches) > 2 && matches[2] != "" {
		version.Minor, _ = strconv.Atoi(matches[2])
	}

	// Parse patch version
	if len(matches) > 3 && matches[3] != "" {
		version.Patch, _ = strconv.Atoi(matches[3])
	}

	// Extract everything after the version (prerelease, build, and suffix)
	fullMatch := matches[0]
	remainder := strings.TrimPrefix(tag, fullMatch)

	if remainder == "" {
		return version, ""
	}

	// Remove leading separator
	remainder = strings.TrimPrefix(remainder, "-")
	remainder = strings.TrimPrefix(remainder, "+")

	// Check if remainder starts with a prerelease identifier
	suffix := remainder
	prerelease := ""

	// Split by common separators
	parts := strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '-' || r == '.' || r == '+'
	})

	if len(parts) > 0 {
		firstPart := strings.ToLower(parts[0])

		// Check if firstPart is or starts with a prerelease identifier
		// This handles cases like "dev", "dev202510300239", "rc1", "beta2", etc.
		isPrerelease := false
		for identifier := range prereleaseIdentifiers {
			if firstPart == identifier || strings.HasPrefix(firstPart, identifier) {
				isPrerelease = true
				break
			}
		}

		if isPrerelease {
			// This is a prerelease version
			// Find where the prerelease ends (before platform suffixes)
			// Prerelease can be: alpha, alpha.1, alpha-1, beta2, rc.3, dev202510300239, etc.
			prereleaseEnd := len(firstPart)
			if len(parts) > 1 {
				// Check if next part is a number (part of prerelease)
				if _, err := strconv.Atoi(parts[1]); err == nil {
					// Include the number as part of prerelease
					prereleaseEnd = strings.Index(remainder, parts[1]) + len(parts[1])
				}
			}

			prerelease = remainder[:prereleaseEnd]
			version.Prerelease = prerelease

			// Everything after prerelease is suffix
			suffix = strings.TrimPrefix(remainder[prereleaseEnd:], "-")
			suffix = strings.TrimPrefix(suffix, ".")
			suffix = strings.TrimPrefix(suffix, "+")
		}
	}

	// Normalize suffix by removing build metadata patterns
	suffix = p.normalizeSuffix(suffix)

	return version, suffix
}

// extractDateVersion attempts to extract a date-based version from a tag
func (p *Parser) extractDateVersion(tag string) *Version {
	for _, dp := range datePatterns {
		if matches := dp.pattern.FindStringSubmatch(tag); matches != nil {
			// Try to parse the date
			dateStr := matches[0]
			if t, err := time.Parse(dp.format, dateStr); err == nil {
				// Convert date to version-like structure for comparison
				// Use year as major, month as minor, day as patch
				return &Version{
					Original: tag,
					Type:     "date",
					Major:    t.Year(),
					Minor:    int(t.Month()),
					Patch:    t.Day(),
					Date:     &t,
				}
			}
		}
	}
	return nil
}

// isCommitHash checks if a tag appears to be a commit hash
func (p *Parser) isCommitHash(tag string) bool {
	// Remove common prefixes
	cleanTag := tag
	cleanTag = strings.TrimPrefix(cleanTag, "sha256-")
	cleanTag = strings.TrimPrefix(cleanTag, "sha1-")
	cleanTag = strings.TrimPrefix(cleanTag, "git-")

	// Check for hash pattern
	return hashPattern.MatchString(cleanTag)
}

// normalizeSuffix removes build metadata patterns from suffixes.
// Keeps variant identifiers like "alpine", "tensorrt" but removes build numbers like "ls286", "r3".
func (p *Parser) normalizeSuffix(suffix string) string {
	if suffix == "" {
		return ""
	}

	// Patterns to remove (build metadata, not variants)
	buildPatterns := []string{
		`-ls\d+`,           // LinuxServer build numbers: -ls286, -ls27
		`-r\d+`,            // Revision numbers: -r3, -r12
		`-\d{8}`,           // Date stamps: -20250413
		`-\d{12,}`,         // Long numbers/timestamps
		`-[0-9a-f]{7,}`,    // Git hashes: -8cddf87
		`\.dev\d+`,         // Dev builds: .dev20210710
		`-ubuntu[\d.]+`,    // Ubuntu version: -ubuntu18.04.1
		`-\d+$`,            // Trailing numbers: -1, -2
		`_\d+$`,            // Trailing numbers with underscore: _1, _2
		`^\.\d+`,           // Leading dot with numbers: .2946 (LinuxServer build numbers)
	}

	normalized := suffix
	for _, pattern := range buildPatterns {
		re := regexp.MustCompile(pattern)
		normalized = re.ReplaceAllString(normalized, "")
	}

	// Clean up any leading/trailing separators
	normalized = strings.Trim(normalized, "-_.")

	return normalized
}

// CompareVersions compares two versions regardless of their type
func (p *Parser) CompareVersions(v1, v2 *Version) int {
	if v1 == nil && v2 == nil {
		return 0
	}
	if v1 == nil {
		return -1
	}
	if v2 == nil {
		return 1
	}

	// If both are the same type, use type-specific comparison
	if v1.Type == v2.Type {
		switch v1.Type {
		case "semantic", "date":
			// Compare major.minor.patch
			if v1.Major != v2.Major {
				if v1.Major < v2.Major {
					return -1
				}
				return 1
			}
			if v1.Minor != v2.Minor {
				if v1.Minor < v2.Minor {
					return -1
				}
				return 1
			}
			if v1.Patch != v2.Patch {
				if v1.Patch < v2.Patch {
					return -1
				}
				return 1
			}
			// If semantic versions are equal, compare prerelease
			if v1.Type == "semantic" {
				// No prerelease is newer than prerelease
				if v1.Prerelease == "" && v2.Prerelease != "" {
					return 1
				}
				if v1.Prerelease != "" && v2.Prerelease == "" {
					return -1
				}
				// Both have prerelease, do string comparison
				if v1.Prerelease < v2.Prerelease {
					return -1
				}
				if v1.Prerelease > v2.Prerelease {
					return 1
				}
			}
			return 0

		case "hash":
			// Hashes can't really be compared for "newer"
			if v1.Original == v2.Original {
				return 0
			}
			return -2 // Indicates incomparable
		}
	}

	// Different types - generally incomparable
	return -2
}