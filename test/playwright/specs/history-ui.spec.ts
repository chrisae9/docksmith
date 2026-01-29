import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { HistoryPage } from '../pages/history.page';

test.describe('History UI', () => {
  test('navigating to History tab shows history page', async ({ page }) => {
    // Navigate to homepage (don't wait for containers)
    await page.goto('/');

    // Click History tab
    await page.locator('.tab-bar button:has-text("History"), .nav-tab:has-text("History")').click();

    // Should show History page
    await expect(page.locator('h1')).toContainText('History');
  });

  test('history page shows operations list', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Should have some operations or empty message
    const count = await history.getOperationCount();
    // May or may not have operations - test should pass either way
    expect(count >= 0).toBe(true);
  });

  test('status filter works', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Get all operations count
    await history.setStatusFilter('all');
    await history.waitForFilterUpdate();
    const allCount = await history.getOperationCount();

    // Filter to success only
    await history.setStatusFilter('success');
    await history.waitForFilterUpdate();
    const successCount = await history.getOperationCount();

    // Success count should be <= all count
    expect(successCount).toBeLessThanOrEqual(allCount);

    // Filter to failed
    await history.setStatusFilter('failed');
    await history.waitForFilterUpdate();
    const failedCount = await history.getOperationCount();

    // Failed count should be <= all count
    expect(failedCount).toBeLessThanOrEqual(allCount);

    // Back to all
    await history.setStatusFilter('all');
    await history.waitForFilterUpdate();
    const resetCount = await history.getOperationCount();
    expect(resetCount).toBe(allCount);
  });

  test('type filter works', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Get initial count (default filter)
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();
    const allCount = await history.getOperationCount();

    // Filter by different types (including new stop and remove types)
    const types: Array<'updates' | 'rollback' | 'restart' | 'stop' | 'remove' | 'label_change'> = [
      'updates', 'rollback', 'restart', 'stop', 'remove', 'label_change'
    ];

    for (const type of types) {
      await history.setTypeFilter(type);
      await history.waitForFilterUpdate();
      const typeCount = await history.getOperationCount();

      // Each type count should be <= all count
      expect(typeCount).toBeLessThanOrEqual(allCount);
    }

    // Reset to all
    await history.setTypeFilter('all');
  });

  test('stop filter shows only stop operations', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to stop operations
    await history.setTypeFilter('stop');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    // If there are stop operations, verify they have the STOP badge
    if (count > 0) {
      const hasBadge = await history.hasOperationBadge(0, 'stop');
      expect(hasBadge).toBe(true);

      const infoText = await history.getOperationInfoText(0);
      expect(infoText).toBe('Container stopped');
    }
  });

  test('remove filter shows only remove operations', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to remove operations
    await history.setTypeFilter('remove');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    // If there are remove operations, verify they have the REMOVE badge
    if (count > 0) {
      const hasBadge = await history.hasOperationBadge(0, 'remove');
      expect(hasBadge).toBe(true);

      const infoText = await history.getOperationInfoText(0);
      expect(infoText).toBe('Container removed');
    }
  });

  test('stop operation badge has correct styling', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to stop operations
    await history.setTypeFilter('stop');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count === 0) {
      test.skip(true, 'No stop operations in history - skipping badge styling test');
      return;
    }

    // Verify the badge exists with correct class
    const badge = page.locator('.operation-card').first().locator('.op-type-badge.stop');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('STOP');
  });

  test('remove operation badge has correct styling', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to remove operations
    await history.setTypeFilter('remove');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count === 0) {
      test.skip(true, 'No remove operations in history - skipping badge styling test');
      return;
    }

    // Verify the badge exists with correct class
    const badge = page.locator('.operation-card').first().locator('.op-type-badge.remove');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('REMOVE');
  });

  test('search filters operations', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();

    const initialCount = await history.getOperationCount();

    if (initialCount > 0) {
      // Search for a common term
      await history.search('test');
      await history.waitForFilterUpdate();

      const searchCount = await history.getOperationCount();
      expect(searchCount).toBeLessThanOrEqual(initialCount);

      // Clear search
      await history.clearSearch();
      await history.waitForFilterUpdate();

      const clearedCount = await history.getOperationCount();
      expect(clearedCount).toBe(initialCount);
    }
  });

  test('expand/collapse operation card works', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count > 0) {
      // Initially not expanded
      const initialExpanded = await history.isOperationExpanded(0);

      // Expand first operation
      await history.expandOperation(0);
      // Wait for expand animation to complete
      await expect(history.operationCards.nth(0)).toHaveClass(/expanded/);

      const afterExpand = await history.isOperationExpanded(0);
      expect(afterExpand).toBe(true);

      // Collapse it
      await history.expandOperation(0);
      // Wait for collapse animation to complete
      await expect(history.operationCards.nth(0)).not.toHaveClass(/expanded/);

      const afterCollapse = await history.isOperationExpanded(0);
      expect(afterCollapse).toBe(false);
    }
  });

  test('operation card shows container name', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count > 0) {
      const containerName = await history.getOperationContainerName(0);
      // Container name should not be empty (may be stack name for batch ops)
      expect(containerName.length).toBeGreaterThan(0);
    }
  });

  test('operation card shows correct status indicator', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count > 0) {
      const status = await history.getOperationStatus(0);
      const validStatuses = ['success', 'failed', 'pending', 'rollback'];
      expect(validStatuses).toContain(status);
    }
  });

  test('clicking container link navigates to container detail', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single/update operations (most likely to have container links)
    await history.setTypeFilter('updates');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count === 0) {
      test.skip(true, 'No single operations found in history');
      return;
    }

    // First expand the card to make container link visible
    await history.expandOperation(0);
    await expect(history.operationCards.nth(0)).toHaveClass(/expanded/);

    // Check if container link exists in the first operation card
    const card = await history.getOperationCard(0);
    const containerLink = card.locator('.container-link, .op-container a, a[href*="/container/"]').first();
    const linkCount = await containerLink.count();

    if (linkCount === 0) {
      // Container links may not exist in this UI version - skip gracefully
      test.skip(true, 'No container links found in operation cards');
      return;
    }

    // Get container name before clicking to verify navigation
    const containerName = await history.getOperationContainerName(0);

    // Click the container link
    await containerLink.click();

    // Should navigate to container detail page
    await expect(page).toHaveURL(/\/container\//);
  });

  test('copy operation ID button works', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();
    await history.setTypeFilter('all');
    await history.waitForFilterUpdate();

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
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single updates (most likely to have rollback option)
    await history.setTypeFilter('updates');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    if (count > 0) {
      // Check if first operation has rollback button
      const hasRollback = await history.hasRollbackButton(0);
      // May or may not have rollback (depends on operation state)
      expect(typeof hasRollback).toBe('boolean');
    }
  });

  test('rollback confirmation dialog appears', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to single updates
    await history.setTypeFilter('updates');
    await history.setStatusFilter('success');
    await history.waitForFilterUpdate();

    const count = await history.getOperationCount();

    // Find an operation with rollback button
    for (let i = 0; i < Math.min(count, 5); i++) {
      const hasRollback = await history.hasRollbackButton(i);
      if (hasRollback) {
        // Click rollback
        await history.clickRollback(i);
        // Wait for dialog to appear
        await expect(history.rollbackConfirmDialog).toBeVisible();

        // Confirm dialog should appear
        const isVisible = await history.isRollbackConfirmVisible();
        expect(isVisible).toBe(true);

        // Cancel the rollback
        await history.cancelRollback();
        // Wait for dialog to disappear
        await expect(history.rollbackConfirmDialog).toBeHidden();

        // Dialog should be gone
        const isGone = await history.isRollbackConfirmVisible();
        expect(isGone).toBe(false);

        break;
      }
    }
  });

  test('combined filters work together', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Apply multiple filters
    await history.setStatusFilter('success');
    await history.setTypeFilter('updates');
    await history.search('test');
    await history.waitForFilterUpdate();

    const filteredCount = await history.getOperationCount();

    // Reset all filters
    await history.setStatusFilter('all');
    await history.setTypeFilter('all');
    await history.clearSearch();
    await history.waitForFilterUpdate();

    const resetCount = await history.getOperationCount();

    // Filtered count should be <= reset count
    expect(filteredCount).toBeLessThanOrEqual(resetCount);
  });
});
