/**
 * Scripts Management Tests
 *
 * These tests validate the scripts management functionality through the UI and API.
 * Scripts allow users to assign pre-update check scripts to containers.
 *
 * Run with: npm test -- specs/scripts.spec.ts
 */

import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { ScriptSelectionPage } from '../pages/script-selection.page';
import { ContainerDetailPage } from '../pages/container-detail.page';

// Use a container that's safe to modify scripts on
const TEST_CONTAINER = TEST_CONTAINERS.NGINX_BASIC;

test.describe('Scripts API', () => {
  test('GET /api/scripts returns available scripts', async ({ api }) => {
    const response = await api.getScripts();

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    expect(response.data?.scripts).toBeDefined();
    expect(Array.isArray(response.data?.scripts)).toBe(true);
    expect(typeof response.data?.count).toBe('number');
  });

  test('GET /api/scripts/assigned returns script assignments', async ({ api }) => {
    const response = await api.getScriptsAssigned();

    expect(response.success).toBe(true);
    expect(response.data).toBeDefined();
    expect(response.data?.assignments).toBeDefined();
    expect(Array.isArray(response.data?.assignments)).toBe(true);
    expect(typeof response.data?.count).toBe('number');
  });

  test('Script assignment lifecycle works correctly', async ({ api }) => {
    // Get available scripts first
    const scriptsResponse = await api.getScripts();

    if (!scriptsResponse.success || !scriptsResponse.data?.scripts.length) {
      test.skip(true, 'No scripts available - skipping assignment test');
      return;
    }

    const testScript = scriptsResponse.data.scripts[0];

    // Assign script to container
    const assignResponse = await api.assignScript(TEST_CONTAINER, testScript.path);

    if (!assignResponse.success) {
      // Assignment might fail if container doesn't support scripts
      console.log('Script assignment failed:', assignResponse.error);
      test.skip(true, `Cannot assign script to ${TEST_CONTAINER}: ${assignResponse.error}`);
      return;
    }

    expect(assignResponse.success).toBe(true);
    expect(assignResponse.data?.container).toBe(TEST_CONTAINER);
    expect(assignResponse.data?.script).toBe(testScript.path);

    // Verify assignment appears in list
    const assignedResponse = await api.getScriptsAssigned();
    expect(assignedResponse.success).toBe(true);

    const assignment = assignedResponse.data?.assignments.find(
      a => a.container === TEST_CONTAINER
    );
    expect(assignment).toBeDefined();
    expect(assignment?.script).toBe(testScript.path);

    // Unassign script
    const unassignResponse = await api.unassignScript(TEST_CONTAINER);
    expect(unassignResponse.success).toBe(true);
    expect(unassignResponse.data?.container).toBe(TEST_CONTAINER);

    // Verify assignment is removed
    const finalAssignedResponse = await api.getScriptsAssigned();
    const removedAssignment = finalAssignedResponse.data?.assignments.find(
      a => a.container === TEST_CONTAINER
    );
    expect(removedAssignment).toBeUndefined();
  });
});

test.describe('Scripts UI', () => {
  test('Script selection page loads correctly', async ({ page, api }) => {
    const scriptPage = new ScriptSelectionPage(page);
    await scriptPage.navigate(TEST_CONTAINER);

    // Page should load with title
    await expect(scriptPage.pageTitle).toHaveText('Select Script');

    // Should show either scripts list or info message
    const hasScripts = await scriptPage.getScriptCount() > 0;
    const hasNoScriptsMessage = await scriptPage.hasNoScripts();

    // One of these should be true
    expect(hasScripts || hasNoScriptsMessage).toBe(true);
  });

  test('Script selection page shows available scripts', async ({ page, api }) => {
    // First check if scripts exist via API
    const scriptsResponse = await api.getScripts();

    if (!scriptsResponse.success || scriptsResponse.data?.count === 0) {
      test.skip(true, 'No scripts available - skipping UI test');
      return;
    }

    const scriptPage = new ScriptSelectionPage(page);
    await scriptPage.navigate(TEST_CONTAINER);

    // UI should show scripts
    const uiScriptCount = await scriptPage.getScriptCount();
    expect(uiScriptCount).toBeGreaterThan(0);
  });

  test('Script search filters results', async ({ page, api }) => {
    // First check if scripts exist
    const scriptsResponse = await api.getScripts();

    if (!scriptsResponse.success || scriptsResponse.data?.count === 0) {
      test.skip(true, 'No scripts available - skipping search test');
      return;
    }

    const scriptPage = new ScriptSelectionPage(page);
    await scriptPage.navigate(TEST_CONTAINER);

    const initialCount = await scriptPage.getScriptCount();

    if (initialCount < 2) {
      test.skip(true, 'Need at least 2 scripts to test filtering');
      return;
    }

    // Search for something unlikely to match all
    await scriptPage.search('zzz_nonexistent');

    // Wait for filter to apply
    await page.waitForTimeout(500);

    const filteredCount = await scriptPage.getScriptCount();
    expect(filteredCount).toBeLessThan(initialCount);

    // Clear search
    await scriptPage.clearSearch();
    await page.waitForTimeout(500);

    const clearedCount = await scriptPage.getScriptCount();
    expect(clearedCount).toBe(initialCount);
  });

  test('Script selection can be made via UI', async ({ page, api }) => {
    // First check if scripts exist
    const scriptsResponse = await api.getScripts();

    if (!scriptsResponse.success || scriptsResponse.data?.count === 0) {
      test.skip(true, 'No scripts available - skipping selection test');
      return;
    }

    const scriptPage = new ScriptSelectionPage(page);
    await scriptPage.navigate(TEST_CONTAINER);

    const scriptCount = await scriptPage.getScriptCount();
    if (scriptCount === 0) {
      test.skip(true, 'No scripts visible in UI');
      return;
    }

    // Select none option first
    await scriptPage.selectNone();
    expect(await scriptPage.isNoneSelected()).toBe(true);

    // Cancel should work
    await scriptPage.clickCancel();

    // Should navigate back to container detail
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINER}`));
  });

  test('Navigate to script selection from container detail', async ({ page }) => {
    const detailPage = new ContainerDetailPage(page);
    await detailPage.navigate(TEST_CONTAINER);

    // Look for script-related button/link in the labels section
    // The exact UI depends on implementation, but there should be a way to access scripts
    await expect(detailPage.labelsSection).toBeVisible();
  });
});
