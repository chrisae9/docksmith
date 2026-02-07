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

// TestStopOperationType tests that 'stop' operation type is accepted by the schema
func TestStopOperationType(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create a stop operation
	op := UpdateOperation{
		OperationID:   "test-stop-001",
		ContainerID:   "container-123",
		ContainerName: "test-container",
		StackName:     "test-stack",
		OperationType: "stop",
		Status:        "complete",
		StartedAt:     &now,
		CompletedAt:   &now,
	}

	// Save should succeed with the new 'stop' operation type
	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save stop operation: %v", err)
	}

	// Retrieve and verify
	retrieved, found, err := storage.GetUpdateOperation(ctx, "test-stop-001")
	if err != nil {
		t.Fatalf("Failed to get stop operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find saved stop operation")
	}
	if retrieved.OperationType != "stop" {
		t.Errorf("Expected operation type 'stop', got %s", retrieved.OperationType)
	}
	if retrieved.ContainerName != "test-container" {
		t.Errorf("Expected container name 'test-container', got %s", retrieved.ContainerName)
	}
	if retrieved.Status != "complete" {
		t.Errorf("Expected status 'complete', got %s", retrieved.Status)
	}
}

// TestRemoveOperationType tests that 'remove' operation type is accepted by the schema
func TestRemoveOperationType(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create a remove operation
	op := UpdateOperation{
		OperationID:   "test-remove-001",
		ContainerID:   "container-456",
		ContainerName: "removed-container",
		StackName:     "test-stack",
		OperationType: "remove",
		Status:        "complete",
		StartedAt:     &now,
		CompletedAt:   &now,
	}

	// Save should succeed with the new 'remove' operation type
	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save remove operation: %v", err)
	}

	// Retrieve and verify
	retrieved, found, err := storage.GetUpdateOperation(ctx, "test-remove-001")
	if err != nil {
		t.Fatalf("Failed to get remove operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find saved remove operation")
	}
	if retrieved.OperationType != "remove" {
		t.Errorf("Expected operation type 'remove', got %s", retrieved.OperationType)
	}
	if retrieved.ContainerName != "removed-container" {
		t.Errorf("Expected container name 'removed-container', got %s", retrieved.ContainerName)
	}
}

// TestStopAndRemoveOperationsInHistory tests that stop and remove operations appear in history queries
func TestStopAndRemoveOperationsInHistory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create operations of different types
	operations := []UpdateOperation{
		{
			OperationID:   "op-single-001",
			ContainerName: "container-1",
			OperationType: "single",
			Status:        "complete",
			StartedAt:     &now,
			CompletedAt:   &now,
		},
		{
			OperationID:   "op-stop-001",
			ContainerName: "container-2",
			OperationType: "stop",
			Status:        "complete",
			StartedAt:     &now,
			CompletedAt:   &now,
		},
		{
			OperationID:   "op-remove-001",
			ContainerName: "container-3",
			OperationType: "remove",
			Status:        "complete",
			StartedAt:     &now,
			CompletedAt:   &now,
		},
		{
			OperationID:   "op-restart-001",
			ContainerName: "container-4",
			OperationType: "restart",
			Status:        "complete",
			StartedAt:     &now,
			CompletedAt:   &now,
		},
	}

	for _, op := range operations {
		err = storage.SaveUpdateOperation(ctx, op)
		if err != nil {
			t.Fatalf("Failed to save operation %s: %v", op.OperationID, err)
		}
	}

	// Query all completed operations
	completed, err := storage.GetUpdateOperations(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get update operations: %v", err)
	}

	if len(completed) != 4 {
		t.Errorf("Expected 4 operations in history, got %d", len(completed))
	}

	// Verify all operation types are present
	typeCount := make(map[string]int)
	for _, op := range completed {
		typeCount[op.OperationType]++
	}

	if typeCount["single"] != 1 {
		t.Errorf("Expected 1 'single' operation, got %d", typeCount["single"])
	}
	if typeCount["stop"] != 1 {
		t.Errorf("Expected 1 'stop' operation, got %d", typeCount["stop"])
	}
	if typeCount["remove"] != 1 {
		t.Errorf("Expected 1 'remove' operation, got %d", typeCount["remove"])
	}
	if typeCount["restart"] != 1 {
		t.Errorf("Expected 1 'restart' operation, got %d", typeCount["restart"])
	}
}

