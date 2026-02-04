package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckComposeMismatch tests all scenarios that can cause COMPOSE_MISMATCH status
func TestCheckComposeMismatch(t *testing.T) {
	// Create a checker with nil dependencies (they're not used for compose mismatch checks)
	checker := &Checker{}

	t.Run("non-compose container returns no mismatch", func(t *testing.T) {
		// Container without compose labels should not trigger mismatch
		container := docker.Container{
			ID:     "abc123",
			Name:   "standalone-container",
			Image:  "nginx:1.25",
			Labels: map[string]string{}, // No compose labels
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Non-compose container should not have mismatch")
		assert.Empty(t, expectedImage)
	})

	t.Run("container with empty compose config file returns no mismatch", func(t *testing.T) {
		container := docker.Container{
			ID:    "abc123",
			Name:  "container-with-empty-label",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": "",
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Container with empty compose config should not have mismatch")
		assert.Empty(t, expectedImage)
	})

	// SCENARIO 1: Lost tag reference tests

	t.Run("lost tag reference with sha256 prefix", func(t *testing.T) {
		// When container.Image starts with "sha256:" - container lost its tag reference
		container := docker.Container{
			ID:    "abc123",
			Name:  "lost-tag-container",
			Image: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": "/some/path/docker-compose.yaml",
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Container with sha256 prefix should be detected as mismatch")
		assert.Contains(t, expectedImage, "lost tag reference", "Should indicate lost tag reference")
	})

	t.Run("lost tag reference with 64-char hex digest only", func(t *testing.T) {
		// When container.Image is exactly 64 hex chars (bare digest without prefix)
		container := docker.Container{
			ID:    "abc123",
			Name:  "bare-digest-container",
			Image: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": "/some/path/docker-compose.yaml",
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Container with bare 64-char digest should be detected as mismatch")
		assert.Contains(t, expectedImage, "lost tag reference", "Should indicate lost tag reference")
	})

	t.Run("normal image name with 64 chars is not detected as bare digest", func(t *testing.T) {
		// A normal image name that happens to be long should not be confused with a digest
		// This tests the colon check: if there's a colon, it's not a bare digest
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  long-name-service:
    container_name: long-name-container
    image: some-very-long-registry-name.example.com/namespace/imagename:v1.0.0
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "long-name-container",
			Image: "some-very-long-registry-name.example.com/namespace/imagename:v1.0.0",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Normal long image name with colon should not be detected as bare digest")
	})

	// SCENARIO 2: Running image differs from compose file tests

	t.Run("tag mismatch - running older version", func(t *testing.T) {
		// Container running nginx:1.24 but compose specifies nginx:1.25
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.24",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Tag mismatch should be detected")
		assert.Equal(t, "nginx:1.25", expectedImage, "Expected image should match compose file")
	})

	t.Run("tag mismatch - running newer version than compose", func(t *testing.T) {
		// Container running nginx:1.26 but compose specifies nginx:1.25
		// (user manually pulled newer image but didn't update compose)
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.26",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Running newer version than compose should be detected as mismatch")
		assert.Equal(t, "nginx:1.25", expectedImage)
	})

	t.Run("tag mismatch - different tag variant", func(t *testing.T) {
		// Container running nginx:alpine but compose specifies nginx:latest
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:latest
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Different tag variant should be detected as mismatch")
		assert.Equal(t, "nginx:latest", expectedImage)
	})

	t.Run("implicit latest mismatch - compose has no tag, container has specific tag", func(t *testing.T) {
		// Compose file has "image: nginx" (implies :latest), but container is running nginx:1.25
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Implicit :latest vs explicit tag should be detected as mismatch")
		assert.Equal(t, "nginx", expectedImage, "Expected image should be the raw compose spec")
	})

	t.Run("image with digest suffix - digest stripped for comparison", func(t *testing.T) {
		// Container image has @sha256:... suffix which should be stripped for comparison
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25@sha256:abc123def456",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Image with digest suffix matching compose tag should not be mismatch")
	})

	t.Run("image with digest suffix but different tag", func(t *testing.T) {
		// Container image has @sha256:... suffix but different base tag
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.26
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25@sha256:abc123def456",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Image with digest suffix but different tag should be mismatch")
		assert.Equal(t, "nginx:1.26", expectedImage)
	})

	// No mismatch cases

	t.Run("no mismatch - exact same tag", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Same tag should not be detected as mismatch")
	})

	t.Run("no mismatch - both implicit latest", func(t *testing.T) {
		// Both compose and container use implicit :latest
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx", // No tag = implicit :latest
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Both using implicit :latest should not be mismatch")
	})

	t.Run("no mismatch - explicit latest matching implicit latest", func(t *testing.T) {
		// Compose has "nginx" (implicit latest), container has "nginx:latest" (explicit)
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:latest",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Explicit :latest should match implicit :latest")
	})

	t.Run("no mismatch - full registry path match", func(t *testing.T) {
		// Full registry paths should match correctly
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  app:
    container_name: app
    image: ghcr.io/myorg/myapp:v1.2.3
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "app",
			Image: "ghcr.io/myorg/myapp:v1.2.3",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Full registry path match should not be mismatch")
	})

	// Edge cases and error handling

	t.Run("compose file not found returns no mismatch", func(t *testing.T) {
		// If compose file doesn't exist, we can't verify - should not error
		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25", // Normal image, not a bare digest
			Labels: map[string]string{
				"com.docker.compose.project.config_files": "/nonexistent/path/docker-compose.yaml",
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Missing compose file should not cause mismatch (fail open)")
	})

	t.Run("service not found in compose file returns no mismatch", func(t *testing.T) {
		// Container name doesn't match any service in compose file
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  other-service:
    container_name: other-service
    image: redis:7
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web", // Not in compose file
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Service not found in compose should not cause mismatch")
	})

	t.Run("build-based service with no image returns no mismatch", func(t *testing.T) {
		// Service uses build: instead of image: - no image to compare
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  custom-app:
    container_name: custom-app
    build:
      context: .
      dockerfile: Dockerfile
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "custom-app",
			Image: "custom-app:latest", // Some local image
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Build-based service without image key should not cause mismatch")
	})

	t.Run("multiple compose files - uses first one", func(t *testing.T) {
		// Label can contain multiple comma-separated paths
		tmpDir := t.TempDir()
		composePath1 := filepath.Join(tmpDir, "docker-compose.yaml")
		composePath2 := filepath.Join(tmpDir, "docker-compose.override.yaml")

		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.26
`
		err := os.WriteFile(composePath1, []byte(composeContent), 0644)
		require.NoError(t, err)

		overrideContent := `
services:
  web:
    ports:
      - "8080:80"
`
		err = os.WriteFile(composePath2, []byte(overrideContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath1 + "," + composePath2,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect mismatch using first compose file")
		assert.Equal(t, "nginx:1.26", expectedImage)
	})

	t.Run("corrupted compose file returns no mismatch", func(t *testing.T) {
		// Invalid YAML should not cause errors
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
this is not valid yaml: {{{{
  definitely broken
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Corrupted compose file should not cause mismatch (fail open)")
	})

	t.Run("whitespace in compose file path is trimmed", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.26
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				// Path with leading/trailing whitespace
				"com.docker.compose.project.config_files": "  " + composePath + "  ",
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect mismatch after trimming whitespace from path")
		assert.Equal(t, "nginx:1.26", expectedImage)
	})
}

// TestComposeMismatchRealWorldScenarios tests specific real-world scenarios
// that could cause containers to end up in COMPOSE_MISMATCH state
func TestComposeMismatchRealWorldScenarios(t *testing.T) {
	checker := &Checker{}

	t.Run("scenario: user edited compose file but forgot docker compose up", func(t *testing.T) {
		// User updated nginx:1.24 -> nginx:1.25 in compose file
		// but didn't run `docker compose up -d`
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.24", // Old version still running
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect edit-without-recreate scenario")
		assert.Equal(t, "nginx:1.25", expectedImage)
	})

	t.Run("scenario: user manually pulled and recreated without updating compose", func(t *testing.T) {
		// User ran `docker pull nginx:1.26 && docker compose up -d --force-recreate`
		// but compose file still says nginx:1.25
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.26", // Newer version running
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect manual-pull-without-compose-update scenario")
		assert.Equal(t, "nginx:1.25", expectedImage)
	})

	t.Run("scenario: container lost tag after garbage collection", func(t *testing.T) {
		// Docker garbage collection removed the tag but kept the container running
		// Container now shows sha256:... instead of nginx:1.25
		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "sha256:e4720093a3c1381245b53a5a51b417963b3c50e8de2d5e2a61a62832c6ac5d7c",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": "/path/to/docker-compose.yaml",
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect garbage-collection lost tag scenario")
		assert.Contains(t, expectedImage, "lost tag reference")
	})

	t.Run("scenario: image was force-replaced by different image", func(t *testing.T) {
		// Someone ran `docker tag other-image:v1 nginx:1.25` overwriting the tag
		// The compose file still expects the real nginx:1.25
		// This is detected because the container's image differs from compose
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:custom-build", // Different tag entirely
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect force-replaced image scenario")
		assert.Equal(t, "nginx:1.25", expectedImage)
	})

	t.Run("scenario: changed from :latest to specific version in compose", func(t *testing.T) {
		// User updated compose from `image: nginx` to `image: nginx:1.25`
		// Container is still running the old :latest image
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: nginx:1.25
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:latest", // Old :latest still running
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect latest-to-versioned migration")
		assert.Equal(t, "nginx:1.25", expectedImage)
	})

	t.Run("scenario: switched image registries in compose", func(t *testing.T) {
		// User changed from Docker Hub to GHCR in compose
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  app:
    container_name: app
    image: ghcr.io/myorg/myapp:v1.0.0
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "app",
			Image: "docker.io/myorg/myapp:v1.0.0", // Old registry
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, expectedImage := checker.checkComposeMismatch(container)
		assert.True(t, mismatch, "Should detect registry change")
		assert.Equal(t, "ghcr.io/myorg/myapp:v1.0.0", expectedImage)
	})
}

// TestComposeMismatchQuotedImageValues tests handling of quoted image values in YAML
func TestComposeMismatchQuotedImageValues(t *testing.T) {
	checker := &Checker{}

	t.Run("single quoted image value", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: 'nginx:1.25'
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Single quoted image should match")
	})

	t.Run("double quoted image value", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yaml")
		composeContent := `
services:
  web:
    container_name: web
    image: "nginx:1.25"
`
		err := os.WriteFile(composePath, []byte(composeContent), 0644)
		require.NoError(t, err)

		container := docker.Container{
			ID:    "abc123",
			Name:  "web",
			Image: "nginx:1.25",
			Labels: map[string]string{
				"com.docker.compose.project.config_files": composePath,
			},
		}

		mismatch, _ := checker.checkComposeMismatch(container)
		assert.False(t, mismatch, "Double quoted image should match")
	})
}
