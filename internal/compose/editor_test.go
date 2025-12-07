package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test compose file content for various scenarios
const validComposeYAML = `services:
  web:
    image: nginx:latest
    container_name: my-nginx
    labels:
      - "app=web"
      - "env=prod"
  db:
    image: postgres:15
    container_name: my-postgres
`

const composeWithMappingLabels = `services:
  app:
    image: myapp:1.0
    container_name: my-app
    labels:
      app: web
      env: production
`

const composeWithoutLabels = `services:
  simple:
    image: alpine:latest
    container_name: simple-service
`

const composeWithoutServices = `version: "3.8"
networks:
  default:
    driver: bridge
`

const composeWithIncludes = `include:
  - services/web.yml
  - services/db.yml
`

// TestLoadComposeFile tests loading compose files
func TestLoadComposeFile(t *testing.T) {
	t.Run("valid compose file loads successfully", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)

		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)
		assert.NotNil(t, cf)
		assert.Equal(t, tmpFile, cf.Path)
		assert.NotNil(t, cf.Root)
		assert.NotNil(t, cf.Services)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := LoadComposeFile("/nonexistent/docker-compose.yml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read compose file")
	})

	t.Run("file without services section returns error", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithoutServices)

		_, err := LoadComposeFile(tmpFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no services section found")
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, "invalid: [yaml: content")

		_, err := LoadComposeFile(tmpFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse compose file")
	})

	t.Run("empty file returns error", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, "")

		_, err := LoadComposeFile(tmpFile)
		assert.Error(t, err)
	})
}

// TestFindServiceByContainerName tests finding services by container name
func TestFindServiceByContainerName(t *testing.T) {
	t.Run("finds service by container_name", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)
		assert.NotNil(t, service)
		assert.Equal(t, "web", service.Name)
	})

	t.Run("finds service by service name when no container_name", func(t *testing.T) {
		yaml := `services:
  myservice:
    image: alpine:latest
`
		tmpFile := createTempComposeFile(t, yaml)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("myservice")
		require.NoError(t, err)
		assert.NotNil(t, service)
		assert.Equal(t, "myservice", service.Name)
	})

	t.Run("returns error for non-existent container", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		_, err = cf.FindServiceByContainerName("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service not found")
	})
}

// TestGetOrCreateLabelsNode tests creating and retrieving label nodes
func TestGetOrCreateLabelsNode(t *testing.T) {
	t.Run("gets existing labels node (sequence style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		labelsNode, err := service.GetOrCreateLabelsNode()
		require.NoError(t, err)
		assert.NotNil(t, labelsNode)
		assert.Equal(t, service.Labels, labelsNode)
	})

	t.Run("gets existing labels node (mapping style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithMappingLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-app")
		require.NoError(t, err)

		labelsNode, err := service.GetOrCreateLabelsNode()
		require.NoError(t, err)
		assert.NotNil(t, labelsNode)
	})

	t.Run("creates labels node when none exists", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithoutLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("simple-service")
		require.NoError(t, err)

		labelsNode, err := service.GetOrCreateLabelsNode()
		require.NoError(t, err)
		assert.NotNil(t, labelsNode)
		assert.Equal(t, service.Labels, labelsNode)
	})
}

// TestSetLabel tests adding and updating labels
func TestSetLabel(t *testing.T) {
	t.Run("adds new label (sequence style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		err = service.SetLabel("new-key", "new-value")
		require.NoError(t, err)

		// Verify label was added
		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "new-value", labels["new-key"])
	})

	t.Run("updates existing label (sequence style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		// Update existing "app" label
		err = service.SetLabel("app", "updated-web")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "updated-web", labels["app"])
	})

	t.Run("adds new label (mapping style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithMappingLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-app")
		require.NoError(t, err)

		err = service.SetLabel("new-key", "new-value")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "new-value", labels["new-key"])
	})

	t.Run("updates existing label (mapping style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithMappingLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-app")
		require.NoError(t, err)

		err = service.SetLabel("app", "updated-app")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "updated-app", labels["app"])
	})

	t.Run("adds label to service without existing labels", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithoutLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("simple-service")
		require.NoError(t, err)

		err = service.SetLabel("new-label", "new-value")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "new-value", labels["new-label"])
	})
}

