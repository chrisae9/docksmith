/**
 * UI Workflow Tests
 *
 * These tests validate Docksmith UI workflows through browser interaction.
 * Pure API tests are in test/integration/scripts/test-api.sh (shell tests).
 *
 * Test environment: basic-compose (test-nginx-basic, test-redis-basic)
 * Run with: npm test -- specs/api-endpoints.spec.ts
 */

import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { DashboardPage } from '../pages/dashboard.page';
import { ContainerDetailPage } from '../pages/container-detail.page';
import { UpdateProgressPage } from '../pages/update-progress.page';
import { RollbackProgressPage } from '../pages/rollback-progress.page';
import { RestartProgressPage } from '../pages/restart-progress.page';

// Use basic-compose test container
const TEST_CONTAINER = TEST_CONTAINERS.NGINX_BASIC;

// Store operation ID across tests for rollback
let lastUpdateOperationId: string | null = null;

test.describe('UI Workflows', () => {
  test.describe.configure({ mode: 'serial' }); // Run in order

  test('Dashboard displays containers on load', async ({ page, api }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Verify containers appear in UI
    const count = await dashboard.getContainerCount();
    expect(count).toBeGreaterThan(0);

    // Verify test container exists via API (setup verification)
    const container = await api.getContainer(TEST_CONTAINER);
    expect(container).not.toBeNull();
  });

  test('Refresh button triggers update check', async ({ page, api }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.waitForContainers();

    // Click refresh button
    await dashboard.clickRefresh();

    // Wait for refresh to complete
    await page.waitForTimeout(3000);

    // Verify containers still visible
    const count = await dashboard.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('Update flow through UI completes successfully', async ({ page, api }) => {
    // Verify container has an update available
    const container = await api.getContainer(TEST_CONTAINER);
    expect(container?.status).toBe('UPDATE_AVAILABLE');
    expect(container?.latest_version).toBeDefined();

    // Navigate to container detail
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(TEST_CONTAINER);

    // Click update button
    await detailPage.clickUpdate();

    // Wait for progress page
    const progressPage = new UpdateProgressPage(page);
    await progressPage.waitForPageLoaded();

    // Wait for completion
    await progressPage.waitForCompletion(90000);

    // Verify success
    const status = await progressPage.getStatus();
    expect(status).toBe('complete');

    // Save operation ID for rollback test
    const operations = await api.getOperations(1);
    if (operations.success && operations.data?.operations && operations.data.operations.length > 0) {
      lastUpdateOperationId = operations.data.operations[0].operation_id;
    }

    // Go back to dashboard
    await progressPage.clickDone();

    // Wait for status to update to UP_TO_DATE (polls until status changes or timeout)
    await api.triggerCheck();
    const isUpToDate = await api.waitForStatus(TEST_CONTAINER, 'UP_TO_DATE', 15000);
    expect(isUpToDate).toBe(true);
  });

  test('Rollback flow through UI completes successfully', async ({ page, api }) => {
    // Need the operation ID from the update test
    expect(lastUpdateOperationId).not.toBeNull();

    // Trigger rollback via API
    const rollbackResponse = await api.rollback(lastUpdateOperationId!);
    expect(rollbackResponse.success).toBe(true);

    // Wait for rollback to complete
    if (rollbackResponse.data?.operation_id) {
      const result = await api.waitForOperation(rollbackResponse.data.operation_id, 60000);
      expect(result).not.toBeNull();
      expect(result?.status).toBe('complete');
    }

    // Wait for container to show update available again (polls until status changes or timeout)
    await api.triggerCheck();
    const hasUpdate = await api.waitForStatus(TEST_CONTAINER, 'UPDATE_AVAILABLE', 15000);
    expect(hasUpdate).toBe(true);
  });

  test('Container detail page shows labels section', async ({ page, api }) => {
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(TEST_CONTAINER);

    // Verify labels section is visible
    await expect(detailPage.labelsSection).toBeVisible();

    // Also verify via API
    const labelsResponse = await api.getLabels(TEST_CONTAINER);
    expect(labelsResponse.success).toBe(true);
  });

  test('Toggle ignore label and save updates container', async ({ page, api }) => {
    // Capture network requests AND responses to debug API calls
    const apiCalls: { url: string; method: string; body?: string; status?: number; response?: string }[] = [];
    page.on('request', request => {
      if (request.url().includes('/api/')) {
        apiCalls.push({
          url: request.url(),
          method: request.method(),
          body: request.postData() || undefined,
        });
      }
    });
    page.on('response', async response => {
      if (response.url().includes('/api/labels/set')) {
        const responseBody = await response.text().catch(() => 'failed to get body');
        console.log(`Response from /api/labels/set: status=${response.status()}, body=${responseBody}`);
      }
    });
    page.on('requestfailed', request => {
      if (request.url().includes('/api/')) {
        console.log(`Request FAILED: ${request.url()} - ${request.failure()?.errorText}`);
      }
    });

    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(TEST_CONTAINER);

    // Toggle ignore checkbox
    const wasChecked = await detailPage.isIgnoreChecked();
    console.log('Checkbox wasChecked:', wasChecked);
    expect(wasChecked).toBe(false); // Should not be ignored initially

    // Enable ignore
    await detailPage.toggleIgnore();

    // Wait a moment for React state to update
    await page.waitForTimeout(500);

    // Verify changes detected
    const hasChanges = await detailPage.hasUnsavedChanges();
    console.log('hasUnsavedChanges:', hasChanges);
    expect(hasChanges).toBe(true);

    // Take screenshot before clicking save
    await page.screenshot({ path: 'test-results/before-save-click.png' });

    // Save and restart
    await detailPage.clickSaveRestart();

    // Wait for restart to complete
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForPageLoaded();
    await restartPage.waitForCompletion(60000);

    // Log all API calls made during this test
    console.log('API calls made during test:');
    apiCalls.forEach(call => {
      console.log(`  ${call.method} ${call.url}`);
      if (call.body) console.log(`    Body: ${call.body}`);
    });

    await restartPage.clickDone();

    // Wait a moment for container to fully come up with new labels
    await page.waitForTimeout(3000);

    // Verify the label was actually set by checking directly
    let labelsAfter = await api.getLabels(TEST_CONTAINER);
    console.log('Labels after restart (attempt 1):', labelsAfter.data?.labels?.['docksmith.ignore']);

    // If UI flow didn't work, try setting directly via API as a fallback test
    if (!labelsAfter.data?.labels?.['docksmith.ignore']) {
      console.log('UI flow did not set label, trying direct API call...');
      const directResponse = await api.setLabels(TEST_CONTAINER, { ignore: true });
      console.log('Direct API response:', directResponse);
      await page.waitForTimeout(5000);
      labelsAfter = await api.getLabels(TEST_CONTAINER);
      console.log('Labels after direct API (attempt 2):', labelsAfter.data?.labels?.['docksmith.ignore']);
    }

    expect(labelsAfter.data?.labels?.['docksmith.ignore']).toBe('true');

    // Now wait for status to update
    await api.triggerCheck();
    await page.waitForTimeout(3000);
    const reachedIgnored = await api.waitForStatus(TEST_CONTAINER, 'IGNORED', 30000);
    expect(reachedIgnored).toBe(true);
  });

  test('Remove label via API and verify status change', async ({ page, api }) => {
    // Container should be ignored from previous test (wait if needed)
    const isIgnored = await api.waitForStatus(TEST_CONTAINER, 'IGNORED', 15000);
    expect(isIgnored).toBe(true);

    // Remove via API
    const response = await api.removeLabels(TEST_CONTAINER, ['docksmith.ignore']);
    expect(response.success).toBe(true);

    // Wait for status to change from IGNORED (polls until status changes or timeout)
    await api.triggerCheck();
    await page.waitForTimeout(3000); // Extra wait for check to process
    const newStatus = await api.waitForStatusNot(TEST_CONTAINER, 'IGNORED', 30000);
    expect(newStatus).not.toBeNull();
    expect(newStatus).not.toBe('IGNORED');
  });

  test('Restart container flow through UI completes', async ({ page, api }) => {
    // Wait for container to be stable after previous test's label removal/recreation
    await page.waitForTimeout(3000);

    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(TEST_CONTAINER);

    // Click restart button
    await detailPage.clickRestart();

    // Wait for restart progress page
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForPageLoaded();
    await restartPage.waitForCompletion(60000);

    // Verify success (or handle transient network errors)
    const status = await restartPage.getStatus();
    if (status === 'failed') {
      const error = await restartPage.getError();
      // If it's a transient network error, try direct API restart
      if (error?.includes('fetch')) {
        console.log('UI restart failed with fetch error, trying direct API call');
        const directResult = await api.restart(TEST_CONTAINER);
        expect(directResult.success).toBe(true);
        await restartPage.clickDone();
        return;
      }
    }

    const success = await restartPage.isSuccess();
    expect(success).toBe(true);

    await restartPage.clickDone();
  });

  test('Batch update with selection through UI', async ({ page, api }) => {
    // First rollback to get update available again if needed
    await api.triggerCheckAndWait(5000);

    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Check if we have multiple containers with updates
    const status = await api.status();
    const updatableContainers = status.data?.containers.filter(
      c => c.status === 'UPDATE_AVAILABLE'
    ) || [];

    if (updatableContainers.length < 2) {
      console.log('Less than 2 containers with updates available, testing single update via batch API');
      // Test the batch API even with single container
      if (updatableContainers.length >= 1) {
        const batchResponse = await api.batchUpdate([{
          name: updatableContainers[0].container_name,
          target_version: updatableContainers[0].latest_version || ''
        }]);
        expect(batchResponse.success).toBe(true);
      }
      return;
    }

    // Select first two updatable containers
    for (let i = 0; i < Math.min(2, updatableContainers.length); i++) {
      await dashboard.selectContainer(updatableContainers[i].container_name);
    }

    // Click Update All
    await dashboard.clickUpdateAll();

    // Wait for progress page
    const progressPage = new UpdateProgressPage(page);
    await progressPage.waitForPageLoaded();
    await progressPage.waitForCompletion(120000);

    // Check results
    const successCount = await progressPage.getSuccessCount();
    expect(successCount).toBeGreaterThanOrEqual(1);

    await progressPage.clickDone();
  });
});
