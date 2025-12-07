import { type Page, type Locator, expect } from '@playwright/test';

export class DashboardPage {
  readonly page: Page;
  readonly searchInput: Locator;
  readonly refreshButton: Locator;
  readonly updateAllButton: Locator;
  readonly filterButtons: Locator;
  readonly containerList: Locator;
  readonly loadingSpinner: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.locator('input[placeholder*="Search"]');
    this.refreshButton = page.locator('button[title*="Refresh"], button:has-text("Refresh")');
    this.updateAllButton = page.locator('button:has-text("Update")');
    this.filterButtons = page.locator('.segmented-control button, button:has-text("All"), button:has-text("Updates")');
    // Container list items - use getByRole for accessibility tree matching
    this.containerList = page.getByRole('listitem');
    this.loadingSpinner = page.locator('.spinner, .loading, [class*="loading"]');
  }

  async navigate() {
    await this.page.goto('/');
    await this.waitForContainers();
  }

  async waitForContainers(timeout = 30000) {
    // Wait for loading to finish
    await expect(this.loadingSpinner).toBeHidden({ timeout });
    // Wait for at least one container to appear
    await expect(this.containerList.first()).toBeVisible({ timeout });
  }

  async getContainerByName(name: string): Promise<Locator> {
    // Find listitem containing the container name
    return this.page.getByRole('listitem').filter({ hasText: name });
  }

  async clickContainer(name: string) {
    const container = await this.getContainerByName(name);
    await container.click();
  }

  async clickRefresh() {
    await this.refreshButton.click();
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
    await checkbox.uncheck();
  }

  async selectContainers(names: string[]) {
    for (const name of names) {
      await this.selectContainer(name);
    }
  }

  async clickUpdateAll() {
    await this.updateAllButton.click();
  }

  async getContainerStatus(name: string): Promise<string> {
    const container = await this.getContainerByName(name);
    const statusBadge = container.locator('.status-badge, .badge');
    const text = await statusBadge.textContent();
    return text?.trim() || '';
  }

  async isContainerVisible(name: string): Promise<boolean> {
    const container = await this.getContainerByName(name);
    return container.isVisible();
  }

  async getContainerCount(): Promise<number> {
    return this.containerList.count();
  }

  async setFilter(filter: 'all' | 'updates' | 'local') {
    const filterButton = this.page.locator(`button:has-text("${filter}"), .segment:has-text("${filter}")`);
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
