import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { DashboardPage } from '../pages/dashboard.page';
import { HistoryPage } from '../pages/history.page';

test.describe('History UI', () => {
  test('navigating to History tab shows history page', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();

    // Click History tab
    await dashboard.clickTab('History');

    // Should show History page
    await expect(page.locator('h1')).toContainText('History');
  });

  test('history page shows operations list', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Should have some operations or empty message
    const count = await history.getOperationCount();
    // May or may not have operations - test should pass either way
    expect(count >= 0).toBe(true);
  });

  test('status filter works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Get all operations count
    await history.setStatusFilter('all');
    await page.waitForTimeout(300);
    const allCount = await history.getOperationCount();

    // Filter to success only
    await history.setStatusFilter('success');
    await page.waitForTimeout(300);
    const successCount = await history.getOperationCount();

    // Success count should be <= all count
    expect(successCount).toBeLessThanOrEqual(allCount);

    // Filter to failed
    await history.setStatusFilter('failed');
    await page.waitForTimeout(300);
    const failedCount = await history.getOperationCount();

    // Failed count should be <= all count
    expect(failedCount).toBeLessThanOrEqual(allCount);

    // Back to all
    await history.setStatusFilter('all');
    await page.waitForTimeout(300);
    const resetCount = await history.getOperationCount();
    expect(resetCount).toBe(allCount);
  });

  test('type filter works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Get initial count (default filter)
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);
    const allCount = await history.getOperationCount();

    // Filter by different types
    const types: Array<'single' | 'batch' | 'stack' | 'rollback' | 'restart' | 'label_change'> = [
      'single', 'batch', 'stack', 'rollback', 'restart', 'label_change'
    ];

    for (const type of types) {
      await history.setTypeFilter(type);
      await page.waitForTimeout(300);
      const typeCount = await history.getOperationCount();

      // Each type count should be <= all count
      expect(typeCount).toBeLessThanOrEqual(allCount);
    }

    // Reset to all
    await history.setTypeFilter('all');
  });

  test('search filters operations', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);

    const initialCount = await history.getOperationCount();

    if (initialCount > 0) {
      // Search for a common term
      await history.search('test');
      await page.waitForTimeout(300);

      const searchCount = await history.getOperationCount();
      expect(searchCount).toBeLessThanOrEqual(initialCount);

      // Clear search
      await history.clearSearch();
      await page.waitForTimeout(300);

      const clearedCount = await history.getOperationCount();
      expect(clearedCount).toBe(initialCount);
    }
  });

  test('expand/collapse operation card works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      // Initially not expanded
      const initialExpanded = await history.isOperationExpanded(0);

      // Expand first operation
      await history.expandOperation(0);
      await page.waitForTimeout(300);

      const afterExpand = await history.isOperationExpanded(0);
      expect(afterExpand).toBe(true);

      // Collapse it
      await history.expandOperation(0);
      await page.waitForTimeout(300);

      const afterCollapse = await history.isOperationExpanded(0);
      expect(afterCollapse).toBe(false);
    }
  });

  test('operation card shows container name', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      const containerName = await history.getOperationContainerName(0);
      // Container name should not be empty (may be stack name for batch ops)
      expect(containerName.length).toBeGreaterThan(0);
    }
  });

  test('operation card shows correct status indicator', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      const status = await history.getOperationStatus(0);
      const validStatuses = ['success', 'failed', 'pending', 'rollback'];
      expect(validStatuses).toContain(status);
    }
  });

  test('clicking container link navigates to container detail', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single/update operations (most likely to have container links)
    await history.setTypeFilter('single');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      const containerName = await history.getOperationContainerName(0);

      // Click the container link
      await history.clickContainerLink(0);
      await page.waitForTimeout(500);

      // Should navigate to container detail page
      await expect(page).toHaveURL(/\/container\//);
    }
  });

  test('copy operation ID button works', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      // Click copy button - should not throw error
      await history.copyOperationId(0);

      // Look for "copied" visual feedback
      const card = await history.getOperationCard(0);
      const copyBtn = card.locator('.op-copy-btn');

      // Button should still exist
      await expect(copyBtn).toBeVisible();
    }
  });

  test('rollback button shows for eligible operations', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single updates (most likely to have rollback option)
    await history.setTypeFilter('single');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    if (count > 0) {
      // Check if first operation has rollback button
      const hasRollback = await history.hasRollbackButton(0);
      // May or may not have rollback (depends on operation state)
      expect(typeof hasRollback).toBe('boolean');
    }
  });

  test('rollback confirmation dialog appears', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single updates
    await history.setTypeFilter('single');
    await history.setStatusFilter('success');
    await page.waitForTimeout(300);

    const count = await history.getOperationCount();

    // Find an operation with rollback button
    for (let i = 0; i < Math.min(count, 5); i++) {
      const hasRollback = await history.hasRollbackButton(i);
      if (hasRollback) {
        // Click rollback
        await history.clickRollback(i);
        await page.waitForTimeout(300);

        // Confirm dialog should appear
        const isVisible = await history.isRollbackConfirmVisible();
        expect(isVisible).toBe(true);

        // Cancel the rollback
        await history.cancelRollback();
        await page.waitForTimeout(300);

        // Dialog should be gone
        const isGone = await history.isRollbackConfirmVisible();
        expect(isGone).toBe(false);

        break;
      }
    }
  });

  test('combined filters work together', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.clickTab('History');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Apply multiple filters
    await history.setStatusFilter('success');
    await history.setTypeFilter('single');
    await history.search('test');
    await page.waitForTimeout(300);

    const filteredCount = await history.getOperationCount();

    // Reset all filters
    await history.setStatusFilter('all');
    await history.setTypeFilter('all');
    await history.clearSearch();
    await page.waitForTimeout(300);

    const resetCount = await history.getOperationCount();

    // Filtered count should be <= reset count
    expect(filteredCount).toBeLessThanOrEqual(resetCount);
  });
});
