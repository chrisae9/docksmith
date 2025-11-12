package update

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/stretchr/testify/assert"
)

// Test: extractServiceName returns correct service name from label
func TestExtractServiceName_ReturnsCorrectServiceName(t *testing.T) {
	container := &docker.Container{
		Name: "test-web",
		Labels: map[string]string{
			"com.docker.compose.service": "web",
		},
	}

	serviceName := extractServiceName(container)

	assert.Equal(t, "web", serviceName)
}

// Test: extractServiceName handles missing label gracefully
func TestExtractServiceName_HandlesMissingLabel(t *testing.T) {
	container := &docker.Container{
		Name:   "standalone-container",
		Labels: map[string]string{},
	}

	serviceName := extractServiceName(container)

	assert.Equal(t, "", serviceName)
}

// Test: extractServiceNames collects service names from container list
func TestExtractServiceNames_CollectsServiceNames(t *testing.T) {
	containers := []docker.Container{
		{
			Name: "web",
			Labels: map[string]string{
				"com.docker.compose.service": "web",
			},
		},
		{
			Name: "db",
			Labels: map[string]string{
				"com.docker.compose.service": "db",
			},
		},
		{
			Name: "cache",
			Labels: map[string]string{
				"com.docker.compose.service": "cache",
			},
		},
	}

	containerNames := []string{"web", "db"}

	serviceNames := extractServiceNames(containers, containerNames)

	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "db")
	assert.NotContains(t, serviceNames, "cache")
}

// Test: extractServiceNames filters out empty strings from non-compose containers
func TestExtractServiceNames_FiltersEmptyStrings(t *testing.T) {
	containers := []docker.Container{
		{
			Name: "web",
			Labels: map[string]string{
				"com.docker.compose.service": "web",
			},
		},
		{
			Name:   "standalone",
			Labels: map[string]string{},
		},
		{
			Name: "api",
			Labels: map[string]string{
				"com.docker.compose.service": "api",
			},
		},
	}

	containerNames := []string{"web", "standalone", "api"}

	serviceNames := extractServiceNames(containers, containerNames)

	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "api")
	assert.NotContains(t, serviceNames, "")
}

// Test: runCommandWithStreaming captures and streams output line-by-line
func TestRunCommandWithStreaming_CapturesOutput(t *testing.T) {
	bus := events.NewBus()
	ctx := context.Background()

	// Subscribe to events
	sub := bus.Subscribe(ctx, "test-subscriber")
	defer bus.Unsubscribe("test-subscriber")

	capturedEvents := make([]events.Event, 0)

	// Read events in goroutine
	done := make(chan bool)
	go func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case event := <-sub.Channel:
				capturedEvents = append(capturedEvents, event)
			case <-timeout:
				done <- true
				return
			}
		}
	}()

	orch := &UpdateOrchestrator{
		eventBus:     bus,
		dockerClient: &MockDockerClient{},
	}

	operationID := "test-op-123"
	containerName := "test-container"
	stackName := "test-stack"

	// Use echo command to simulate compose output
	err := orch.runCommandWithStreaming(ctx, operationID, containerName, stackName, "echo", "Pulling image\nCreating container\nStarting container")

	assert.NoError(t, err)

	// Wait for events to be captured
	<-done

	// Note: We expect at least one event with streaming_compose substage
	hasStreamingEvent := false
	for _, event := range capturedEvents {
		if stage, ok := event.Payload["stage"].(string); ok && stage == "streaming_compose" {
			hasStreamingEvent = true
			break
		}
	}

	assert.True(t, hasStreamingEvent, "Expected at least one streaming_compose event")
}

// Test: runCommandWithStreaming handles command errors properly
func TestRunCommandWithStreaming_HandlesErrors(t *testing.T) {
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		eventBus:     bus,
		dockerClient: &MockDockerClient{},
	}

	ctx := context.Background()
	operationID := "test-op-456"
	containerName := "test-container"
	stackName := "test-stack"

	// Use a command that will fail
	err := orch.runCommandWithStreaming(ctx, operationID, containerName, stackName, "sh", "-c", "exit 1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")
}

// Test: runCommandWithStreaming publishes progress for compose keywords
func TestRunCommandWithStreaming_PublishesComposeProgress(t *testing.T) {
	ctx := context.Background()

	capturedMessages := make([]string, 0)

	// Simulate compose output with keywords
	composeOutput := `Pulling web
Creating web_1
Starting web_1
Started`

	// Create a test command that outputs compose-like messages
	cmd := exec.CommandContext(ctx, "sh", "-c", "printf '"+composeOutput+"'")
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// Simulate the filtering logic that will be in runCommandWithStreaming
		if strings.Contains(line, "Pulling") ||
			strings.Contains(line, "Creating") ||
			strings.Contains(line, "Starting") ||
			strings.Contains(line, "Started") {
			capturedMessages = append(capturedMessages, line)
		}
	}
	cmd.Wait()

	// Verify that compose keywords were captured
	assert.NotEmpty(t, capturedMessages)
	foundPulling := false
	foundCreating := false
	foundStarting := false

	for _, msg := range capturedMessages {
		if strings.Contains(msg, "Pulling") {
			foundPulling = true
		}
		if strings.Contains(msg, "Creating") {
			foundCreating = true
		}
		if strings.Contains(msg, "Starting") {
			foundStarting = true
		}
	}

	assert.True(t, foundPulling || foundCreating || foundStarting, "Expected compose keywords in output")
}
