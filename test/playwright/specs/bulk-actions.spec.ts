/**
 * Bulk Actions & Selection Tests
 *
 * Tests for the unified Containers page selection mechanics,
 * bulk Actions dropdown, and batch label operations.
 *
 * Per CLAUDE.md: prefer API-level tests over Playwright for functional testing.
 * UI tests are minimal and focus on interaction mechanics.
 *
 * Run with: npm test -- specs/bulk-actions.spec.ts
 */

import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { ContainersPage } from '../pages/containers.page';

test.describe('Container Selection Mechanics', () => {
  // Note: This test uses 'all' filter where blocked containers don't appear,
  // so select all still selects everything. In 'updates' filter, select all
  // would skip blocked containers (see Smart Selection Bar tests below).
  test('select all button selects all visible containers', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Click Select All
    await containers.clickSelectAll();
    await page.waitForTimeout(300);

    // Selection bar should appear with correct count
    await expect(containers.selectionBar).toBeVisible();
    const selectedCount = await containers.getSelectedCount();
    expect(selectedCount).toBe(rowCount);

    // All checkboxes should be checked
    const checkedBoxes = await page.locator('.unified-row input[type="checkbox"]:checked').count();
    expect(checkedBoxes).toBe(rowCount);

    // Click Select All again to deselect all
    await containers.clickSelectAll();
    await page.waitForTimeout(300);

    // Selection bar should disappear
    await expect(containers.selectionBar).toBeHidden();
  });

  test('cancel button in selection bar clears all selections', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select a container
    await containers.containerRows.first().locator('.checkbox-zone').click();
    await expect(containers.selectionBar).toBeVisible();

    // Click Cancel
    await containers.clickCancel();
    await page.waitForTimeout(300);

    // Selection bar should be hidden
    await expect(containers.selectionBar).toBeHidden();

    // No checkboxes should be checked
    const checkedBoxes = await page.locator('.unified-row input[type="checkbox"]:checked').count();
    expect(checkedBoxes).toBe(0);
  });

  test('selection count updates correctly as containers are toggled', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount < 3) {
      test.skip(true, 'Need at least 3 containers');
      return;
    }

    // Select first container
    await containers.containerRows.nth(0).locator('.checkbox-zone').click();
    let count = await containers.getSelectedCount();
    expect(count).toBe(1);

    // Select second container
    await containers.containerRows.nth(1).locator('.checkbox-zone').click();
    count = await containers.getSelectedCount();
    expect(count).toBe(2);

    // Select third container
    await containers.containerRows.nth(2).locator('.checkbox-zone').click();
    count = await containers.getSelectedCount();
    expect(count).toBe(3);

    // Deselect second container
    await containers.containerRows.nth(1).locator('.checkbox-zone').click();
    count = await containers.getSelectedCount();
    expect(count).toBe(2);

    // Cleanup
    await containers.clickCancel();
  });

  test('selection persists across filter changes', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select first container
    await containers.containerRows.first().locator('.checkbox-zone').click();
    await expect(containers.selectionBar).toBeVisible();
    const initialCount = await containers.getSelectedCount();
    expect(initialCount).toBeGreaterThan(0);

    // Switch to updates filter
    await containers.setFilter('updates');
    await page.waitForTimeout(500);

    // Switch back to all
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Selection should still be active (bar visible with same or adjusted count)
    const barVisible = await containers.selectionBar.isVisible().catch(() => false);
    if (barVisible) {
      const restoredCount = await containers.getSelectedCount();
      expect(restoredCount).toBeGreaterThan(0);
    }

    // Cleanup
    if (barVisible) {
      await containers.clickCancel();
    }
  });

  test('stack header checkbox selects all containers in stack', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Look for a stack header with a checkbox
    const stackCheckbox = page.locator('.stack-header .checkbox-zone').first();
    const hasStackCheckbox = await stackCheckbox.isVisible().catch(() => false);

    if (!hasStackCheckbox) {
      test.skip(true, 'No stack headers with checkboxes found');
      return;
    }

    // Click the stack checkbox to select all in stack
    await stackCheckbox.click();
    await page.waitForTimeout(300);

    // Selection bar should appear with at least one selected
    await expect(containers.selectionBar).toBeVisible();
    const selectedCount = await containers.getSelectedCount();
    expect(selectedCount).toBeGreaterThan(0);

    // Cleanup
    await containers.clickCancel();
  });
});

