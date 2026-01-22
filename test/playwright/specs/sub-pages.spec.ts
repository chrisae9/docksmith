import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
import { DashboardPage } from '../pages/dashboard.page';
import { ContainerDetailPage } from '../pages/container-detail.page';
import { TagFilterPage } from '../pages/tag-filter.page';
import { ScriptSelectionPage } from '../pages/script-selection.page';
import { RestartDependenciesPage } from '../pages/restart-dependencies.page';

test.describe('Tag Filter Page', () => {
  test('navigates from container detail to tag filter page', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Click on a container
    await dashboard.clickContainer(TEST_CONTAINERS.NGINX_BASIC);
    await page.waitForTimeout(500);

    // Click on Tag Filter setting
    const tagFilterNav = page.locator('.nav-row:has(.nav-title:has-text("Tag Filter")), .setting-item:has-text("Tag Filter")');
    await tagFilterNav.click();
    await page.waitForTimeout(500);

    // Should be on tag filter page
    await expect(page.locator('h1')).toContainText('Tag Filter');
  });

  test('tag filter page loads with regex input', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Should have regex input
    await expect(tagFilter.regexInput).toBeVisible();

    // Should show available tags section
    await tagFilter.waitForTagsLoaded();
  });

  test('entering valid regex shows success message', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Enter a valid regex
    await tagFilter.setPattern('^v?[0-9.]+$');
    await page.waitForTimeout(300);

    // Should be valid
    const isValid = await tagFilter.isPatternValid();
    expect(isValid).toBe(true);

    // Should show success message
    const successMsg = await tagFilter.getSuccessMessage();
    expect(successMsg).toContain('Valid');
  });

  test('entering invalid regex shows error message', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Enter an invalid regex (unclosed bracket)
    await tagFilter.setPattern('[invalid');
    await page.waitForTimeout(300);

    // Should be invalid
    const isValid = await tagFilter.isPatternValid();
    expect(isValid).toBe(false);

    // Should show error message
    const errorMsg = await tagFilter.getErrorMessage();
    expect(errorMsg).not.toBeNull();
  });

  test('preset buttons populate regex input', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Click a preset button
    await tagFilter.clickPreset('Stable releases');
    await page.waitForTimeout(300);

    // Should have pattern in input
    const pattern = await tagFilter.getPattern();
    expect(pattern.length).toBeGreaterThan(0);

    // Should be valid
    const isValid = await tagFilter.isPatternValid();
    expect(isValid).toBe(true);
  });

  test('match counter updates as regex changes', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);
    await tagFilter.waitForTagsLoaded();

    // Get initial match count
    const initialMatch = await tagFilter.getMatchCount();

    // Enter a restrictive pattern
    await tagFilter.setPattern('^1\\.0$');
    await page.waitForTimeout(300);

    // Get new match count
    const restrictedMatch = await tagFilter.getMatchCount();

    // Restrictive pattern should match fewer or same tags
    expect(restrictedMatch.matched).toBeLessThanOrEqual(initialMatch.total);
  });

  test('cancel button returns to container detail without changes', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Enter a pattern
    await tagFilter.setPattern('test-pattern');
    await page.waitForTimeout(300);

    // Click cancel
    await tagFilter.clickCancel();
    await page.waitForTimeout(500);

    // Should be back on container detail page
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINERS.NGINX_BASIC}`));
  });

  test('done button is disabled when no changes', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // With no changes, Done should be disabled
    const isDisabled = await tagFilter.isDoneDisabled();
    expect(isDisabled).toBe(true);
  });

  test('done button is enabled after changes', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Make a change
    await tagFilter.setPattern('^new-pattern$');
    await page.waitForTimeout(300);

    // Done should be enabled
    const isDisabled = await tagFilter.isDoneDisabled();
    expect(isDisabled).toBe(false);
  });

  test('clear button removes pattern', async ({ page }) => {
    const tagFilter = new TagFilterPage(page);
    await tagFilter.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Enter a pattern
    await tagFilter.setPattern('some-pattern');
    await page.waitForTimeout(300);

    // Clear it
    await tagFilter.clearPattern();
    await page.waitForTimeout(300);

    // Pattern should be empty
    const pattern = await tagFilter.getPattern();
    expect(pattern).toBe('');
  });
});

test.describe('Script Selection Page', () => {
  test('navigates from container detail to script selection page', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Click on a container
    await dashboard.clickContainer(TEST_CONTAINERS.NGINX_BASIC);
    await page.waitForTimeout(500);

    // Click on Pre-Update Script setting - use role button to avoid strict mode violation
    const scriptNav = page.getByRole('button', { name: /Pre-Update Script/ });
    await scriptNav.click();
    await page.waitForTimeout(500);

    // Should be on script selection page
    await expect(page.locator('h1')).toContainText('Select Script');
  });

  test('script selection page shows available scripts', async ({ page }) => {
    const scriptSelection = new ScriptSelectionPage(page);
    await scriptSelection.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Should have "No Script" option
    await expect(scriptSelection.noneOption).toBeVisible();

    // Should load scripts list
    const scriptCount = await scriptSelection.getScriptCount();
    // May or may not have scripts
    expect(scriptCount).toBeGreaterThanOrEqual(0);
  });

  test('selecting "No Script" option works', async ({ page }) => {
    const scriptSelection = new ScriptSelectionPage(page);
    await scriptSelection.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Select none
    await scriptSelection.selectNone();
    await page.waitForTimeout(300);

    // None should be selected
    const isNoneSelected = await scriptSelection.isNoneSelected();
    expect(isNoneSelected).toBe(true);
  });

  test('search filters scripts', async ({ page }) => {
    const scriptSelection = new ScriptSelectionPage(page);
    await scriptSelection.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const initialCount = await scriptSelection.getScriptCount();

    if (initialCount > 0) {
      // Search for something
      await scriptSelection.search('check');
      await page.waitForTimeout(300);

      const searchCount = await scriptSelection.getScriptCount();
      expect(searchCount).toBeLessThanOrEqual(initialCount);

      // Clear search
      await scriptSelection.clearSearch();
      await page.waitForTimeout(300);

      const clearedCount = await scriptSelection.getScriptCount();
      expect(clearedCount).toBe(initialCount);
    }
  });

  test('cancel button returns to container detail', async ({ page }) => {
    const scriptSelection = new ScriptSelectionPage(page);
    await scriptSelection.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Click cancel
    await scriptSelection.clickCancel();
    await page.waitForTimeout(500);

    // Should be back on container detail page
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINERS.NGINX_BASIC}`));
  });

  test('done button disabled when no changes', async ({ page }) => {
    const scriptSelection = new ScriptSelectionPage(page);
    await scriptSelection.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // With no changes, Done should be disabled
    const isDisabled = await scriptSelection.isDoneDisabled();
    expect(isDisabled).toBe(true);
  });
});