// TestStopOperationFailure tests recording a failed stop operation
func TestStopOperationFailure(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create a stop operation in progress
	op := UpdateOperation{
		OperationID:   "test-stop-fail-001",
		ContainerID:   "container-789",
		ContainerName: "failing-container",
		OperationType: "stop",
		Status:        "in_progress",
		StartedAt:     &now,
	}

	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save stop operation: %v", err)
	}

	// Update status to failed with error message
	err = storage.UpdateOperationStatus(ctx, "test-stop-fail-001", "failed", "container not running")
	if err != nil {
		t.Fatalf("Failed to update operation status: %v", err)
	}

	// Verify the failure was recorded
	retrieved, found, err := storage.GetUpdateOperation(ctx, "test-stop-fail-001")
	if err != nil {
		t.Fatalf("Failed to get stop operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find stop operation")
	}
	if retrieved.Status != "failed" {
		t.Errorf("Expected status 'failed', got %s", retrieved.Status)
	}
	if retrieved.ErrorMessage != "container not running" {
		t.Errorf("Expected error message 'container not running', got %s", retrieved.ErrorMessage)
	}
}

// TestBatchDetailsWithChangeType tests that BatchContainerDetail with *int ChangeType
// correctly round-trips through the database (including zero value vs nil).
func TestBatchDetailsWithChangeType(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Create helper to make *int
	intPtr := func(v int) *int { return &v }

	// Operation with ChangeType=0 (rebuild), resolved versions, and nil ChangeType
	op := UpdateOperation{
		OperationID:   "batch-meta-001",
		ContainerName: "test-batch",
		StackName:     "test-stack",
		OperationType: "batch",
		Status:        "complete",
		StartedAt:     &now,
		BatchDetails: []BatchContainerDetail{
			{
				ContainerName:      "container-a",
				StackName:          "stack-1",
				OldVersion:         "v1.0.0",
				NewVersion:         "v1.1.0",
				ChangeType:         intPtr(1), // PATCH
				OldResolvedVersion: "v1.0.0",
				NewResolvedVersion: "v1.1.0",
			},
			{
				ContainerName:      "container-b",
				StackName:          "stack-1",
				OldVersion:         "master-omnibus",
				NewVersion:         "master-omnibus",
				ChangeType:         intPtr(0), // REBUILD (zero value, must be preserved)
				OldResolvedVersion: "v0.8.0-omnibus",
				NewResolvedVersion: "v0.8.1-omnibus",
			},
			{
				ContainerName: "container-c",
				StackName:     "stack-1",
				OldVersion:    "latest",
				NewVersion:    "latest",
				// ChangeType is nil — legacy behavior, should not appear in JSON
			},
		},
	}

	err = storage.SaveUpdateOperation(ctx, op)
	if err != nil {
		t.Fatalf("Failed to save operation: %v", err)
	}

	// Retrieve and verify
	retrieved, found, err := storage.GetUpdateOperation(ctx, "batch-meta-001")
	if err != nil {
		t.Fatalf("Failed to get operation: %v", err)
	}
	if !found {
		t.Fatal("Expected to find operation")
	}
	if len(retrieved.BatchDetails) != 3 {
		t.Fatalf("Expected 3 batch details, got %d", len(retrieved.BatchDetails))
	}

	// Verify container-a: PATCH with resolved versions
	detailA := retrieved.BatchDetails[0]
	if detailA.ContainerName != "container-a" {
		t.Errorf("Expected container-a, got %s", detailA.ContainerName)
	}
	if detailA.ChangeType == nil || *detailA.ChangeType != 1 {
		t.Errorf("Expected ChangeType=1 (PATCH), got %v", detailA.ChangeType)
	}
	if detailA.OldResolvedVersion != "v1.0.0" {
		t.Errorf("Expected OldResolvedVersion=v1.0.0, got %s", detailA.OldResolvedVersion)
	}
	if detailA.NewResolvedVersion != "v1.1.0" {
		t.Errorf("Expected NewResolvedVersion=v1.1.0, got %s", detailA.NewResolvedVersion)
	}

	// Verify container-b: REBUILD (ChangeType=0, must NOT be nil)
	detailB := retrieved.BatchDetails[1]
	if detailB.ChangeType == nil {
		t.Error("Expected ChangeType=0 (REBUILD) to be non-nil, but got nil")
	} else if *detailB.ChangeType != 0 {
		t.Errorf("Expected ChangeType=0, got %d", *detailB.ChangeType)
	}
	if detailB.OldResolvedVersion != "v0.8.0-omnibus" {
		t.Errorf("Expected OldResolvedVersion=v0.8.0-omnibus, got %s", detailB.OldResolvedVersion)
	}
	if detailB.NewResolvedVersion != "v0.8.1-omnibus" {
		t.Errorf("Expected NewResolvedVersion=v0.8.1-omnibus, got %s", detailB.NewResolvedVersion)
	}

	// Verify container-c: nil ChangeType (legacy)
	detailC := retrieved.BatchDetails[2]
	if detailC.ChangeType != nil {
		t.Errorf("Expected nil ChangeType for legacy container, got %d", *detailC.ChangeType)
	}
	if detailC.OldResolvedVersion != "" {
		t.Errorf("Expected empty OldResolvedVersion, got %s", detailC.OldResolvedVersion)
	}
}

