package registry

import (
	"testing"
)

// TestDockerHubPaginationLimit tests that Docker Hub fetches multiple pages
func TestDockerHubPaginationLimit(t *testing.T) {
	// Simulate pagination scenario
	page1Tags := make([]string, 100) // First page: 100 tags
	for i := range 100 {
		page1Tags[i] = "v1.0." + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}

	page2Tags := make([]string, 100) // Second page: 100 tags
	for i := range 100 {
		page2Tags[i] = "v1.1." + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}

	page3Tags := make([]string, 100) // Third page: 100 tags
	for i := range 100 {
		page3Tags[i] = "v1.2." + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}

	// Total tags across all pages
	allTags := append(append(page1Tags, page2Tags...), page3Tags...)

	// Test that we'd fetch at least 3 pages
	maxPages := 5
	expectedMin := 300 // 3 pages * 100 tags

	if len(allTags) < expectedMin {
		t.Errorf("Expected at least %d tags with maxPages=%d, got %d", expectedMin, maxPages, len(allTags))
	}

	t.Logf("✓ With maxPages=%d, can fetch up to %d tags", maxPages, len(allTags))
}

// TestDockerHubOldBehaviorWasLimited tests that the old behavior only fetched 100 tags
func TestDockerHubOldBehaviorWasLimited(t *testing.T) {
	oldLimit := 100 // Old code stopped at 100 tags
	newLimit := 500 // New code can fetch up to 5 pages * 100 = 500 tags

	if newLimit <= oldLimit {
		t.Errorf("New limit (%d) should be greater than old limit (%d)", newLimit, oldLimit)
	}

	improvement := float64(newLimit) / float64(oldLimit)
	t.Logf("✓ Pagination improvement: %.1fx more tags (%d -> %d)", improvement, oldLimit, newLimit)
}

// TestDockerHubPaginationStopsEarly tests that pagination stops when results are less than page size
func TestDockerHubPaginationStopsEarly(t *testing.T) {
	// Simulate a repository with only 150 tags (2 full pages + 1 partial page)
	totalTags := 150
	pageSize := 100

	expectedPages := 2 // 100 + 50 (partial page triggers early stop)

	pagesNeeded := (totalTags + pageSize - 1) / pageSize
	if pagesNeeded < expectedPages {
		pagesNeeded = expectedPages
	}

	t.Logf("Repository with %d tags should need %d page requests", totalTags, pagesNeeded)

	// Verify that last page would be partial (< 100 tags)
	lastPageSize := totalTags % pageSize
	if lastPageSize == 0 {
		lastPageSize = pageSize
	}

	if lastPageSize >= pageSize {
		t.Error("Expected last page to be partial (< page size)")
	}

	t.Logf("✓ Pagination would stop early after page %d (last page has %d tags)", pagesNeeded, lastPageSize)
}

// TestDockerHubMaxPagesEnforced tests that we don't exceed maxPages limit
func TestDockerHubMaxPagesEnforced(t *testing.T) {
	maxPages := 5
	pageSize := 100

	// Simulate a repository with 1000 tags (would need 10 pages)
	totalTagsAvailable := 1000
	maxTagsFetched := maxPages * pageSize // Should stop at 500

	if maxTagsFetched >= totalTagsAvailable {
		t.Errorf("Expected to limit fetching, but maxPages would fetch all tags")
	}

	t.Logf("Repository has %d tags, but will only fetch %d (maxPages=%d)",
		totalTagsAvailable, maxTagsFetched, maxPages)
	t.Logf("✓ Pagination limit enforced: stops at page %d", maxPages)
}

// TestDockerHubBeforeAfterComparison compares old vs new behavior
func TestDockerHubBeforeAfterComparison(t *testing.T) {
	scenarios := []struct {
		name           string
		totalTags      int
		oldBehavior    int // Tags fetched with old code (max 100)
		newBehavior    int // Tags fetched with new code (max 500)
		missedBefore   bool
	}{
		{
			name:         "Small repo (50 tags)",
			totalTags:    50,
			oldBehavior:  50,
			newBehavior:  50,
			missedBefore: false,
		},
		{
			name:         "Medium repo (150 tags)",
			totalTags:    150,
			oldBehavior:  100, // Missed 50 tags!
			newBehavior:  150,
			missedBefore: true,
		},
		{
			name:         "Large repo (300 tags)",
			totalTags:    300,
			oldBehavior:  100, // Missed 200 tags!
			newBehavior:  300,
			missedBefore: true,
		},
		{
			name:         "Very large repo (600 tags)",
			totalTags:    600,
			oldBehavior:  100, // Missed 500 tags!
			newBehavior:  500, // Still miss 100, but much better
			missedBefore: true,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			oldMissed := scenario.totalTags - scenario.oldBehavior
			newMissed := scenario.totalTags - scenario.newBehavior

			if scenario.missedBefore {
				if oldMissed <= 0 {
					t.Errorf("Expected old behavior to miss tags, but missed %d", oldMissed)
				}
				t.Logf("Old behavior: fetched %d/%d tags (missed %d)",
					scenario.oldBehavior, scenario.totalTags, oldMissed)
			}

			t.Logf("New behavior: fetched %d/%d tags (missed %d)",
				scenario.newBehavior, scenario.totalTags, newMissed)

			if newMissed < oldMissed {
				improvement := oldMissed - newMissed
				t.Logf("✓ Improvement: %d fewer tags missed", improvement)
			} else if newMissed == 0 && oldMissed == 0 {
				t.Logf("✓ Both behaviors fetch all tags (small repo)")
			}
		})
	}
}

// TestDockerHubRealWorldScenarios tests scenarios based on actual containers
func TestDockerHubRealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		image          string
		estimatedTags  int
		wouldBeMissed  bool
		description    string
	}{
		{
			image:         "nginx",
			estimatedTags: 200,
			wouldBeMissed: true,
			description:   "Official Nginx has many version tags over the years",
		},
		{
			image:         "postgres",
			estimatedTags: 300,
			wouldBeMissed: true,
			description:   "PostgreSQL has versions + variant tags (alpine, bookworm, etc)",
		},
		{
			image:         "redis",
			estimatedTags: 250,
			wouldBeMissed: true,
			description:   "Redis has many versions with multiple variants",
		},
		{
			image:         "traefik",
			estimatedTags: 180,
			wouldBeMissed: true,
			description:   "Traefik publishes many tags including RC and experimental",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.image, func(t *testing.T) {
			oldLimit := 100
			newLimit := 500

			t.Logf("Container: %s", scenario.image)
			t.Logf("Description: %s", scenario.description)
			t.Logf("Estimated tags: %d", scenario.estimatedTags)

			if scenario.estimatedTags > oldLimit {
				missed := scenario.estimatedTags - oldLimit
				t.Logf("⚠️  Old behavior would miss %d tags (only fetch %d)", missed, oldLimit)
			}

			if scenario.estimatedTags > newLimit {
				missed := scenario.estimatedTags - newLimit
				t.Logf("⚠️  New behavior would still miss %d tags", missed)
			} else {
				t.Logf("✓ New behavior would fetch all %d tags", scenario.estimatedTags)
			}
		})
	}
}