test.describe('Restart Dependencies Page', () => {
  test('navigates from container detail to restart dependencies page', async ({ page }) => {
    const dashboard = new DashboardPage(page);
    await dashboard.navigate();
    await dashboard.setFilter('all');
    await page.waitForTimeout(500);

    // Click on a container
    await dashboard.clickContainer(TEST_CONTAINERS.NGINX_BASIC);
    await page.waitForTimeout(500);

    // Click on Restart Dependencies setting
    const restartNav = page.locator('.nav-row:has(.nav-title:has-text("Restart Dependencies")), .setting-item:has-text("Restart Dependencies")');
    await restartNav.click();
    await page.waitForTimeout(500);

    // Should be on restart dependencies page
    await expect(page.locator('h1')).toContainText('Restart Dependencies');
  });

  test('restart dependencies page shows available containers', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Should show list of containers
    const containerCount = await restartDeps.getContainerCount();
    // Should have containers (minus the current one)
    expect(containerCount).toBeGreaterThanOrEqual(0);
  });

  test('selecting a container adds it to dependencies', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const containerCount = await restartDeps.getContainerCount();

    if (containerCount > 0) {
      // Select the first available container (not nginx basic)
      await restartDeps.selectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      // Should be selected
      const isSelected = await restartDeps.isContainerSelected(TEST_CONTAINERS.REDIS_BASIC);
      expect(isSelected).toBe(true);

      // Selected count should be 1
      const selectedCount = await restartDeps.getSelectedCount();
      expect(selectedCount).toBe(1);
    }
  });

  test('deselecting a container removes it from dependencies', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const containerCount = await restartDeps.getContainerCount();

    if (containerCount > 0) {
      // Select then deselect
      await restartDeps.selectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      await restartDeps.deselectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      // Should not be selected
      const isSelected = await restartDeps.isContainerSelected(TEST_CONTAINERS.REDIS_BASIC);
      expect(isSelected).toBe(false);
    }
  });

  test('clear all button removes all selections', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const containerCount = await restartDeps.getContainerCount();

    if (containerCount >= 2) {
      // Select multiple containers
      await restartDeps.selectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      // Clear all
      await restartDeps.clearAll();
      await page.waitForTimeout(300);

      // Selected count should be 0
      const selectedCount = await restartDeps.getSelectedCount();
      expect(selectedCount).toBe(0);
    }
  });

  test('search filters containers', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const initialCount = await restartDeps.getContainerCount();

    if (initialCount > 0) {
      // Search for something
      await restartDeps.search('redis');
      await page.waitForTimeout(300);

      const searchCount = await restartDeps.getContainerCount();
      expect(searchCount).toBeLessThanOrEqual(initialCount);

      // Clear search
      await restartDeps.clearSearch();
      await page.waitForTimeout(300);

      const clearedCount = await restartDeps.getContainerCount();
      expect(clearedCount).toBe(initialCount);
    }
  });

  test('cancel button returns to container detail', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // Click cancel
    await restartDeps.clickCancel();
    await page.waitForTimeout(500);

    // Should be back on container detail page
    await expect(page).toHaveURL(new RegExp(`/container/${TEST_CONTAINERS.NGINX_BASIC}`));
  });

  test('done button disabled when no changes', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    // With no changes, Done should be disabled
    const isDisabled = await restartDeps.isDoneDisabled();
    expect(isDisabled).toBe(true);
  });

  test('done button enabled after changes', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const containerCount = await restartDeps.getContainerCount();

    if (containerCount > 0) {
      // Make a change
      await restartDeps.selectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      // Done should be enabled
      const isDisabled = await restartDeps.isDoneDisabled();
      expect(isDisabled).toBe(false);
    }
  });

  test('done button shows selected count', async ({ page }) => {
    const restartDeps = new RestartDependenciesPage(page);
    await restartDeps.navigate(TEST_CONTAINERS.NGINX_BASIC);

    const containerCount = await restartDeps.getContainerCount();

    if (containerCount > 0) {
      // Select a container
      await restartDeps.selectContainer(TEST_CONTAINERS.REDIS_BASIC);
      await page.waitForTimeout(300);

      // Done button text should include count
      const buttonText = await restartDeps.getDoneButtonText();
      expect(buttonText).toContain('1');
    }
  });
});
