package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestSaveAndGetUpdateOperation tests saving and retrieving update operations
func TestSaveAndGetUpdateOperation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create and save an update operation
	op := UpdateOperation{
		OperationID:        "test-op-001",
		ContainerID:        "container-123",
		ContainerName:      "test-container",
		StackName:          "test-stack",
		OperationType:      "single",
		Status:             "queued",
		OldVersion:         "1.0.0",
		NewVersion:         "1.1.0",
		StartedAt:          &now,
		DependentsAffected: []string{"dependent-1", "dependent-2"},
		RollbackOccurred:   false,
	}

	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save update operation: %v", err)
	}

	// Retrieve the operation
	retrieved, found, err := storage.GetUpdateOperation(ctx, "test-op-001")
	if err != nil {
		t.Fatalf("Failed to get update operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find saved update operation")
	}

	// Verify fields
	if retrieved.OperationID != op.OperationID {
		t.Errorf("Expected operation ID %s, got %s", op.OperationID, retrieved.OperationID)
	}
	if retrieved.ContainerName != op.ContainerName {
		t.Errorf("Expected container name %s, got %s", op.ContainerName, retrieved.ContainerName)
	}
	if retrieved.Status != op.Status {
		t.Errorf("Expected status %s, got %s", op.Status, retrieved.Status)
	}
	if len(retrieved.DependentsAffected) != 2 {
		t.Errorf("Expected 2 dependents affected, got %d", len(retrieved.DependentsAffected))
	}
}

// TestUpdateOperationStatus tests updating operation status
func TestUpdateOperationStatus(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create initial operation
	op := UpdateOperation{
		OperationID:   "test-op-002",
		ContainerName: "test-container",
		StackName:     "test-stack",
		OperationType: "single",
		Status:        "queued",
	}

	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save update operation: %v", err)
	}

	// Update status
	err = storage.UpdateOperationStatus(ctx, "test-op-002", "complete", "")
	if err != nil {
		t.Fatalf("Failed to update operation status: %v", err)
	}

	// Verify status was updated
	retrieved, found, err := storage.GetUpdateOperation(ctx, "test-op-002")
	if err != nil {
		t.Fatalf("Failed to get update operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find update operation")
	}
	if retrieved.Status != "complete" {
		t.Errorf("Expected status 'complete', got %s", retrieved.Status)
	}
}

// TestGetUpdateOperationsByStatus tests filtering operations by status
func TestGetUpdateOperationsByStatus(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create multiple operations with different statuses
	operations := []UpdateOperation{
		{OperationID: "op-001", ContainerName: "container-1", OperationType: "single", Status: "queued"},
		{OperationID: "op-002", ContainerName: "container-2", OperationType: "single", Status: "queued"},
		{OperationID: "op-003", ContainerName: "container-3", OperationType: "single", Status: "complete"},
	}

	for _, op := range operations {
		err = storage.SaveUpdateOperation(ctx, op)
		if err != nil {
			t.Fatalf("Failed to save update operation: %v", err)
		}
	}

	// Query queued operations
	queued, err := storage.GetUpdateOperationsByStatus(ctx, "queued", 10)
	if err != nil {
		t.Fatalf("Failed to get queued operations: %v", err)
	}

	if len(queued) != 2 {
		t.Errorf("Expected 2 queued operations, got %d", len(queued))
	}

	// Query complete operations
	complete, err := storage.GetUpdateOperationsByStatus(ctx, "complete", 10)
	if err != nil {
		t.Fatalf("Failed to get complete operations: %v", err)
	}

	if len(complete) != 1 {
		t.Errorf("Expected 1 complete operation, got %d", len(complete))
	}
}

// TestSaveAndGetComposeBackup tests saving and retrieving compose backups
func TestSaveAndGetComposeBackup(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create and save compose backup
	backup := ComposeBackup{
		OperationID:     "test-op-003",
		ContainerName:   "test-container",
		StackName:       "test-stack",
		ComposeFilePath: "/path/to/docker-compose.yml",
		BackupFilePath:  "/path/to/docker-compose.yml.backup.20250109-120000",
		BackupTimestamp: time.Now(),
	}

	err = storage.SaveComposeBackup(ctx, backup)
	if err != nil {
		t.Fatalf("Failed to save compose backup: %v", err)
	}

	// Retrieve the backup
	retrieved, found, err := storage.GetComposeBackup(ctx, "test-op-003")
	if err != nil {
		t.Fatalf("Failed to get compose backup: %v", err)
	}
	if !found {
		t.Fatal("Expected to find saved compose backup")
	}

	// Verify fields
	if retrieved.OperationID != backup.OperationID {
		t.Errorf("Expected operation ID %s, got %s", backup.OperationID, retrieved.OperationID)
	}
	if retrieved.ComposeFilePath != backup.ComposeFilePath {
		t.Errorf("Expected compose file path %s, got %s", backup.ComposeFilePath, retrieved.ComposeFilePath)
	}
	if retrieved.BackupFilePath != backup.BackupFilePath {
		t.Errorf("Expected backup file path %s, got %s", backup.BackupFilePath, retrieved.BackupFilePath)
	}
}

