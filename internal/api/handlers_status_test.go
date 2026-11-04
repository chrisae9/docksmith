package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCheckRunner struct {
	result     *update.DiscoveryResult
	lastCheck  time.Time
	lastRun    time.Time
	checking   bool
	triggered  bool
	forceCalls []bool
	cleared    int
}

func (f *fakeCheckRunner) GetCachedResults() (*update.DiscoveryResult, time.Time, time.Time, bool) {
	return f.result, f.lastCheck, f.lastRun, f.checking
}
func (f *fakeCheckRunner) TriggerCheck() {}
func (f *fakeCheckRunner) TriggerCheckClient(force bool) bool {
	f.forceCalls = append(f.forceCalls, force)
	return f.triggered
}
func (f *fakeCheckRunner) MarkCacheCleared() { f.cleared++ }
func (f *fakeCheckRunner) Start()            {}
func (f *fakeCheckRunner) Stop()             {}

func TestBuildStatusResult_SlimDropsOnlyTagsAndStacks(t *testing.T) {
	cached := &update.DiscoveryResult{
		Containers: []update.ContainerInfo{{
			ContainerUpdate: update.ContainerUpdate{
				ContainerName: "nginx", Status: update.UpToDate,
				AvailableTags: []string{"1.0", "2.0"},
			},
			ID:     "abc",
			Labels: map[string]string{"docksmith.ignore": "true"},
		}},
		Stacks:       map[string]*update.Stack{"app": {Name: "app"}},
		UpdateOrder:  []string{"nginx"},
		TotalChecked: 1, UpToDate: 1,
	}
	lastCheck := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lastRun := lastCheck.Add(time.Minute)

	got := buildStatusResult(cached, lastCheck, lastRun, true, time.Minute, time.Hour, true)

	assert.Nil(t, got.Stacks)
	require.Len(t, got.Containers, 1)
	assert.Nil(t, got.Containers[0].AvailableTags)
	assert.Equal(t, map[string]string{"docksmith.ignore": "true"}, got.Containers[0].Labels)
	assert.Equal(t, []string{"nginx"}, got.UpdateOrder)
	assert.Equal(t, 1, got.TotalChecked)
	assert.Equal(t, lastCheck.Format(time.RFC3339), got.LastCacheRefresh)
	assert.Equal(t, lastRun.Format(time.RFC3339), got.LastBackgroundRun)
	assert.True(t, got.Checking)
	assert.Equal(t, "1m0s", got.CheckInterval)
	assert.Equal(t, "1h0m0s", got.CacheTTL)

	body, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "available_tags")
	assert.NotContains(t, string(body), `"stacks"`)
}

func TestBuildStatusResult_FullKeepsTagsAndStacks(t *testing.T) {
	cached := &update.DiscoveryResult{
		Containers: []update.ContainerInfo{{ContainerUpdate: update.ContainerUpdate{
			ContainerName: "nginx", AvailableTags: []string{"1.0"},
		}}},
		Stacks: map[string]*update.Stack{"app": {Name: "app"}},
	}
	got := buildStatusResult(cached, time.Time{}, time.Time{}, false, time.Minute, time.Hour, false)
	assert.NotNil(t, got.Stacks)
	assert.Len(t, got.Containers[0].AvailableTags, 1)
}

func TestHandleTriggerCheck_ReportsTriggeredAndForce(t *testing.T) {
	for _, tc := range []struct {
		query         string
		wantForce     bool
		wantTriggered bool
	}{
		{"", false, false},
		{"?force=true", true, true},
	} {
		fake := &fakeCheckRunner{triggered: tc.wantTriggered, checking: true}
		s := &Server{backgroundChecker: fake}
		w := httptest.NewRecorder()
		s.handleTriggerCheck(w, httptest.NewRequest("POST", "/api/trigger-check"+tc.query, nil))

		var resp struct {
			Success bool `json:"success"`
			Data    struct {
				Triggered         bool   `json:"triggered"`
				Checking          bool   `json:"checking"`
				LastBackgroundRun string `json:"last_background_run"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.True(t, resp.Success)
		assert.Equal(t, tc.wantTriggered, resp.Data.Triggered)
		assert.Equal(t, tc.wantForce, fake.forceCalls[0])
	}
}

func TestHandleGetStatus_IsSlimAndHandleCheckIsFull(t *testing.T) {
	cached := &update.DiscoveryResult{
		Containers: []update.ContainerInfo{{ContainerUpdate: update.ContainerUpdate{
			ContainerName: "nginx", AvailableTags: []string{"1.0"},
		}}},
		Stacks: map[string]*update.Stack{"app": {Name: "app"}},
	}
	fake := &fakeCheckRunner{result: cached}
	s := &Server{backgroundChecker: fake, discoveryOrchestrator: update.NewOrchestrator(nil, nil)}

	w := httptest.NewRecorder()
	s.handleGetStatus(w, httptest.NewRequest("GET", "/api/status", nil))
	assert.NotContains(t, w.Body.String(), "available_tags")
	assert.NotContains(t, w.Body.String(), `"stacks"`)

	w = httptest.NewRecorder()
	s.handleCheck(w, httptest.NewRequest("GET", "/api/check", nil))
	assert.Contains(t, w.Body.String(), "available_tags")
	assert.Contains(t, w.Body.String(), `"stacks"`)
	assert.Equal(t, 1, fake.cleared)
}