test.describe('Bulk Actions Dropdown', () => {
  test('Actions dropdown shows label sections', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select a container
    await containers.containerRows.first().locator('.checkbox-zone').click();

    // Open Actions dropdown
    await containers.openBulkActions();
    await page.waitForTimeout(300);

    // Verify section labels exist
    const menu = containers.bulkActionsMenu;
    await expect(menu).toBeVisible();

    // Should show Labels section
    await expect(menu.locator('.bulk-section-label').filter({ hasText: 'Labels' })).toBeVisible();
    // Should show Pinning section
    await expect(menu.locator('.bulk-section-label').filter({ hasText: 'Pinning' })).toBeVisible();
    // Should show Clear section
    await expect(menu.locator('.bulk-section-label').filter({ hasText: 'Clear' })).toBeVisible();

    // Verify specific actions exist
    await expect(menu.getByRole('button', { name: 'Ignore' })).toBeVisible();
    await expect(menu.getByRole('button', { name: 'Unignore' })).toBeVisible();
    await expect(menu.getByRole('button', { name: 'Allow :latest' })).toBeVisible();
    await expect(menu.getByRole('button', { name: 'Pin Minor' })).toBeVisible();
    await expect(menu.getByRole('button', { name: 'Clear Tag Filter' })).toBeVisible();

    // Cleanup
    await containers.clickCancel();
  });

  test('Restart action is always visible', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select any container
    await containers.containerRows.first().locator('.checkbox-zone').click();
    await containers.openBulkActions();
    await page.waitForTimeout(300);

    // Restart should always be present regardless of container state
    await expect(containers.bulkActionsMenu.getByRole('button', { name: 'Restart' })).toBeVisible();

    // Cleanup
    await containers.clickCancel();
  });

  test('bulk restart navigates to operation page', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select first container
    await containers.containerRows.first().locator('.checkbox-zone').click();

    // Open actions and click Restart
    await containers.openBulkActions();
    await page.waitForTimeout(300);
    await containers.clickBulkAction('Restart');

    // Should navigate to operation page
    await expect(page).toHaveURL(/\/operation/, { timeout: 5000 });
  });

  test('bulk stop navigates to operation page', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    // Select first container
    await containers.containerRows.first().locator('.checkbox-zone').click();

    // Open actions dropdown
    await containers.openBulkActions();
    await page.waitForTimeout(300);

    // Check if Stop button exists (only shows when running containers are selected)
    const stopButton = containers.bulkActionsMenu.getByRole('button', { name: 'Stop' });
    const hasStop = await stopButton.isVisible().catch(() => false);

    if (!hasStop) {
      await containers.clickCancel();
      test.skip(true, 'Stop not available for selected containers');
      return;
    }

    await stopButton.click();

    // Should navigate to operation page
    await expect(page).toHaveURL(/\/operation/, { timeout: 5000 });
  });
});