// TestGetAndSetRollbackPolicy tests rollback policy CRUD operations
func TestGetAndSetRollbackPolicy(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Test global policy (created by migration)
	globalPolicy, found, err := storage.GetRollbackPolicy(ctx, "global", "")
	if err != nil {
		t.Fatalf("Failed to get global rollback policy: %v", err)
	}
	if !found {
		t.Fatal("Expected to find default global rollback policy")
	}
	if globalPolicy.AutoRollbackEnabled {
		t.Error("Expected default global policy to have auto-rollback disabled")
	}

	// Create container-specific policy
	containerPolicy := RollbackPolicy{
		EntityType:          "container",
		EntityID:            "test-container",
		AutoRollbackEnabled: true,
		HealthCheckRequired: true,
	}

	err = storage.SetRollbackPolicy(ctx, containerPolicy)
	if err != nil {
		t.Fatalf("Failed to set container rollback policy: %v", err)
	}

	// Retrieve container policy
	retrieved, found, err := storage.GetRollbackPolicy(ctx, "container", "test-container")
	if err != nil {
		t.Fatalf("Failed to get container rollback policy: %v", err)
	}
	if !found {
		t.Fatal("Expected to find container rollback policy")
	}
	if !retrieved.AutoRollbackEnabled {
		t.Error("Expected auto-rollback to be enabled for container policy")
	}

	// Create stack-specific policy
	stackPolicy := RollbackPolicy{
		EntityType:          "stack",
		EntityID:            "test-stack",
		AutoRollbackEnabled: true,
		HealthCheckRequired: false,
	}

	err = storage.SetRollbackPolicy(ctx, stackPolicy)
	if err != nil {
		t.Fatalf("Failed to set stack rollback policy: %v", err)
	}

	// Retrieve stack policy
	stackRetrieved, found, err := storage.GetRollbackPolicy(ctx, "stack", "test-stack")
	if err != nil {
		t.Fatalf("Failed to get stack rollback policy: %v", err)
	}
	if !found {
		t.Fatal("Expected to find stack rollback policy")
	}
	if !stackRetrieved.AutoRollbackEnabled {
		t.Error("Expected auto-rollback to be enabled for stack policy")
	}
}

// TestQueueAndDequeueUpdate tests queue operations
func TestQueueAndDequeueUpdate(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Queue an update
	queue := UpdateQueue{
		OperationID: "queued-op-001",
		StackName:   "test-stack",
		Containers:  []string{"container-1", "container-2"},
		Priority:    0,
		QueuedAt:    time.Now(),
	}

	err = storage.QueueUpdate(ctx, queue)
	if err != nil {
		t.Fatalf("Failed to queue update: %v", err)
	}

	// Verify queue entry exists
	queued, err := storage.GetQueuedUpdates(ctx)
	if err != nil {
		t.Fatalf("Failed to get queued updates: %v", err)
	}
	if len(queued) != 1 {
		t.Errorf("Expected 1 queued update, got %d", len(queued))
	}

	// Dequeue the update
	dequeued, found, err := storage.DequeueUpdate(ctx, "test-stack")
	if err != nil {
		t.Fatalf("Failed to dequeue update: %v", err)
	}
	if !found {
		t.Fatal("Expected to find queued update")
	}
	if dequeued.OperationID != "queued-op-001" {
		t.Errorf("Expected operation ID 'queued-op-001', got %s", dequeued.OperationID)
	}
	if len(dequeued.Containers) != 2 {
		t.Errorf("Expected 2 containers, got %d", len(dequeued.Containers))
	}

	// Verify queue is now empty
	queued, err = storage.GetQueuedUpdates(ctx)
	if err != nil {
		t.Fatalf("Failed to get queued updates: %v", err)
	}
	if len(queued) != 0 {
		t.Errorf("Expected empty queue after dequeue, got %d entries", len(queued))
	}
}

// TestQueuePersistenceAcrossRestart tests that queue persists across restarts
func TestQueuePersistenceAcrossRestart(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Create first storage instance and queue an update
	storage1, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	ctx := context.Background()
	queue := UpdateQueue{
		OperationID: "persistent-op-001",
		StackName:   "test-stack",
		Containers:  []string{"container-1"},
		Priority:    0,
		QueuedAt:    time.Now(),
	}

	err = storage1.QueueUpdate(ctx, queue)
	if err != nil {
		t.Fatalf("Failed to queue update: %v", err)
	}

	// Close the database
	storage1.Close()

	// Reopen the database
	storage2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer storage2.Close()

	// Verify queue entry still exists
	queued, err := storage2.GetQueuedUpdates(ctx)
	if err != nil {
		t.Fatalf("Failed to get queued updates: %v", err)
	}
	if len(queued) != 1 {
		t.Errorf("Expected 1 queued update after restart, got %d", len(queued))
	}
	if queued[0].OperationID != "persistent-op-001" {
		t.Errorf("Expected operation ID 'persistent-op-001', got %s", queued[0].OperationID)
	}
}

// TestDequeueUpdateWithNoQueue tests dequeue when queue is empty
func TestDequeueUpdateWithNoQueue(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Try to dequeue from empty queue
	_, found, err := storage.DequeueUpdate(ctx, "nonexistent-stack")
	if err != nil {
		t.Fatalf("Failed to dequeue update: %v", err)
	}
	if found {
		t.Error("Expected not to find update in empty queue")
	}
}
