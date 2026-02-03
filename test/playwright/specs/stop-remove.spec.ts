/**
 * Stop and Remove Operations UI Tests
 *
 * These tests validate the UI display of stop and remove operations in History.
 * Container operations are tested via API integration tests (test-api.sh).
 * This spec focuses on verifying the UI correctly displays stop/remove operations.
 *
 * Run with: npm test -- specs/stop-remove.spec.ts
 */

import { test, expect } from '../fixtures/test-setup';
import { HistoryPage } from '../pages/history.page';

test.describe('Stop and Remove Operations UI', () => {
  test('stop operations are accessible via API', async ({ api }) => {
    // Verify the API returns stop operations correctly
    const operations = await api.getOperations(50);
    expect(operations.success).toBe(true);

    // Find any stop operations in history
    const stopOps = operations.data?.operations.filter(
      op => op.operation_type === 'stop'
    ) || [];

    // If there are stop operations, verify their structure
    if (stopOps.length > 0) {
      const stopOp = stopOps[0];
      expect(stopOp.operation_type).toBe('stop');
      expect(stopOp.operation_id).toBeDefined();
      expect(['complete', 'failed', 'pending']).toContain(stopOp.status);
    }
  });

  test('remove operations are accessible via API', async ({ api }) => {
    // Verify the API returns remove operations correctly
    const operations = await api.getOperations(50);
    expect(operations.success).toBe(true);

    // Find any remove operations in history
    const removeOps = operations.data?.operations.filter(
      op => op.operation_type === 'remove'
    ) || [];

    // If there are remove operations, verify their structure
    if (removeOps.length > 0) {
      const removeOp = removeOps[0];
      expect(removeOp.operation_type).toBe('remove');
      expect(removeOp.operation_id).toBeDefined();
      expect(['complete', 'failed', 'pending']).toContain(removeOp.status);
    }
  });

  test('History page shows stop filter option', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Verify the type filter dropdown has the stop option
    const options = await history.typeFilterSelect.locator('option').allTextContents();
    const hasStopOption = options.some(opt => opt.toLowerCase().includes('stop'));
    expect(hasStopOption).toBe(true);
  });

  test('History page shows remove filter option', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Verify the type filter dropdown has the remove option
    const options = await history.typeFilterSelect.locator('option').allTextContents();
    const hasRemoveOption = options.some(opt => opt.toLowerCase().includes('remov'));
    expect(hasRemoveOption).toBe(true);
  });

  test('stop filter shows stop operations with STOP badge', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to stop operations
    await history.setTypeFilter('stop');
    await page.waitForTimeout(500);

    const count = await history.getOperationCount();

    // If there are stop operations, verify the badge
    if (count > 0) {
      const hasBadge = await history.hasOperationBadge(0, 'stop');
      expect(hasBadge).toBe(true);

      const infoText = await history.getOperationInfoText(0);
      expect(infoText).toBe('Container stopped');
    }
  });

  test('remove filter shows remove operations with REMOVE badge', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to remove operations
    await history.setTypeFilter('remove');
    await page.waitForTimeout(500);

    const count = await history.getOperationCount();

    // If there are remove operations, verify the badge
    if (count > 0) {
      const hasBadge = await history.hasOperationBadge(0, 'remove');
      expect(hasBadge).toBe(true);

      const infoText = await history.getOperationInfoText(0);
      expect(infoText).toBe('Container removed');
    }
  });

  test('stop operations show correct details when expanded', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to stop operations
    await history.setTypeFilter('stop');
    await page.waitForTimeout(500);

    const count = await history.getOperationCount();

    if (count === 0) {
      test.skip(true, 'No stop operations in history');
      return;
    }

    // Expand the first operation
    await history.expandOperation(0);
    await page.waitForTimeout(300);

    // Verify expanded state
    const isExpanded = await history.isOperationExpanded(0);
    expect(isExpanded).toBe(true);

    // Check that type shows as 'stop' in details
    const card = await history.getOperationCard(0);
    const typeText = await card.locator('text=stop').first().isVisible();
    expect(typeText).toBe(true);
  });

  test('remove operations show correct details when expanded', async ({ page }) => {
    // Navigate directly to history page
    await page.goto('/history');

    const history = new HistoryPage(page);
    await history.waitForLoaded();

    // Filter to remove operations
    await history.setTypeFilter('remove');
    await page.waitForTimeout(500);

    const count = await history.getOperationCount();

    if (count === 0) {
      test.skip(true, 'No remove operations in history');
      return;
    }

    // Expand the first operation
    await history.expandOperation(0);
    await page.waitForTimeout(300);

    // Verify expanded state
    const isExpanded = await history.isOperationExpanded(0);
    expect(isExpanded).toBe(true);

    // Check that type shows as 'remove' in details
    const card = await history.getOperationCard(0);
    const typeText = await card.locator('text=remove').first().isVisible();
    expect(typeText).toBe(true);
  });
});