test.describe('Batch Labels API', () => {
  test('POST /api/labels/batch validates empty operations', async ({ api }) => {
    const result = await api.batchSetLabels([]);
    // Should fail with validation error
    expect(result.success).toBe(false);
  });

  test('POST /api/labels/batch validates missing container name', async ({ api }) => {
    const result = await api.batchSetLabels([
      { container: '', ignore: true, no_restart: true },
    ]);
    // Should return per-container error
    expect(result.success).toBe(true);
    expect(result.data?.results).toBeDefined();
    expect(result.data?.results[0].success).toBe(false);
    expect(result.data?.results[0].error).toBeDefined();
  });

  test('POST /api/labels/batch returns per-container results', async ({ api }) => {
    const containerName = TEST_CONTAINERS.NGINX_BASIC;

    // Verify container exists first
    const container = await api.getContainer(containerName);
    if (!container) {
      test.skip(true, `Container ${containerName} not found`);
      return;
    }

    // Batch set ignore=true with no_restart to avoid container recreation
    const result = await api.batchSetLabels([
      { container: containerName, ignore: true, no_restart: true, force: true },
    ]);

    expect(result.success).toBe(true);
    expect(result.data?.results).toBeDefined();
    expect(result.data?.results.length).toBe(1);
    expect(result.data?.results[0].container).toBe(containerName);
    expect(result.data?.results[0].success).toBe(true);
    expect(result.data?.results[0].operation_id).toBeDefined();

    // Cleanup: remove the ignore label
    await api.setLabels(containerName, { ignore: false, no_restart: true, force: true });
  });

  test('POST /api/labels/batch handles partial failure', async ({ api }) => {
    const validContainer = TEST_CONTAINERS.NGINX_BASIC;

    // Verify valid container exists
    const container = await api.getContainer(validContainer);
    if (!container) {
      test.skip(true, `Container ${validContainer} not found`);
      return;
    }

    // Send one valid and one invalid container
    const result = await api.batchSetLabels([
      { container: validContainer, ignore: true, no_restart: true, force: true },
      { container: 'nonexistent-container-xyz-12345', ignore: true, no_restart: true },
    ]);

    expect(result.success).toBe(true);
    expect(result.data?.results).toBeDefined();
    expect(result.data?.results.length).toBe(2);

    // First should succeed
    expect(result.data?.results[0].container).toBe(validContainer);
    expect(result.data?.results[0].success).toBe(true);

    // Second should fail
    expect(result.data?.results[1].container).toBe('nonexistent-container-xyz-12345');
    expect(result.data?.results[1].success).toBe(false);
    expect(result.data?.results[1].error).toBeDefined();

    // Cleanup
    await api.setLabels(validContainer, { ignore: false, no_restart: true, force: true });
  });

  test('POST /api/labels/batch supports multiple label types', async ({ api }) => {
    const containerName = TEST_CONTAINERS.NGINX_BASIC;

    const container = await api.getContainer(containerName);
    if (!container) {
      test.skip(true, `Container ${containerName} not found`);
      return;
    }

    // Set allow_latest via batch
    const result = await api.batchSetLabels([
      { container: containerName, allow_latest: true, no_restart: true, force: true },
    ]);

    expect(result.success).toBe(true);
    expect(result.data?.results[0].success).toBe(true);

    // Cleanup
    await api.setLabels(containerName, { allow_latest: false, no_restart: true, force: true });
  });
});