// TestRemoveLabel tests removing labels
func TestRemoveLabel(t *testing.T) {
	t.Run("removes existing label (sequence style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		err = service.RemoveLabel("app")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		_, exists := labels["app"]
		assert.False(t, exists)
		// Other labels should still exist
		assert.Equal(t, "prod", labels["env"])
	})

	t.Run("removes existing label (mapping style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithMappingLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-app")
		require.NoError(t, err)

		err = service.RemoveLabel("app")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		_, exists := labels["app"]
		assert.False(t, exists)
		// Other labels should still exist
		assert.Equal(t, "production", labels["env"])
	})

	t.Run("removing non-existent label is not an error", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		err = service.RemoveLabel("non-existent-label")
		assert.NoError(t, err)
	})
}

// TestGetAllLabels tests retrieving all labels
func TestGetAllLabels(t *testing.T) {
	t.Run("returns all labels (sequence style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "web", labels["app"])
		assert.Equal(t, "prod", labels["env"])
	})

	t.Run("returns all labels (mapping style)", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithMappingLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-app")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "web", labels["app"])
		assert.Equal(t, "production", labels["env"])
	})

	t.Run("returns empty map for service without labels", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, composeWithoutLabels)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("simple-service")
		require.NoError(t, err)

		labels, err := service.GetAllLabels()
		require.NoError(t, err)
		// Will create empty labels section
		assert.NotNil(t, labels)
	})
}

// TestSave tests saving compose files
func TestSave(t *testing.T) {
	t.Run("saves modified compose file", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		cf, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service, err := cf.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		err = service.SetLabel("test-label", "test-value")
		require.NoError(t, err)

		err = cf.Save()
		require.NoError(t, err)

		// Reload and verify
		cf2, err := LoadComposeFile(tmpFile)
		require.NoError(t, err)

		service2, err := cf2.FindServiceByContainerName("my-nginx")
		require.NoError(t, err)

		labels, err := service2.GetAllLabels()
		require.NoError(t, err)
		assert.Equal(t, "test-value", labels["test-label"])
	})
}

// TestBackupAndRestore tests backup and restore functionality
func TestBackupAndRestore(t *testing.T) {
	t.Run("creates and restores backup", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)

		// Create backup
		backupPath, err := BackupComposeFile(tmpFile)
		require.NoError(t, err)
		assert.NotEmpty(t, backupPath)
		defer os.Remove(backupPath)

		// Verify backup exists
		backupContent, err := os.ReadFile(backupPath)
		require.NoError(t, err)
		assert.Equal(t, validComposeYAML, string(backupContent))

		// Modify original
		err = os.WriteFile(tmpFile, []byte("modified content"), 0644)
		require.NoError(t, err)

		// Restore
		err = RestoreFromBackup(tmpFile, backupPath)
		require.NoError(t, err)

		// Verify restoration
		restoredContent, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Equal(t, validComposeYAML, string(restoredContent))
	})

	t.Run("backup fails for non-existent file", func(t *testing.T) {
		_, err := BackupComposeFile("/nonexistent/file.yml")
		assert.Error(t, err)
	})

	t.Run("restore fails for non-existent backup", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)
		err := RestoreFromBackup(tmpFile, "/nonexistent/backup.yml")
		assert.Error(t, err)
	})
}

// TestGetIncludePaths tests extraction of include paths
func TestGetIncludePaths(t *testing.T) {
	t.Run("returns include paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		err := os.WriteFile(composePath, []byte(composeWithIncludes), 0644)
		require.NoError(t, err)

		includes, err := GetIncludePaths(composePath)
		require.NoError(t, err)
		require.Len(t, includes, 2)

		// Paths should be resolved relative to compose file
		assert.True(t, strings.HasSuffix(includes[0], "services/web.yml"))
		assert.True(t, strings.HasSuffix(includes[1], "services/db.yml"))
	})

	t.Run("returns nil for file without includes", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)

		includes, err := GetIncludePaths(tmpFile)
		require.NoError(t, err)
		assert.Nil(t, includes)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := GetIncludePaths("/nonexistent/docker-compose.yml")
		assert.Error(t, err)
	})
}

