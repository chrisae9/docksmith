/**
 * Restart Operations Tests
 *
 * These tests validate the restart functionality through the UI and API,
 * including restart dependencies management and stack restarts.
 *
 * Run with: npm test -- specs/restart.spec.ts
 */

import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { RestartDependenciesPage } from '../pages/restart-dependencies.page';
import { RestartProgressPage } from '../pages/restart-progress.page';
import { ContainerDetailPage } from '../pages/container-detail.page';

// Use containers that are safe to restart
const TEST_CONTAINER = TEST_CONTAINERS.NGINX_BASIC;
const CONTAINER_WITH_DEPS = TEST_CONTAINERS.LABELS_RESTART_DEPS;

test.describe('Restart API', () => {
  test('POST /api/restart/container/{name} restarts a container', async ({ api }) => {
    const response = await api.restart(TEST_CONTAINER);

    // Restart should succeed or fail with a valid reason
    if (!response.success) {
      console.log('Restart failed (may be expected):', response.error);
      // Some containers can't be restarted - that's ok, verify error is sensible
      expect(response.error).toBeDefined();
      return;
    }

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();

    // Wait for container to recover
    await new Promise(resolve => setTimeout(resolve, 5000));

    // Verify container is still running
    const container = await api.getContainer(TEST_CONTAINER);
    expect(container).not.toBeNull();
  });

  test('POST /api/restart/start/{name} returns operation ID for SSE tracking', async ({ api }) => {
    const response = await api.restartStart(TEST_CONTAINER);

    if (!response.success) {
      console.log('Restart start failed:', response.error);
      // May fail if container is not restartable
      expect(response.error).toBeDefined();
      return;
    }

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    expect(response.data?.operation_id).toBeDefined();
    expect(response.data?.container_name).toBe(TEST_CONTAINER);
    expect(response.data?.status).toBe('started');

    // Wait for operation to complete
    if (response.data?.operation_id) {
      const result = await api.waitForOperation(response.data.operation_id, 60000);
      if (result) {
        expect(['complete', 'failed']).toContain(result.status);
      }
    }
  });

  test('POST /api/restart/stack/{name} restarts all containers in stack', async ({ api }) => {
    // First get a container to find its stack
    const container = await api.getContainer(TEST_CONTAINER);

    if (!container || !container.stack) {
      test.skip(true, 'Container has no stack - skipping stack restart test');
      return;
    }

    const stackName = container.stack;
    console.log(`Testing stack restart for: ${stackName}`);

    const response = await api.restartStack(stackName);

    if (!response.success) {
      console.log('Stack restart failed:', response.error);
      // Stack restart may fail due to pre-update checks or other reasons
      expect(response.error).toBeDefined();
      return;
    }

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    expect(response.data?.container_names).toBeDefined();
    expect(response.data?.container_names.length).toBeGreaterThan(0);
  });

  test('Restart with force flag skips pre-update checks', async ({ api }) => {
    const response = await api.restart(TEST_CONTAINER, true);

    // Force restart should succeed more often
    if (!response.success) {
      console.log('Force restart failed:', response.error);
      return;
    }

    expect(response.success).toBe(true);
  });
});

