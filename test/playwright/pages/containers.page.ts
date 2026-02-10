import { type Page, type Locator, expect } from '@playwright/test';

export class ContainersPage {
  readonly page: Page;
  readonly searchInput: Locator;
  readonly selectAllButton: Locator;
  readonly filterButtons: Locator;
  readonly containerRows: Locator;
  readonly loadingState: Locator;
  readonly skeletonLoader: Locator;
  readonly selectionBar: Locator;
  readonly actionsButton: Locator;
  readonly bulkActionsMenu: Locator;
  readonly subTabButtons: Locator;
  readonly blockedExcludedBanner: Locator;
  readonly includeBlockedButton: Locator;
  readonly forceUpdateWarning: Locator;
  readonly updateTypeBadges: Locator;
  readonly updateButton: Locator;
  readonly forceUpdateButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.locator('input[placeholder*="Search"]');
    this.selectAllButton = page.locator('.select-all-btn');
    this.filterButtons = page.locator('.filter-toolbar .segmented-control button');
    // Container rows use CSS grid with .unified-row class
    this.containerRows = page.locator('.unified-row');
    // Loading states
    this.loadingState = page.locator('.main-loading, .skeleton-dashboard');
    this.skeletonLoader = page.locator('.skeleton-dashboard');
    // Selection bar at bottom
    this.selectionBar = page.locator('.selection-bar');
    // Actions dropdown button in selection bar
    this.actionsButton = this.selectionBar.locator('.actions-dropdown-btn');
    this.bulkActionsMenu = page.locator('.bulk-actions-menu');
    // Sub-tab buttons (Containers | Images | Networks | Volumes)
    this.subTabButtons = page.locator('.explorer-tabs button');
    // Smart selection bar elements
    this.blockedExcludedBanner = this.selectionBar.locator('.blocked-excluded-info');
    this.includeBlockedButton = this.selectionBar.locator('.include-blocked-btn');
    this.forceUpdateWarning = this.selectionBar.locator('.force-update-warning');
    this.updateTypeBadges = this.selectionBar.locator('.update-type-badges');
    this.updateButton = this.selectionBar.locator('.update-btn');
    this.forceUpdateButton = this.selectionBar.locator('.update-btn.force');
  }

  async navigate() {
    await this.page.goto('/');
    await this.waitForContainers();
  }

  async waitForContainers(timeout = 30000) {
    // Wait for skeleton to disappear
    await expect(this.skeletonLoader).toBeHidden({ timeout });
    // Wait for at least one container row to appear
    await expect(this.containerRows.first()).toBeVisible({ timeout });
  }

  async getContainerByName(name: string): Promise<Locator> {
    // In the new grid layout, the row is a .unified-row <li> containing the name
    return this.containerRows.filter({ hasText: name });
  }

  async clickContainer(name: string) {
    const row = await this.getContainerByName(name);
    // Click the .row-link zone (middle column) which handles navigation
    await row.locator('.row-link').click();
  }

  async triggerRefresh() {
    await this.page.evaluate(() => fetch('/api/trigger-check', { method: 'POST' }));
    await this.page.waitForTimeout(500);
  }

  async selectContainer(name: string) {
    const row = await this.getContainerByName(name);
    // Click the .checkbox-zone label which toggles selection
    const checkbox = row.locator('.checkbox-zone');
    await checkbox.click();
  }

  async deselectContainer(name: string) {
    const row = await this.getContainerByName(name);
    const checkboxInput = row.locator('input[type="checkbox"]');
    const isChecked = await checkboxInput.isChecked();
    if (isChecked) {
      // Click the checkbox zone to deselect
      await row.locator('.checkbox-zone').click();
    }
  }

  async selectContainers(names: string[]) {
    for (const name of names) {
      await this.selectContainer(name);
    }
  }

  async clickSelectAll() {
    await this.selectAllButton.click();
  }

  async clickActions() {
    await this.actionsButton.click();
  }

  async getContainerStatus(name: string): Promise<string> {
    const row = await this.getContainerByName(name);
    // Status badge is inside .row-link as a .status-badge span with title attribute
    const badge = row.locator('.status-badge');
    if (await badge.count() > 0) {
      const title = await badge.first().getAttribute('title');
      return title || await badge.first().textContent() || '';
    }
    return '';
  }

  async isContainerVisible(name: string): Promise<boolean> {
    const row = await this.getContainerByName(name);
    return row.isVisible();
  }

  async getContainerCount(): Promise<number> {
    return this.containerRows.count();
  }

  async setFilter(filter: 'all' | 'updates') {
    const filterText = filter.charAt(0).toUpperCase() + filter.slice(1);
    const filterButton = this.page.locator('.filter-toolbar .segmented-control').getByRole('button', { name: filterText, exact: true });
    await filterButton.click();
  }

  async search(query: string) {
    await this.searchInput.fill(query);
  }

  async clearSearch() {
    await this.searchInput.clear();
  }

  async clickTab(tabName: 'Containers' | 'History' | 'Settings') {
    await this.page.locator(`.tab-bar button:has-text("${tabName}"), .nav-tab:has-text("${tabName}")`).click();
  }

  async clickSubTab(tabName: 'Containers' | 'Images' | 'Networks' | 'Volumes') {
    await this.subTabButtons.filter({ hasText: tabName }).click();
  }

  async getSelectedCount(): Promise<number> {
    // Try plain "{N} selected" text in .selection-summary first
    const summarySpan = this.selectionBar.locator('.selection-summary > span');
    if (await summarySpan.count() > 0) {
      const text = await summarySpan.first().textContent();
      const match = text?.match(/(\d+)/);
      if (match) return parseInt(match[1], 10);
    }
    // Fall back to summing badge counts from .update-type-badge spans
    const badges = this.selectionBar.locator('.update-type-badge');
    const count = await badges.count();
    if (count > 0) {
      let total = 0;
      for (let i = 0; i < count; i++) {
        const text = await badges.nth(i).textContent();
        const match = text?.match(/(\d+)/);
        if (match) total += parseInt(match[1], 10);
      }
      return total;
    }
    return 0;
  }

  async clickCancel() {
    await this.selectionBar.locator('.cancel-btn').click();
  }

  async openBulkActions() {
    await this.actionsButton.click();
  }

  async clickBulkAction(actionText: string) {
    await this.bulkActionsMenu.getByRole('button', { name: actionText }).click();
  }

  async getUpdateTypeBadges(): Promise<{ patch: number; minor: number; major: number; rebuild: number; blocked: number; mismatch: number }> {
    const result = { patch: 0, minor: 0, major: 0, rebuild: 0, blocked: 0, mismatch: 0 };
    const badges = this.selectionBar.locator('.update-type-badge');
    const count = await badges.count();
    for (let i = 0; i < count; i++) {
      const text = (await badges.nth(i).textContent()) || '';
      const numMatch = text.match(/(\d+)/);
      const num = numMatch ? parseInt(numMatch[1], 10) : 0;
      const lower = text.toLowerCase();
      if (lower.includes('patch')) result.patch = num;
      else if (lower.includes('minor')) result.minor = num;
      else if (lower.includes('major')) result.major = num;
      else if (lower.includes('rebuild')) result.rebuild = num;
      else if (lower.includes('blocked')) result.blocked = num;
      else if (lower.includes('mismatch')) result.mismatch = num;
    }
    return result;
  }

  async isBlockedExcludedBannerVisible(): Promise<boolean> {
    return this.blockedExcludedBanner.isVisible();
  }

  async clickIncludeBlocked() {
    await this.includeBlockedButton.click();
  }

  async isForceUpdateWarningVisible(): Promise<boolean> {
    return this.forceUpdateWarning.isVisible();
  }

  async isForceUpdateButton(): Promise<boolean> {
    return this.forceUpdateButton.isVisible();
  }

  async getBlockedExcludedCount(): Promise<number> {
    const text = await this.blockedExcludedBanner.textContent();
    const match = text?.match(/(\d+)/);
    return match ? parseInt(match[1], 10) : 0;
  }
}

// Re-export with old name for backward compatibility during migration
export { ContainersPage as DashboardPage };
