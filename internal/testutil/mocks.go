// Package testutil provides shared testing utilities for the docksmith test suite.
// This package contains fixtures, test data factories, and common assertions.
package testutil

import (
	"errors"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

// Common test errors for use in mocks
var (
	ErrMockNotFound    = errors.New("not found")
	ErrMockUnavailable = errors.New("service unavailable")
	ErrMockPermission  = errors.New("permission denied")
	ErrMockTimeout     = errors.New("operation timed out")
	ErrMockDatabase    = errors.New("database error")
)

// TestContainer holds common test container names
var TestContainer = struct {
	Nginx    string
	Redis    string
	Postgres string
}{
	Nginx:    "test-nginx",
	Redis:    "test-redis",
	Postgres: "test-postgres",
}

// TestStack holds common test stack names
var TestStack = struct {
	Basic   string
	Multi   string
	Include string
}{
	Basic:   "basic-compose",
	Multi:   "multi-stack",
	Include: "include-compose",
}

// NewCheckHistoryEntry creates a CheckHistoryEntry for testing
func NewCheckHistoryEntry(containerName, status string) storage.CheckHistoryEntry {
	return storage.CheckHistoryEntry{
		ContainerName:  containerName,
		Image:          containerName + ":latest",
		CurrentVersion: "1.0.0",
		LatestVersion:  "1.1.0",
		Status:         status,
		CheckTime:      time.Now(),
	}
}

// NewUpdateLogEntry creates an UpdateLogEntry for testing
func NewUpdateLogEntry(containerName string, success bool) storage.UpdateLogEntry {
	entry := storage.UpdateLogEntry{
		ContainerName: containerName,
		Operation:     "update",
		FromVersion:   "1.0.0",
		ToVersion:     "1.1.0",
		Success:       success,
		Timestamp:     time.Now(),
	}
	if !success {
		entry.Error = "update failed"
	}
	return entry
}

// NewUpdateOperation creates an UpdateOperation for testing
func NewUpdateOperation(operationID, containerName, status string) storage.UpdateOperation {
	now := time.Now()
	return storage.UpdateOperation{
		OperationID:   operationID,
		ContainerName: containerName,
		StackName:     "test-stack",
		OperationType: "update",
		Status:        status,
		OldVersion:    "1.0.0",
		NewVersion:    "1.1.0",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// NewComposeBackup creates a ComposeBackup for testing
func NewComposeBackup(operationID, containerName string) storage.ComposeBackup {
	now := time.Now()
	return storage.ComposeBackup{
		OperationID:     operationID,
		ContainerName:   containerName,
		StackName:       "test-stack",
		ComposeFilePath: "/opt/stacks/" + containerName + "/docker-compose.yml",
		BackupFilePath:  "/data/backups/" + operationID + ".yml",
		BackupTimestamp: now,
		CreatedAt:       now,
	}
}

// NewRollbackPolicy creates a RollbackPolicy for testing
func NewRollbackPolicy(entityType string) storage.RollbackPolicy {
	now := time.Now()
	return storage.RollbackPolicy{
		EntityType:          entityType,
		AutoRollbackEnabled: true,
		HealthCheckRequired: true,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

// GenerateOperationID creates a test operation ID
func GenerateOperationID() string {
	return "test-op-" + time.Now().Format("20060102150405")
}