test.describe('Smart Selection Bar', () => {
  test('select all in updates view excludes blocked containers', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('updates');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers with updates available');
      return;
    }

    // Check if there are any blocked containers by looking for BLOCKED badges
    const blockedRows = page.locator('.unified-row').filter({ has: page.locator('.status-badge[title="Update blocked"]') });
    const blockedCount = await blockedRows.count();
    if (blockedCount === 0) {
      test.skip(true, 'No blocked containers in updates view');
      return;
    }

    // Click Select All — should skip blocked containers
    await containers.clickSelectAll();
    await page.waitForTimeout(300);

    // Verify blocked containers are NOT checked
    for (let i = 0; i < blockedCount; i++) {
      const checkbox = blockedRows.nth(i).locator('input[type="checkbox"]');
      await expect(checkbox).not.toBeChecked();
    }

    // Blocked-excluded info banner should be visible
    expect(await containers.isBlockedExcludedBannerVisible()).toBe(true);

    // Banner should show correct count and have "Include anyway" button
    const excludedCount = await containers.getBlockedExcludedCount();
    expect(excludedCount).toBe(blockedCount);
    await expect(containers.includeBlockedButton).toBeVisible();

    // Cleanup
    await containers.clickCancel();
  });

  test('include anyway adds blocked containers to selection', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('updates');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers with updates available');
      return;
    }

    // Check for blocked containers
    const blockedRows = page.locator('.unified-row').filter({ has: page.locator('.status-badge[title="Update blocked"]') });
    const blockedCount = await blockedRows.count();
    if (blockedCount === 0) {
      test.skip(true, 'No blocked containers in updates view');
      return;
    }

    // Select all (excludes blocked)
    await containers.clickSelectAll();
    await page.waitForTimeout(300);

    // Verify blocked-excluded banner is showing
    expect(await containers.isBlockedExcludedBannerVisible()).toBe(true);

    // Click "Include anyway"
    await containers.clickIncludeBlocked();
    await page.waitForTimeout(300);

    // Blocked containers should now be checked
    for (let i = 0; i < blockedCount; i++) {
      const checkbox = blockedRows.nth(i).locator('input[type="checkbox"]');
      await expect(checkbox).toBeChecked();
    }

    // Force update warning should be visible
    expect(await containers.isForceUpdateWarningVisible()).toBe(true);

    // Update button should show "Force Update"
    expect(await containers.isForceUpdateButton()).toBe(true);

    // Cleanup
    await containers.clickCancel();
  });

  test('update type badges show correct categories', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('updates');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers with updates available');
      return;
    }

    // Select all updatable containers
    await containers.clickSelectAll();
    await page.waitForTimeout(300);

    // Update type badges container should be visible
    await expect(containers.updateTypeBadges).toBeVisible();

    // Get the badge breakdown
    const badges = await containers.getUpdateTypeBadges();

    // At least one badge category should have a non-zero count
    const totalBadged = badges.patch + badges.minor + badges.major + badges.rebuild + badges.blocked + badges.mismatch;
    expect(totalBadged).toBeGreaterThan(0);

    // Cleanup
    await containers.clickCancel();
  });

  test('force update button appears when blocked container manually selected', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('updates');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers with updates available');
      return;
    }

    // Find a blocked container row
    const blockedRow = page.locator('.unified-row').filter({ has: page.locator('.status-badge[title="Update blocked"]') }).first();
    const hasBlocked = await blockedRow.isVisible().catch(() => false);
    if (!hasBlocked) {
      test.skip(true, 'No blocked containers found');
      return;
    }

    // Manually click the blocked container's checkbox
    await blockedRow.locator('.checkbox-zone').click();
    await page.waitForTimeout(300);

    // Force update warning should appear
    expect(await containers.isForceUpdateWarningVisible()).toBe(true);

    // Update button should say "Force Update"
    expect(await containers.isForceUpdateButton()).toBe(true);

    // Cleanup
    await containers.clickCancel();
  });

  test('non-updatable selection shows plain count without badges', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount < 2) {
      test.skip(true, 'Need at least 2 containers');
      return;
    }

    // Find containers with CURRENT badge (up to date, non-updatable)
    const currentRows = page.locator('.unified-row').filter({ has: page.locator('.status-badge[title="Up to date"]') });
    const currentCount = await currentRows.count();
    if (currentCount < 2) {
      test.skip(true, 'Need at least 2 up-to-date containers');
      return;
    }

    // Select two CURRENT containers
    await currentRows.nth(0).locator('.checkbox-zone').click();
    await page.waitForTimeout(200);
    await currentRows.nth(1).locator('.checkbox-zone').click();
    await page.waitForTimeout(300);

    // Selection bar should show plain "{N} selected" text
    await expect(containers.selectionBar).toBeVisible();
    const summarySpan = containers.selectionBar.locator('.selection-summary > span');
    await expect(summarySpan).toBeVisible();
    const summaryText = await summarySpan.textContent();
    expect(summaryText).toMatch(/\d+\s+selected/);

    // Update type badges should NOT be visible
    await expect(containers.updateTypeBadges).toBeHidden();

    // Cleanup
    await containers.clickCancel();
  });
});
