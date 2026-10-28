package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
)

// TestLabelOperationOnSelfContainerDefersRestartToHelper verifies that pinning
// docksmith itself does not run docker compose from inside the container being
// restarted. The operation is parked as pending_restart and handed to a helper
// container that survives docksmith's own restart.
func TestLabelOperationOnSelfContainerDefersRestartToHelper(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yaml")
	content := `services:
  docksmith:
    image: ghcr.io/chrisae9/docksmith:latest
    container_name: docksmith
`
	if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewMockStorage()
	store.AddOperation(storage.UpdateOperation{
		OperationID:   "op-self-1",
		ContainerName: "docksmith",
		OperationType: "label_change",
		Status:        "in_progress",
	})

	s := &Server{
		storageService: store,
		pathTranslator: docker.NewPathTranslator(nil),
	}

	type helperCall struct {
		image   string
		compose string
		service string
	}
	calls := make(chan helperCall, 1)
	origRecreate := selfRecreate
	t.Cleanup(func() { selfRecreate = origRecreate })
	selfRecreate = func(_ context.Context, image, hostComposePath, serviceName string) error {
		calls <- helperCall{image: image, compose: hostComposePath, service: serviceName}
		return nil
	}

	container := &docker.Container{
		ID:    "docksmith-container-id",
		Name:  "docksmith",
		Image: "ghcr.io/chrisae9/docksmith:latest",
		Labels: map[string]string{
			ComposeConfigFilesLabel: composePath,
			ComposeServiceLabel:     "docksmith",
			ComposeProjectLabel:     "engager",
		},
	}

	modify := func(service *compose.Service) (map[string]string, []string, error) {
		if err := service.SetLabel(scripts.VersionPinPatchLabel, "true"); err != nil {
			return nil, nil, err
		}
		return map[string]string{scripts.VersionPinPatchLabel: "true"}, nil, nil
	}

	s.executeLabelOperationAsync(context.Background(), "op-self-1", container,
		labelOperationConfig{containerName: "docksmith", operationType: "set"},
		composePath, "docksmith", modify)

	select {
	case call := <-calls:
		if call.image != container.Image {
			t.Errorf("helper image = %q, want %q", call.image, container.Image)
		}
		if call.compose != composePath {
			t.Errorf("helper compose path = %q, want %q", call.compose, composePath)
		}
		if call.service != "docksmith" {
			t.Errorf("helper service = %q, want docksmith", call.service)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("self-recreate helper was never started")
	}

	op, found, err := store.GetUpdateOperation(context.Background(), "op-self-1")
	if err != nil || !found {
		t.Fatalf("operation lookup failed: found=%v err=%v", found, err)
	}
	if op.Status != "pending_restart" {
		t.Errorf("operation status = %q, want pending_restart", op.Status)
	}
	if !strings.Contains(op.NewVersion, scripts.VersionPinPatchLabel) {
		t.Errorf("operation NewVersion = %q, want it to record %s", op.NewVersion, scripts.VersionPinPatchLabel)
	}

	saved, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), scripts.VersionPinPatchLabel+"=true") {
		t.Errorf("compose file was not updated with the pin label:\n%s", saved)
	}
}