// TestBatchGroupIDLinking tests that operations with the same batch_group_id
// can be queried together.
func TestBatchGroupIDLinking(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()
	groupID := "group-abc-123"

	// Create two operations in the same batch group
	op1 := UpdateOperation{
		OperationID:   "group-op-001",
		ContainerName: "container-1",
		StackName:     "stack-a",
		OperationType: "single",
		Status:        "complete",
		OldVersion:    "1.0",
		NewVersion:    "2.0",
		BatchGroupID:  groupID,
		StartedAt:     &now,
	}
	op2 := UpdateOperation{
		OperationID:   "group-op-002",
		ContainerName: "container-2",
		StackName:     "stack-b",
		OperationType: "batch",
		Status:        "complete",
		BatchGroupID:  groupID,
		StartedAt:     &now,
		BatchDetails: []BatchContainerDetail{
			{ContainerName: "container-2", OldVersion: "v1", NewVersion: "v2"},
			{ContainerName: "container-3", OldVersion: "v3", NewVersion: "v4"},
		},
	}
	// Operation in a different group
	op3 := UpdateOperation{
		OperationID:   "other-op-001",
		ContainerName: "container-x",
		StackName:     "stack-c",
		OperationType: "single",
		Status:        "complete",
		BatchGroupID:  "group-other-456",
		StartedAt:     &now,
	}

	for _, op := range []UpdateOperation{op1, op2, op3} {
		if err := storage.SaveUpdateOperation(ctx, op); err != nil {
			t.Fatalf("Failed to save operation %s: %v", op.OperationID, err)
		}
	}

	// Query by batch group ID
	grouped, err := storage.GetUpdateOperationsByBatchGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("Failed to get operations by batch group: %v", err)
	}

	if len(grouped) != 2 {
		t.Fatalf("Expected 2 operations in group, got %d", len(grouped))
	}

	// Verify both operations are present
	ids := map[string]bool{}
	for _, op := range grouped {
		ids[op.OperationID] = true
	}
	if !ids["group-op-001"] || !ids["group-op-002"] {
		t.Errorf("Expected group-op-001 and group-op-002, got %v", ids)
	}

	// Verify batch details preserved in group query
	for _, op := range grouped {
		if op.OperationID == "group-op-002" && len(op.BatchDetails) != 2 {
			t.Errorf("Expected 2 batch details for group-op-002, got %d", len(op.BatchDetails))
		}
	}
}

// TestRollbackOccurredTracking tests that rollback_occurred is correctly saved and retrieved.
func TestRollbackOccurredTracking(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	op := UpdateOperation{
		OperationID:      "rollback-track-001",
		ContainerName:    "test-container",
		StackName:        "test-stack",
		OperationType:    "batch",
		Status:           "complete",
		OldVersion:       "1.0",
		NewVersion:       "2.0",
		StartedAt:        &now,
		RollbackOccurred: false,
		BatchDetails: []BatchContainerDetail{
			{ContainerName: "test-container", OldVersion: "1.0", NewVersion: "2.0"},
		},
	}

	if err := storage.SaveUpdateOperation(ctx, op); err != nil {
		t.Fatalf("Failed to save operation: %v", err)
	}

	// Verify initially not rolled back
	retrieved, _, _ := storage.GetUpdateOperation(ctx, "rollback-track-001")
	if retrieved.RollbackOccurred {
		t.Error("Expected RollbackOccurred=false initially")
	}

	// Mark as rolled back
	retrieved.RollbackOccurred = true
	if err := storage.SaveUpdateOperation(ctx, retrieved); err != nil {
		t.Fatalf("Failed to update operation: %v", err)
	}

	// Verify it persists
	final, _, _ := storage.GetUpdateOperation(ctx, "rollback-track-001")
	if !final.RollbackOccurred {
		t.Error("Expected RollbackOccurred=true after update")
	}
}
