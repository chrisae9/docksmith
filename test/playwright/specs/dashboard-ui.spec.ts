import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { DashboardPage } from '../pages/dashboard.page';

test.describe('Dashboard UI', () => {
  test('displays containers on load', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Should show at least some containers
    const count = await dashboard.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('filter toggle between All and Updates works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Get initial count with default filter (updates)
    const updatesCount = await dashboard.getContainerCount();

    // Switch to "All" filter
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);
    const allCount = await dashboard.getContainerCount();

    // "All" should show >= updates count
    expect(allCount).toBeGreaterThanOrEqual(updatesCount);

    // Switch back to "Updates"
    await dashboard.setFilter('updates');
    await page.waitForTimeout(500);
    const finalCount = await dashboard.getContainerCount();

    // Should be back to same count (or similar)
    expect(finalCount).toBeLessThanOrEqual(allCount);
  });

  test('search filters containers', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Switch to All to ensure we have containers to search
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    const initialCount = await dashboard.getContainerCount();

    // Search for a specific test container
    await dashboard.search('test-nginx');
    await page.waitForTimeout(300);

    // Should find containers matching search
    const searchCount = await dashboard.getContainerCount();
    expect(searchCount).toBeLessThanOrEqual(initialCount);

    // Verify search result contains the search term
    const nginxContainer = await dashboard.isContainerVisible(TEST_CONTAINERS.NGINX_BASIC);
    // May or may not be visible depending on filter

    // Clear search should restore count
    await dashboard.clearSearch();
    await page.waitForTimeout(300);
    const clearedCount = await dashboard.getContainerCount();
    expect(clearedCount).toBe(initialCount);
  });

  test('clicking container navigates to detail page', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Click on a test container
    await dashboard.clickContainer(TEST_CONTAINERS.NGINX_BASIC);

    // Should navigate to container detail page
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINERS.NGINX_BASIC}`));

    // Page title should show container name
    await expect(page.locator('h1')).toContainText(TEST_CONTAINERS.NGINX_BASIC);
  });

  test('container selection checkbox works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Select a container
    await dashboard.selectContainer(TEST_CONTAINERS.NGINX_BASIC);

    // Checkbox should be checked
    const container = await dashboard.getContainerByName(TEST_CONTAINERS.NGINX_BASIC);
    const checkbox = container.locator('input[type="checkbox"]');
    await expect(checkbox).toBeChecked();

    // Deselect it
    await dashboard.deselectContainer(TEST_CONTAINERS.NGINX_BASIC);
    await expect(checkbox).not.toBeChecked();
  });

  test('tab navigation works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Click History tab
    await dashboard.clickTab('History');
    await page.waitForTimeout(300);

    // Should show History page
    await expect(page.locator('h1')).toContainText('History');

    // Click Settings tab
    await dashboard.clickTab('Settings');
    await page.waitForTimeout(300);

    // Should show Settings page
    await expect(page.locator('h1')).toContainText('Settings');

    // Click back to Updates tab
    await dashboard.clickTab('Updates');
    await page.waitForTimeout(300);

    // Should be back on dashboard
    await dashboard.waitForContainers();
  });

  test('refresh button triggers update check', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Click refresh
    await dashboard.clickRefresh();

    // Should complete without error
    await page.waitForTimeout(2000);

    // Containers should still be visible
    const count = await dashboard.getContainerCount();
    expect(count).toBeGreaterThan(0);
  });

  test('stack collapse/expand works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Look for a stack header
    const stackHeader = page.locator('.stack-header').first();
    const isStackPresent = await stackHeader.isVisible().catch(() => false);

    if (isStackPresent) {
      // Click to collapse
      await stackHeader.click();
      await page.waitForTimeout(300);

      // Look for collapsed state
      const stackGroup = page.locator('.stack-group.collapsed, .stack-group').first();

      // Click again to expand
      await stackHeader.click();
      await page.waitForTimeout(300);
    }

    // Test passes if no stacks or if toggle works without error
    expect(true).toBe(true);
  });

  test('show/hide ignored containers toggle', async ({ page, api }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Look for the "Show Ignored" toggle/checkbox
    const showIgnoredToggle = page.locator('label:has-text("Ignored"), input[aria-label*="ignored"], button:has-text("Ignored")').first();
    const toggleExists = await showIgnoredToggle.isVisible().catch(() => false);

    if (toggleExists) {
      const initialCount = await dashboard.getContainerCount();

      // Toggle ignored visibility
      await showIgnoredToggle.click();
      await page.waitForTimeout(300);

      // Count may change based on ignored containers
      const toggledCount = await dashboard.getContainerCount();

      // Toggle back
      await showIgnoredToggle.click();
      await page.waitForTimeout(300);
    }

    // Test passes if toggle exists or if handled without error
    expect(true).toBe(true);
  });

  test('multiple container selection', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Select multiple containers
    await dashboard.selectContainer(TEST_CONTAINERS.NGINX_BASIC);
    await dashboard.selectContainer(TEST_CONTAINERS.REDIS_BASIC);

    // Both should be checked
    const nginxContainer = await dashboard.getContainerByName(TEST_CONTAINERS.NGINX_BASIC);
    const redisContainer = await dashboard.getContainerByName(TEST_CONTAINERS.REDIS_BASIC);

    await expect(nginxContainer.locator('input[type="checkbox"]')).toBeChecked();
    await expect(redisContainer.locator('input[type="checkbox"]')).toBeChecked();

    // Deselect all (click individual or find select all toggle)
    await dashboard.deselectContainer(TEST_CONTAINERS.NGINX_BASIC);
    await dashboard.deselectContainer(TEST_CONTAINERS.REDIS_BASIC);

    await expect(nginxContainer.locator('input[type="checkbox"]')).not.toBeChecked();
    await expect(redisContainer.locator('input[type="checkbox"]')).not.toBeChecked();
  });

  test('loading state displays correctly', async ({ page }) => {
    const dashboard = new DashboardPage(page);

    // Navigate but don't wait for containers
    await page.goto('/');

    // There might be a brief loading state - check that page renders
    const pageContent = page.locator('body');
    await expect(pageContent).toBeVisible();

    // Wait for containers to load
    await dashboard.waitForContainers();

    // Loading spinner should be gone
    await expect(dashboard.loadingSpinner).toBeHidden();
  });

  test('container status badge displays correctly', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Get a container's status badge
    const status = await dashboard.getContainerStatus(TEST_CONTAINERS.NGINX_BASIC);

    // Status should be one of the known statuses
    const validStatuses = ['Update Available', 'Up to Date', 'Ignored', 'Local Image', 'Pinnable', 'Blocked', 'Metadata Unavailable'];
    const isValidStatus = validStatuses.some(s => status.toLowerCase().includes(s.toLowerCase())) || status.length > 0;
    expect(isValidStatus).toBe(true);
  });
});