// TestFindServiceInIncludes tests finding services in included files
func TestFindServiceInIncludes(t *testing.T) {
	t.Run("finds service in included file", func(t *testing.T) {
		// Create temp directory structure
		tmpDir := t.TempDir()
		servicesDir := filepath.Join(tmpDir, "services")
		err := os.MkdirAll(servicesDir, 0755)
		require.NoError(t, err)

		// Create main compose file with includes
		mainCompose := `include:
  - services/web.yml
`
		mainPath := filepath.Join(tmpDir, "docker-compose.yml")
		err = os.WriteFile(mainPath, []byte(mainCompose), 0644)
		require.NoError(t, err)

		// Create included web.yml
		webCompose := `services:
  nginx:
    image: nginx:latest
    container_name: my-web-nginx
`
		webPath := filepath.Join(servicesDir, "web.yml")
		err = os.WriteFile(webPath, []byte(webCompose), 0644)
		require.NoError(t, err)

		// Find service
		foundPath, err := FindServiceInIncludes(mainPath, "my-web-nginx")
		require.NoError(t, err)
		assert.Equal(t, webPath, foundPath)
	})

	t.Run("returns error when service not found in any includes", func(t *testing.T) {
		tmpDir := t.TempDir()
		servicesDir := filepath.Join(tmpDir, "services")
		err := os.MkdirAll(servicesDir, 0755)
		require.NoError(t, err)

		mainCompose := `include:
  - services/web.yml
`
		mainPath := filepath.Join(tmpDir, "docker-compose.yml")
		err = os.WriteFile(mainPath, []byte(mainCompose), 0644)
		require.NoError(t, err)

		webCompose := `services:
  nginx:
    image: nginx:latest
`
		webPath := filepath.Join(servicesDir, "web.yml")
		err = os.WriteFile(webPath, []byte(webCompose), 0644)
		require.NoError(t, err)

		_, err = FindServiceInIncludes(mainPath, "non-existent-service")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service not found")
	})

	t.Run("returns empty string when no includes", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)

		path, err := FindServiceInIncludes(tmpFile, "my-nginx")
		require.NoError(t, err)
		assert.Empty(t, path)
	})
}

// TestParseLabelFunctions tests the label parsing helper functions
func TestParseLabelFunctions(t *testing.T) {
	t.Run("parseLabelKey extracts key", func(t *testing.T) {
		assert.Equal(t, "key", parseLabelKey("key=value"))
		assert.Equal(t, "simple-key", parseLabelKey("simple-key=complex=value=here"))
		assert.Equal(t, "no-equals", parseLabelKey("no-equals"))
		assert.Equal(t, "", parseLabelKey(""))
	})

	t.Run("parseLabelValue extracts value", func(t *testing.T) {
		assert.Equal(t, "value", parseLabelValue("key=value"))
		assert.Equal(t, "complex=value=here", parseLabelValue("simple-key=complex=value=here"))
		assert.Equal(t, "", parseLabelValue("no-equals"))
		assert.Equal(t, "", parseLabelValue(""))
	})
}

// TestLoadComposeFileOrIncluded tests loading compose files with includes
func TestLoadComposeFileOrIncluded(t *testing.T) {
	t.Run("loads regular compose file directly", func(t *testing.T) {
		tmpFile := createTempComposeFile(t, validComposeYAML)

		cf, err := LoadComposeFileOrIncluded(tmpFile, "my-nginx")
		require.NoError(t, err)
		assert.NotNil(t, cf)
	})

	t.Run("follows includes when main file has no services", func(t *testing.T) {
		tmpDir := t.TempDir()
		servicesDir := filepath.Join(tmpDir, "services")
		err := os.MkdirAll(servicesDir, 0755)
		require.NoError(t, err)

		// Main file with only includes (no services)
		mainCompose := `include:
  - services/web.yml
`
		mainPath := filepath.Join(tmpDir, "docker-compose.yml")
		err = os.WriteFile(mainPath, []byte(mainCompose), 0644)
		require.NoError(t, err)

		// Included file with services
		webCompose := `services:
  nginx:
    image: nginx:latest
    container_name: included-nginx
`
		webPath := filepath.Join(servicesDir, "web.yml")
		err = os.WriteFile(webPath, []byte(webCompose), 0644)
		require.NoError(t, err)

		cf, err := LoadComposeFileOrIncluded(mainPath, "included-nginx")
		require.NoError(t, err)
		assert.NotNil(t, cf)
		assert.Equal(t, webPath, cf.Path)
	})
}

// Helper function to create temporary compose files
func createTempComposeFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)
	return tmpFile
}