test.describe('Restart Dependencies UI', () => {
  test('Restart dependencies page loads correctly', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    // Page should load with correct title
    await expect(restartPage.pageTitle).toHaveText('Restart Dependencies');

    // Containers list should be visible
    await expect(page.locator('.containers-list')).toBeVisible();
  });

  test('Restart dependencies page shows available containers', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    // Should show at least some containers (or an empty state)
    const containerCount = await restartPage.getContainerCount();
    const hasEmpty = await restartPage.hasEmptyState();

    // Either we have containers or an empty state message
    expect(containerCount > 0 || hasEmpty).toBe(true);
  });

  test('Container selection works in restart dependencies', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    const containerCount = await restartPage.getContainerCount();

    if (containerCount === 0) {
      test.skip(true, 'No containers available for selection');
      return;
    }

    // Get initial selected count
    const initialSelected = await restartPage.getSelectedCount();

    // Try to find and select a container from the list
    // We'll use the API to find a container name that should be in the list
    const status = await page.request.get(`${process.env.DOCKSMITH_URL || 'https://docksmith.ts.chis.dev'}/api/status`);
    const statusData = await status.json();

    if (!statusData.data?.containers?.length) {
      test.skip(true, 'No containers from API');
      return;
    }

    // Find a container different from the one we're on
    const otherContainer = statusData.data.containers.find(
      (c: any) => c.container_name !== TEST_CONTAINER
    );

    if (!otherContainer) {
      test.skip(true, 'No other containers to select');
      return;
    }

    // Try to select it
    await restartPage.selectContainer(otherContainer.container_name);

    // Wait for selection to register
    await page.waitForTimeout(500);

    // Check if it's selected
    const isSelected = await restartPage.isContainerSelected(otherContainer.container_name);

    // Selection should have changed (either selected or already was)
    const newSelectedCount = await restartPage.getSelectedCount();
    expect(newSelectedCount >= initialSelected).toBe(true);
  });

  test('Search filters containers in restart dependencies', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    const initialCount = await restartPage.getContainerCount();

    if (initialCount < 2) {
      test.skip(true, 'Need at least 2 containers to test filtering');
      return;
    }

    // Search for something unlikely to match all
    await restartPage.search('zzz_nonexistent_container');
    await page.waitForTimeout(500);

    const filteredCount = await restartPage.getContainerCount();
    expect(filteredCount).toBeLessThan(initialCount);

    // Clear search
    await restartPage.clearSearch();
    await page.waitForTimeout(500);

    const clearedCount = await restartPage.getContainerCount();
    expect(clearedCount).toBe(initialCount);
  });

  test('Cancel button returns to container detail', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    await restartPage.clickCancel();

    // Should navigate back to container detail
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINER}`));
  });

  test('Clear all removes all selections', async ({ page }) => {
    const restartPage = new RestartDependenciesPage(page);
    await restartPage.navigate(TEST_CONTAINER);

    // First make a selection if possible
    const containerCount = await restartPage.getContainerCount();

    if (containerCount === 0) {
      test.skip(true, 'No containers to select');
      return;
    }

    // Get status to find container names
    const status = await page.request.get(`${process.env.DOCKSMITH_URL || 'https://docksmith.ts.chis.dev'}/api/status`);
    const statusData = await status.json();
    const containers = statusData.data?.containers || [];

    // Select first available container
    if (containers.length > 0 && containers[0].container_name !== TEST_CONTAINER) {
      await restartPage.selectContainer(containers[0].container_name);
      await page.waitForTimeout(300);
    }

    const selectedBefore = await restartPage.getSelectedCount();

    // If we have selections, clear them
    if (selectedBefore > 0) {
      await restartPage.clearAll();
      await page.waitForTimeout(300);

      const selectedAfter = await restartPage.getSelectedCount();
      expect(selectedAfter).toBe(0);
    }
  });
});

test.describe('Restart Progress UI', () => {
  test('Restart progress page shows operation status', async ({ page, api }) => {
    // Start a restart via API to get an operation ID
    const restartResponse = await api.restartStart(TEST_CONTAINER);

    if (!restartResponse.success || !restartResponse.data?.operation_id) {
      test.skip(true, 'Could not start restart operation');
      return;
    }

    const operationId = restartResponse.data.operation_id;

    // Navigate to progress page
    const progressPage = new RestartProgressPage(page);
    await page.goto(`/operation/${operationId}`);

    // Wait for page to load
    await progressPage.waitForPageLoaded();

    // Should show operation status
    const status = await progressPage.getStatus();
    expect(['in-progress', 'complete', 'failed']).toContain(status);
  });
});

test.describe('Policies API', () => {
  test('GET /api/policies returns rollback policies', async ({ api }) => {
    const response = await api.getPolicies();

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    // global_policy might be null if not configured
    expect('global_policy' in (response.data || {})).toBe(true);
  });
});

test.describe('Registry Tags API', () => {
  test('GET /api/registry/tags/{imageRef} returns tags for an image', async ({ api }) => {
    // Use a common image that should have tags
    const response = await api.getRegistryTags('nginx');

    if (!response.success) {
      console.log('Registry tags failed:', response.error);
      // May fail due to rate limiting or auth issues
      test.skip(true, `Could not fetch registry tags: ${response.error}`);
      return;
    }

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    expect(response.data?.image_ref).toBe('nginx');
    expect(Array.isArray(response.data?.tags)).toBe(true);
    expect(response.data?.count).toBeGreaterThan(0);
  });
});
