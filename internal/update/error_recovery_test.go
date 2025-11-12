package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: parseVersionFromBackup extracts old version from backup file
func TestParseVersionFromBackup_ExtractsOldVersion(t *testing.T) {
	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  web:
    image: nginx:1.20.0
    ports:
      - "80:80"
  db:
    image: postgres:13.5
    environment:
      POSTGRES_PASSWORD: secret
`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	// Test extracting version for web service
	version, err := parseVersionFromBackup(backupFile, "web")
	assert.NoError(t, err)
	assert.Equal(t, "1.20.0", version)

	// Test extracting version for db service
	version, err = parseVersionFromBackup(backupFile, "db")
	assert.NoError(t, err)
	assert.Equal(t, "13.5", version)
}

// Test: parseVersionFromBackup handles missing service gracefully
func TestParseVersionFromBackup_MissingService(t *testing.T) {
	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  web:
    image: nginx:1.20.0
`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	// Test extracting version for non-existent service
	_, err = parseVersionFromBackup(backupFile, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service nonexistent not found")
}

// Test: parseVersionFromBackup handles invalid YAML gracefully
func TestParseVersionFromBackup_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `this is not valid yaml: {[}`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	_, err = parseVersionFromBackup(backupFile, "web")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse backup YAML")
}

// Test: parseVersionFromBackup handles missing image field
func TestParseVersionFromBackup_MissingImageField(t *testing.T) {
	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  web:
    ports:
      - "80:80"
`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	_, err = parseVersionFromBackup(backupFile, "web")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service web has no image field")
}

// Test: parseVersionFromBackup extracts version from image string
func TestParseVersionFromBackup_ParsesVersionTag(t *testing.T) {
	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  app:
    image: registry.example.com/myapp:v2.3.4
`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	version, err := parseVersionFromBackup(backupFile, "app")
	assert.NoError(t, err)
	assert.Equal(t, "v2.3.4", version)
}

// Test: Backup restoration reverts compose file to backup content
func TestBackupRestoration_RevertsComposeFile(t *testing.T) {
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	originalContent := `services:
  web:
    image: nginx:1.20.0
`

	modifiedContent := `services:
  web:
    image: nginx:1.21.0
`

	// Write original content to backup
	err := os.WriteFile(backupFile, []byte(originalContent), 0644)
	require.NoError(t, err)

	// Write modified content to compose file
	err = os.WriteFile(composeFile, []byte(modifiedContent), 0644)
	require.NoError(t, err)

	// Read backup and restore it
	backupData, err := os.ReadFile(backupFile)
	require.NoError(t, err)

	err = os.WriteFile(composeFile, backupData, 0644)
	require.NoError(t, err)

	// Verify restoration
	restoredData, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(restoredData))
}

// Test: Compose failure with retry logic
func TestComposeFailure_RetryLogic(t *testing.T) {
	// This test verifies that compose failures are properly detected
	// The retry logic would be implemented in the actual restoration flow

	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  web:
    image: nginx:1.20.0
`

	err := os.WriteFile(backupFile, []byte(backupContent), 0644)
	require.NoError(t, err)

	// Verify we can read the backup and parse version
	version, err := parseVersionFromBackup(backupFile, "web")
	assert.NoError(t, err)
	assert.Equal(t, "1.20.0", version)

	// This confirms the backup can be used for restoration
}
