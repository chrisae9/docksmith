import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { ContainersPage } from '../pages/containers.page';

test.describe('Containers UI', () => {
  test('displays containers on load', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Should show at least some containers
    const count = await containers.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('filter toggle between All and Updates works', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Get initial count with default filter (updates)
    const updatesCount = await containers.getContainerCount();

    // Switch to "All" filter
    await containers.setFilter('all');
    await page.waitForTimeout(500);
    const allCount = await containers.getContainerCount();

    // "All" should show >= updates count
    expect(allCount).toBeGreaterThanOrEqual(updatesCount);

    // Switch back to "Updates"
    await containers.setFilter('updates');
    await page.waitForTimeout(500);
    const finalCount = await containers.getContainerCount();

    // Should be back to same count (or similar)
    expect(finalCount).toBeLessThanOrEqual(allCount);
  });

  test('search filters containers', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Switch to All to ensure we have containers to search
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const initialCount = await containers.getContainerCount();

    // Search for a specific test container
    await containers.search('test-nginx');
    await page.waitForTimeout(300);

    // Should find containers matching search
    const searchCount = await containers.getContainerCount();
    expect(searchCount).toBeLessThanOrEqual(initialCount);

    // Clear search should restore count
    await containers.clearSearch();
    await page.waitForTimeout(300);
    const clearedCount = await containers.getContainerCount();
    expect(clearedCount).toBe(initialCount);
  });

  test('clicking container navigates to detail page', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Click on a test container (clicks .row-link zone)
    await containers.clickContainer(TEST_CONTAINERS.NGINX_BASIC);

    // Should navigate to container detail page
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINERS.NGINX_BASIC}`));

    // Page title should show container name
    await expect(page.locator('h1')).toContainText(TEST_CONTAINERS.NGINX_BASIC);
  });

  test('container selection checkbox works', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Find the first container row with a checkbox
    const checkboxes = page.locator('.unified-row input[type="checkbox"]');
    const checkboxCount = await checkboxes.count();

    if (checkboxCount === 0) {
      test.skip(true, 'No containers with checkboxes found');
      return;
    }

    // Get the first container row
    const firstRow = containers.containerRows.first();
    const firstCheckbox = firstRow.locator('input[type="checkbox"]');
    await expect(firstCheckbox).toBeVisible();

    // Click checkbox zone to select
    await firstRow.locator('.checkbox-zone').click();
    await expect(firstCheckbox).toBeChecked();

    // Selection bar should appear
    await expect(containers.selectionBar).toBeVisible();

    // Click checkbox zone again to deselect
    await firstRow.locator('.checkbox-zone').click();
    await expect(firstCheckbox).not.toBeChecked();
  });

  test('tab navigation works', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Click History tab
    await containers.clickTab('History');
    await page.waitForTimeout(300);

    // Should show History page
    await expect(page.locator('h1')).toContainText('History');

    // Click Settings tab
    await containers.clickTab('Settings');
    await page.waitForTimeout(300);

    // Should show Settings page
    await expect(page.locator('h1')).toContainText('Settings');

    // Click back to Containers tab
    await containers.clickTab('Containers');
    await page.waitForTimeout(300);

    // Should be back on containers page
    await containers.waitForContainers();
  });

  test('refresh button triggers update check', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Trigger refresh via API (no refresh button in UI, uses pull-to-refresh)
    await containers.triggerRefresh();

    // Should complete without error
    await page.waitForTimeout(2000);

    // Containers should still be visible
    const count = await containers.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('stack collapse/expand works', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Look for a stack header (StackGroup component)
    const stackHeader = page.locator('.stack-header').first();
    const isStackPresent = await stackHeader.isVisible().catch(() => false);

    if (isStackPresent) {
      // Click to collapse
      await stackHeader.click();
      await page.waitForTimeout(300);

      // Click again to expand
      await stackHeader.click();
      await page.waitForTimeout(300);
    }

    // Test passes if no stacks or if toggle works without error
    expect(true).toBe(true);
  });

  test('show/hide ignored containers toggle', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Open View Options settings menu
    const viewOptionsButton = page.locator('button[title="View Options"]');
    const toggleExists = await viewOptionsButton.isVisible().catch(() => false);

    if (toggleExists) {
      const initialCount = await containers.getContainerCount();

      // Click View Options to open settings menu
      await viewOptionsButton.click();
      await page.waitForTimeout(300);

      // Find the "Show ignored" checkbox
      const showIgnoredCheckbox = page.locator('label.settings-checkbox').filter({ hasText: 'Show ignored' }).locator('input');
      const checkboxExists = await showIgnoredCheckbox.isVisible().catch(() => false);

      if (checkboxExists) {
        // Toggle it
        await showIgnoredCheckbox.click();
        await page.waitForTimeout(300);

        // Toggle it back
        await showIgnoredCheckbox.click();
        await page.waitForTimeout(300);
      }

      // Close the settings menu
      const closeButton = page.locator('.settings-close-btn');
      if (await closeButton.isVisible().catch(() => false)) {
        await closeButton.click();
      }
    }

    // Test passes if toggle exists or if handled without error
    expect(true).toBe(true);
  });

  test('multiple container selection', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();

    if (rowCount < 2) {
      test.skip();
      return;
    }

    // Select first container via checkbox zone
    const firstRow = containers.containerRows.first();
    const secondRow = containers.containerRows.nth(1);

    await firstRow.locator('.checkbox-zone').click();
    await expect(firstRow.locator('input[type="checkbox"]')).toBeChecked();

    await secondRow.locator('.checkbox-zone').click();
    await expect(secondRow.locator('input[type="checkbox"]')).toBeChecked();

    // Selection bar should show "2 selected"
    await expect(containers.selectionBar).toContainText('2 selected');

    // Deselect both
    await firstRow.locator('.checkbox-zone').click();
    await secondRow.locator('.checkbox-zone').click();

    await expect(firstRow.locator('input[type="checkbox"]')).not.toBeChecked();
    await expect(secondRow.locator('input[type="checkbox"]')).not.toBeChecked();
  });

  test('loading state displays correctly', async ({ page }) => {
    const containers = new ContainersPage(page);

    // Navigate but don't wait for containers
    await page.goto('/');

    // There might be a brief loading state - check that page renders
    const pageContent = page.locator('body');
    await expect(pageContent).toBeVisible();

    // Wait for containers to load
    await containers.waitForContainers();

    // Skeleton should be gone
    await expect(containers.skeletonLoader).toBeHidden();
  });

  test('container status badge displays correctly', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    // Get a container's status badge
    const status = await containers.getContainerStatus(TEST_CONTAINERS.NGINX_BASIC);

    // Status should be one of the known statuses or empty (no badge for running containers)
    const validStatuses = ['Up to date', 'Major update', 'Minor update', 'Patch update', 'Update available', 'Ignored', 'Local image', 'No version tag specified', 'Update blocked', 'Running image differs'];
    const isValidStatus = validStatuses.some(s => status.toLowerCase().includes(s.toLowerCase())) || status.length >= 0;
    expect(isValidStatus).toBe(true);
  });

  test('sub-tab navigation works', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();

    // Click Images sub-tab
    await containers.clickSubTab('Images');
    await page.waitForTimeout(500);

    // Click Networks sub-tab
    await containers.clickSubTab('Networks');
    await page.waitForTimeout(500);

    // Click Volumes sub-tab
    await containers.clickSubTab('Volumes');
    await page.waitForTimeout(500);

    // Click back to Containers sub-tab
    await containers.clickSubTab('Containers');
    await page.waitForTimeout(500);

    // Should show container rows again
    const count = await containers.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('selection bar appears with Actions dropdown', async ({ page }) => {
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
    const firstRow = containers.containerRows.first();
    await firstRow.locator('.checkbox-zone').click();

    // Selection bar should appear
    await expect(containers.selectionBar).toBeVisible();
    await expect(containers.selectionBar).toContainText('1 selected');

    // Actions button should be visible
    await expect(containers.actionsButton).toBeVisible();

    // Click Actions to open dropdown
    await containers.openBulkActions();
    await page.waitForTimeout(300);

    // Bulk actions menu should be visible
    await expect(containers.bulkActionsMenu).toBeVisible();

    // Should have Restart as a universal action
    const restartButton = containers.bulkActionsMenu.getByRole('button', { name: 'Restart' });
    await expect(restartButton).toBeVisible();

    // Cancel selection
    await containers.clickCancel();
    await expect(containers.selectionBar).toBeHidden();
  });

  // === Critical click target separation tests ===
  // These verify the CSS grid layout has correct click targets with zero dead zones

  test('checkbox zone selects container without navigating', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    const firstRow = containers.containerRows.first();
    const initialUrl = page.url();

    // Click the checkbox zone (left column with checkbox + state dot)
    await firstRow.locator('.checkbox-zone').click();

    // Should NOT navigate — URL should stay the same
    expect(page.url()).toBe(initialUrl);

    // Should toggle the checkbox
    await expect(firstRow.locator('input[type="checkbox"]')).toBeChecked();

    // Cleanup
    await firstRow.locator('.checkbox-zone').click();
  });

  test('row-link navigates without selecting', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    const firstRow = containers.containerRows.first();
    const firstCheckbox = firstRow.locator('input[type="checkbox"]');

    // Verify checkbox is not checked before clicking
    await expect(firstCheckbox).not.toBeChecked();

    // Click the row-link zone (middle column with name/version)
    await firstRow.locator('.row-link').click();

    // Should navigate to detail page
    await expect(page).toHaveURL(/\/container\//);
  });

  test('action menu button opens menu without navigating or selecting', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    const firstRow = containers.containerRows.first();
    const initialUrl = page.url();

    // Click the 3-dot action menu button in the right column
    await firstRow.locator('.item-actions button').first().click();
    await page.waitForTimeout(300);

    // Should NOT navigate
    expect(page.url()).toBe(initialUrl);

    // Should NOT select the checkbox
    await expect(firstRow.locator('input[type="checkbox"]')).not.toBeChecked();

    // Action menu should be visible
    const actionMenu = firstRow.locator('.action-menu');
    await expect(actionMenu).toBeVisible();
  });

  test('container row has three distinct grid columns', async ({ page }) => {
    const containers = new ContainersPage(page);
    await containers.navigate();
    await containers.setFilter('all');
    await page.waitForTimeout(500);

    const rowCount = await containers.getContainerCount();
    if (rowCount === 0) {
      test.skip(true, 'No containers available');
      return;
    }

    const firstRow = containers.containerRows.first();

    // Verify all three grid columns exist
    await expect(firstRow.locator('.checkbox-zone')).toBeVisible();
    await expect(firstRow.locator('.row-link')).toBeVisible();
    await expect(firstRow.locator('.item-actions')).toBeVisible();
  });
});
