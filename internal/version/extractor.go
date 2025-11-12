package version

import "strings"

// Extractor extracts version information from Docker image strings.
type Extractor struct {
	parser     *Parser
	comparator *Comparator
}

// NewExtractor creates a new version extractor.
func NewExtractor() *Extractor {
	return &Extractor{
		parser:     NewParser(),
		comparator: NewComparator(),
	}
}

// ImageInfo contains extracted information from a Docker image string.
type ImageInfo struct {
	// Full is the complete image string
	Full string

	// Registry is the registry URL (e.g., "ghcr.io", "docker.io")
	Registry string

	// Repository is the image repository (e.g., "linuxserver/plex")
	Repository string

	// Tag information
	Tag *TagInfo
}

// ExtractFromImage parses a full Docker image string.
// Examples:
//   - "nginx:1.21.3"
//   - "ghcr.io/linuxserver/plex:latest"
//   - "docker.io/library/nginx:1.21.3-alpine"
func (e *Extractor) ExtractFromImage(imageStr string) *ImageInfo {
	info := &ImageInfo{
		Full: imageStr,
	}

	// Split by last colon to separate tag
	lastColon := strings.LastIndex(imageStr, ":")
	var imagePath string
	var tag string

	if lastColon == -1 {
		// No tag specified
		imagePath = imageStr
		tag = "latest"
	} else {
		imagePath = imageStr[:lastColon]
		tag = imageStr[lastColon+1:]

		// Check if the "tag" is actually a port number (e.g., "localhost:5000/image")
		// If it contains a slash, it's part of the path, not a tag
		if strings.Contains(tag, "/") {
			imagePath = imageStr
			tag = "latest"
		}
	}

	// Extract registry and repository
	parts := strings.Split(imagePath, "/")
	switch len(parts) {
	case 1:
		// Just image name (e.g., "nginx")
		info.Registry = "docker.io"
		info.Repository = "library/" + parts[0]
	case 2:
		// User/image or registry/image
		if strings.Contains(parts[0], ".") || parts[0] == "localhost" {
			// Contains a dot or is localhost, likely a registry
			info.Registry = parts[0]
			info.Repository = parts[1]
		} else {
			// User/image format
			info.Registry = "docker.io"
			info.Repository = imagePath
		}
	default:
		// Full path with registry
		info.Registry = parts[0]
		info.Repository = strings.Join(parts[1:], "/")
	}

	// Parse tag
	fullImageTag := imagePath + ":" + tag
	info.Tag = e.parser.ParseImageTag(fullImageTag)

	return info
}

// CompareImages compares versions of two images and returns the change type.
func (e *Extractor) CompareImages(current, new string) ChangeType {
	currentInfo := e.ExtractFromImage(current)
	newInfo := e.ExtractFromImage(new)

	if currentInfo.Tag.Version == nil || newInfo.Tag.Version == nil {
		return NoChange
	}

	return e.comparator.GetChangeType(currentInfo.Tag.Version, newInfo.Tag.Version)
}
