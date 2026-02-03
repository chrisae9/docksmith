import { type Page, type Locator, expect } from '@playwright/test';

export class DashboardPage {
  readonly page: Page;
  readonly searchInput: Locator;
  readonly selectAllButton: Locator;
  readonly updateButton: Locator;
  readonly filterButtons: Locator;
  readonly containerList: Locator;
  readonly loadingState: Locator;
  readonly skeletonLoader: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.locator('input[placeholder*="Search"]');
    // Select All/Deselect All button in header
    this.selectAllButton = page.locator('.select-all-btn');
    // Update button in selection bar at bottom
    this.updateButton = page.locator('.selection-bar .update-btn');
    // Filter buttons inside segmented control
    this.filterButtons = page.locator('.segmented-control button');
    // Container list items - buttons inside list elements
    this.containerList = page.getByRole('list').getByRole('button');
    // Loading states
    this.loadingState = page.locator('.main-loading');
    this.skeletonLoader = page.locator('.skeleton-dashboard');
  }

  async navigate() {
    await this.page.goto('/');
    await this.waitForContainers();
  }

  async waitForContainers(timeout = 30000) {
    // Wait for loading state to disappear
    await expect(this.loadingState).toBeHidden({ timeout });
    // Wait for at least one container to appear
    await expect(this.containerList.first()).toBeVisible({ timeout });
  }

  async getContainerByName(name: string): Promise<Locator> {
    // Find button inside list containing the container name
    return this.page.getByRole('list').getByRole('button').filter({ hasText: name });
  }

  async clickContainer(name: string) {
    const container = await this.getContainerByName(name);
    await container.click();
  }

  async triggerRefresh() {
    // Dashboard uses pull-to-refresh, no button. Trigger via API instead.
    await this.page.evaluate(() => fetch('/api/trigger-check', { method: 'POST' }));
    // Wait a bit for the refresh to start
    await this.page.waitForTimeout(500);
  }

  async selectContainer(name: string) {
    const container = await this.getContainerByName(name);
    const checkbox = container.locator('input[type="checkbox"]');
    await checkbox.check();
  }

  async deselectContainer(name: string) {
    const container = await this.getContainerByName(name);
    const checkbox = container.locator('input[type="checkbox"]');
    // Force uncheck to bypass selection bar overlay that may intercept clicks
    await checkbox.uncheck({ force: true });
  }

  async selectContainers(names: string[]) {
    for (const name of names) {
      await this.selectContainer(name);
    }
  }

  async clickUpdate() {
    await this.updateButton.click();
  }

  async clickSelectAll() {
    await this.selectAllButton.click();
  }

  async getContainerStatus(name: string): Promise<string> {
    const container = await this.getContainerByName(name);
    // Status is shown in a generic element with aria-label/title like "Up to date", "Minor update", etc.
    // Look for common status text patterns
    const statusPatterns = ['Up to date', 'Update', 'Ignored', 'Local', 'Blocked', 'Pinnable'];
    for (const pattern of statusPatterns) {
      const statusElement = container.getByTitle(new RegExp(pattern, 'i'));
      if (await statusElement.count() > 0) {
        const title = await statusElement.getAttribute('title');
        return title || pattern;
      }
    }
    // Fallback - try to find any element with status text
    const statusLocator = container.locator('[title]').last();
    const title = await statusLocator.getAttribute('title');
    return title?.trim() || '';
  }

  async isContainerVisible(name: string): Promise<boolean> {
    const container = await this.getContainerByName(name);
    return container.isVisible();
  }

  async getContainerCount(): Promise<number> {
    return this.containerList.count();
  }

  async setFilter(filter: 'all' | 'updates' | 'local') {
    // Use exact text matching within segmented control to avoid matching "Select All"
    const filterText = filter.charAt(0).toUpperCase() + filter.slice(1); // Capitalize: "All", "Updates", "Local"
    const filterButton = this.page.locator('.segmented-control').getByRole('button', { name: filterText, exact: true });
    await filterButton.click();
  }

  async search(query: string) {
    await this.searchInput.fill(query);
  }

  async clearSearch() {
    await this.searchInput.clear();
  }

  async clickTab(tabName: 'Updates' | 'History' | 'Settings') {
    await this.page.locator(`.tab-bar button:has-text("${tabName}"), .nav-tab:has-text("${tabName}")`).click();
  }
}
