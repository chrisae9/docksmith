/**
 * Label Tests
 *
 * These tests mirror test-labels.sh and verify Docksmith label functionality
 * through the browser UI.
 *
 * IMPORTANT: These tests require a dedicated test environment with:
 * - Containers configured with specific docksmith labels in docker-compose.yml
 * - Ability to modify container labels (requires container recreation)
 *
 * When running against production, these tests will be skipped automatically.
 *
 * Test environment: labels (various test containers with different labels)
 * Run with: ./run-tests.sh labels
 */

import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { DashboardPage } from '../pages/dashboard.page';
import { ContainerDetailPage } from '../pages/container-detail.page';
import { RestartProgressPage } from '../pages/restart-progress.page';
import { UpdateProgressPage } from '../pages/update-progress.page';

// Check if we're in a proper test environment
// Production containers use real names like "factorio", "plex", etc.
// These tests require a dedicated test environment with specific labels configured
function shouldSkipLabelTests(): boolean {
  // These tests require containers with specific test prefixes
  // In production, we use real container names - so always skip
  // Only run if DOCKSMITH_RUN_LABEL_TESTS=true is set
  return process.env.DOCKSMITH_RUN_LABEL_TESTS !== 'true';
}

test.describe('Label Functionality', () => {
  test.describe.configure({ mode: 'serial' }); // Run in order

  // Skip all tests if not in proper test environment
  test.beforeEach(async () => {
    if (shouldSkipLabelTests()) {
      test.skip(true, 'Label tests require dedicated test environment (set DOCKSMITH_RUN_LABEL_TESTS=true)');
    }
  });

  // ==================== Test 1: Ignore Label ====================
  test('1. Ignore label - shows IGNORED status', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_IGNORED;

    // Wait for container to be discovered as IGNORED (has ignore label from compose)
    await api.triggerCheck();
    const isIgnored = await api.waitForStatus(containerName, 'IGNORED', 15000);
    expect(isIgnored).toBe(true);

    // Navigate to detail page
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    // Verify status badge shows Ignored
    const statusText = await detailPage.getStatus();
    expect(statusText.toLowerCase()).toContain('ignored');

    // Toggle off ignore
    await detailPage.toggleIgnore();
    await detailPage.clickSaveRestart();

    // Wait for restart
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForCompletion(60000);
    await restartPage.clickDone();

    // Wait for status to change from IGNORED (polls until status changes or timeout)
    // Give more time for background check to complete after restart
    await api.triggerCheck();
    await page.waitForTimeout(3000); // Extra wait for check to process
    const newStatus = await api.waitForStatusNot(containerName, 'IGNORED', 30000);
    expect(newStatus).not.toBeNull();
    expect(newStatus).not.toBe('IGNORED');

    // Re-enable ignore for cleanup
    await api.setLabels(containerName, { ignore: true, force: true });
    await api.waitForStatus(containerName, 'IGNORED', 15000);
  });

  // ==================== Test 2: Allow Latest Label ====================
  test('2. Allow Latest label - visible in detail', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_LATEST;

    // Verify label via API
    const labelsResponse = await api.getLabels(containerName);
    expect(labelsResponse.success).toBe(true);
    expect(labelsResponse.data?.labels['docksmith.allow-latest']).toBe('true');

    // Navigate to detail
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    // Verify allow-latest checkbox is checked
    const isChecked = await detailPage.isAllowLatestChecked();
    expect(isChecked).toBe(true);

    // Verify container appears in discovery
    const container = await api.getContainer(containerName);
    expect(container).not.toBeNull();
  });

  // ==================== Test 3: Pre-Update Check Pass ====================
  test('3. Pre-Update Check (pass) - Update succeeds', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_PRE_PASS;

    // Verify pre-update-check label exists
    const labelsResponse = await api.getLabels(containerName);
    expect(labelsResponse.success).toBe(true);
    const preCheck = labelsResponse.data?.labels['docksmith.pre-update-check'];
    expect(preCheck).toBeDefined();
    expect(preCheck).not.toBe('');

    // Navigate to detail page
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    // Verify pre-update script is set
    const scriptValue = await detailPage.getPreUpdateScriptValue();
    expect(scriptValue).not.toBe('None');

    // Attempt update via API (should pass)
    const updateResponse = await api.update(containerName, '8.4');
    expect(updateResponse.success).toBe(true);

    // Wait for completion
    if (updateResponse.data?.operation_id) {
      const result = await api.waitForOperation(updateResponse.data.operation_id, 60000);
      expect(result).not.toBeNull();
      expect(result?.status).toBe('complete');
    }

    // Wait for version to update (polls until version matches or timeout)
    await api.triggerCheck();
    const versionUpdated = await api.waitForVersion(containerName, '8.4', 15000);
    expect(versionUpdated).toBe(true);
  });

  // ==================== Test 4: Pre-Update Check Fail ====================
  test('4. Pre-Update Check (fail) - Update blocked', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_PRE_FAIL;

    // Attempt update via API
    const updateResponse = await api.update(containerName, '8.4');

    // Should start but then fail
    if (updateResponse.success && updateResponse.data?.operation_id) {
      const result = await api.waitForOperation(updateResponse.data.operation_id, 30000);
      expect(result?.status).toBe('failed');
      expect(result?.error_message).toContain('Pre-update check failed');
    }
  });

  // ==================== Test 5: Restart After ====================
  test('5. Restart After - triggers dependents', async ({ page, api }) => {
    const primaryContainer = TEST_CONTAINERS.LABELS_RESTART_DEPS;

    // Navigate and restart primary
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(primaryContainer);
    await detailPage.clickRestartButton();

    // Wait for restart
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForCompletion(60000);

    // Check if dependents were restarted
    const dependentsRestarted = await restartPage.getDependentsRestarted();

    // Verify primary was restarted
    const isSuccess = await restartPage.isSuccess();
    expect(isSuccess).toBe(true);

    // Dependents should have been restarted
    console.log('Dependents restarted:', dependentsRestarted);
    expect(dependentsRestarted.length).toBeGreaterThanOrEqual(1);

    await restartPage.clickDone();
  });

  // ==================== Test 6: Label Sync Detection ====================
  test('6. Label Sync Detection - Out of sync banner shows', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_RESTART_DEPS;

    // First clean up any stale labels
    await api.removeLabels(containerName, ['docksmith.allow-latest']).catch(() => {});
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Set a label with no_restart - creates out-of-sync state
    const setResponse = await api.setLabels(containerName, {
      allow_latest: true,
      no_restart: true,
    });
    expect(setResponse.success).toBe(true);

    // Trigger check to detect sync state
    await api.triggerCheckAndWait(5000);

    // Navigate to detail page
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    // Verify out-of-sync banner is visible
    const isOutOfSync = await detailPage.isOutOfSyncBannerVisible();
    expect(isOutOfSync).toBe(true);

    // Click sync button to apply
    await detailPage.clickSyncButton();

    // Wait for restart
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForCompletion(60000);
    await restartPage.clickDone();

    // Verify no longer out of sync
    await api.triggerCheckAndWait(3000);
    await detailPage.navigate(containerName);
    const stillOutOfSync = await detailPage.isOutOfSyncBannerVisible();
    expect(stillOutOfSync).toBe(false);

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.allow-latest']);
  });

  // ==================== Test 7: Version Pin Minor ====================
  test('7. Version Pin Minor - limits versions', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_NGINX;

    // Navigate to detail
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    // Set version pin to minor
    await detailPage.setVersionPin('minor');
    await detailPage.clickSaveRestart();

    // Wait for restart
    const restartPage = new RestartProgressPage(page);
    await restartPage.waitForCompletion(60000);
    await restartPage.clickDone();

    // Trigger check and verify pinning works
    await api.triggerCheckAndWait(5000);
    const container = await api.getContainer(containerName);

    // Should only suggest versions with same minor version (1.25.x)
    if (container?.latest_version) {
      console.log('Latest version suggested:', container.latest_version);
      expect(container.latest_version).toMatch(/^1\.25\./);
    }

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.version-pin-minor']);
  });

  // ==================== Test 8: Tag Regex ====================
  test('8. Tag Regex - filters tags', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_ALPINE;

    // Set tag-regex to only allow alpine tags
    const setResponse = await api.setLabels(containerName, {
      tag_regex: '^[0-9]+-alpine$',
    });
    expect(setResponse.success).toBe(true);

    // Trigger check
    await api.triggerCheckAndWait(5000);

    // Verify latest version matches alpine pattern
    const container = await api.getContainer(containerName);
    if (container?.latest_version) {
      expect(container.latest_version).toMatch(/-alpine$/);
    }

    // Navigate and verify via UI
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    const tagFilterValue = await detailPage.getTagFilterValue();
    expect(tagFilterValue).not.toBe('None');

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.tag-regex']);
  });

  // ==================== Test 9: Version Min ====================
  test('9. Version Min - filters old versions', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_POSTGRES;

    // Set minimum version
    const setResponse = await api.setLabels(containerName, {
      version_min: '14.0',
    });
    expect(setResponse.success).toBe(true);

    // Trigger check
    await api.triggerCheckAndWait(5000);

    // Verify latest version is >= 14.0
    const container = await api.getContainer(containerName);
    if (container?.latest_version) {
      const majorVersion = parseInt(container.latest_version.split('.')[0], 10);
      expect(majorVersion).toBeGreaterThanOrEqual(14);
    }

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.version-min']);
  });

  // ==================== Test 10: Version Max ====================
  test('10. Version Max - filters new versions', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_REDIS;

    // Set maximum version
    const setResponse = await api.setLabels(containerName, {
      version_max: '7.99',
    });
    expect(setResponse.success).toBe(true);

    // Trigger check
    await api.triggerCheckAndWait(5000);

    // Verify latest version is <= 7.99
    const container = await api.getContainer(containerName);
    if (container?.latest_version) {
      const majorVersion = parseInt(container.latest_version.split('.')[0], 10);
      expect(majorVersion).toBeLessThanOrEqual(7);
    }

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.version-max']);
  });

  // ==================== Test 11: Invalid Regex ====================
  test('11. Invalid Regex - shows error', async ({ api }) => {
    const containerName = TEST_CONTAINERS.LABELS_NGINX;

    // Try to set invalid regex
    const response = await api.setLabels(containerName, {
      tag_regex: '(invalid[regex',
      no_restart: true,
    });

    // Should fail with validation error
    expect(response.success).toBe(false);
    expect(response.error).toContain('invalid regex');
  });

  // ==================== Test 12: Regex Too Long ====================
  test('12. Regex Too Long - rejected', async ({ api }) => {
    const containerName = TEST_CONTAINERS.LABELS_NGINX;

    // Create pattern over 500 chars
    const longPattern = 'a'.repeat(501);

    // Try to set overly long regex
    const response = await api.setLabels(containerName, {
      tag_regex: longPattern,
      no_restart: true,
    });

    // Should fail with length error
    expect(response.success).toBe(false);
    expect(response.error).toContain('too long');
  });

  // ==================== Test 13: Combined Constraints ====================
  test('13. Combined Constraints - multiple labels work', async ({ page, api }) => {
    const containerName = TEST_CONTAINERS.LABELS_NODE;

    // Set multiple constraints (with restart to apply)
    const setResponse = await api.setLabels(containerName, {
      version_pin_minor: true,
      version_max: '20.99',
    });
    expect(setResponse.success).toBe(true);

    // Wait for restart
    await new Promise(resolve => setTimeout(resolve, 8000));

    // Trigger check
    await api.triggerCheckAndWait(5000);

    // Verify both labels persisted
    const labelsResponse = await api.getLabels(containerName);
    expect(labelsResponse.success).toBe(true);
    expect(labelsResponse.data?.labels['docksmith.version-pin-minor']).toBe('true');
    expect(labelsResponse.data?.labels['docksmith.version-max']).toBe('20.99');

    // Navigate and verify in UI
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(containerName);

    const versionPin = await detailPage.getVersionPin();
    expect(versionPin).toBe('minor');

    // Cleanup
    await api.removeLabels(containerName, ['docksmith.version-pin-minor', 'docksmith.version-max']);
  });
});
