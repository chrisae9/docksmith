import { type Page, type Locator, expect } from '@playwright/test';

export class RestartDependenciesPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly pageTitle: Locator;
  readonly containerConfigCard: Locator;
  readonly infoCard: Locator;
  readonly selectionSummary: Locator;
  readonly summaryCount: Locator;
  readonly clearAllButton: Locator;
  readonly selectedTags: Locator;
  readonly searchInput: Locator;
  readonly clearSearchButton: Locator;
  readonly containerItems: Locator;
  readonly loadingSpinner: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly cancelButton: Locator;
  readonly doneButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.pageTitle = page.locator('.page-header h1');
    this.containerConfigCard = page.locator('.container-config-card');
    this.infoCard = page.locator('.info-card');
    this.selectionSummary = page.locator('.selection-summary');
    this.summaryCount = page.locator('.summary-count');
    this.clearAllButton = page.locator('.clear-all-button');
    this.selectedTags = page.locator('.selected-tag');
    this.searchInput = page.locator('.search-input');
    this.clearSearchButton = page.locator('.clear-search');
    // Container items are direct children of .containers-list, they have cursor:pointer
    this.containerItems = page.locator('.containers-list > div');
    // Loading state includes spinner and text
    this.loadingSpinner = page.locator('.loading-state');
    this.errorState = page.locator('.error-state');
    this.emptyState = page.locator('.empty-state');
    this.cancelButton = page.locator('.page-footer .button-secondary');
    this.doneButton = page.locator('.page-footer .button-primary');
  }

  async navigate(containerName: string) {
    await this.page.goto(`/container/${encodeURIComponent(containerName)}/restart-dependencies`);
    await this.waitForLoaded();
  }

  async waitForLoaded(timeout = 30000) {
    await expect(this.pageTitle).toHaveText('Restart Dependencies', { timeout });
    // Wait for BOTH loading states to disappear (container data + containers list)
    // Using text-based wait to be more specific
    await expect(this.page.getByText('Loading containers...')).toBeHidden({ timeout });
    // Wait for containers list to appear (more specific selector)
    await expect(this.page.locator('.containers-list')).toBeVisible({ timeout });
  }

  async search(query: string) {
    await this.searchInput.fill(query);
  }

  async clearSearch() {
    const isVisible = await this.clearSearchButton.isVisible();
    if (isVisible) {
      await this.clearSearchButton.click();
    } else {
      await this.searchInput.clear();
    }
  }

  async getContainerCount(): Promise<number> {
    return this.containerItems.count();
  }

  async selectContainer(containerName: string) {
    // Container items are generic elements with the container name text inside
    // Use text-based matching since there are no specific class names in accessibility tree
    const containerItem = this.page.locator('.containers-list > div').filter({ hasText: containerName }).first();
    await containerItem.click();
  }

  async deselectContainer(containerName: string) {
    // Click the item again to deselect, or click the X in the selected tags
    const tag = this.page.locator(`.selected-tag:has-text("${containerName}")`);
    const tagVisible = await tag.isVisible();
    if (tagVisible) {
      await tag.locator('button').click();
    } else {
      await this.selectContainer(containerName);
    }
  }

  async isContainerSelected(containerName: string): Promise<boolean> {
    // Check if container appears in selected tags
    const selectedTag = this.page.locator('.selected-tag').filter({ hasText: containerName });
    return selectedTag.count().then(count => count > 0);
  }

  async getSelectedCount(): Promise<number> {
    const isVisible = await this.summaryCount.isVisible();
    if (!isVisible) return 0;
    const text = await this.summaryCount.textContent();
    const match = text?.match(/(\d+)/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async getSelectedContainers(): Promise<string[]> {
    const count = await this.selectedTags.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      const tag = this.selectedTags.nth(i);
      const text = await tag.textContent();
      // Remove the X button text
      const name = text?.replace(/[\s×]/g, '').trim();
      if (name) names.push(name);
    }
    return names;
  }

  async clearAll() {
    const isVisible = await this.clearAllButton.isVisible();
    if (isVisible) {
      await this.clearAllButton.click();
    }
  }

  async hasError(): Promise<boolean> {
    return this.errorState.isVisible();
  }

  async getError(): Promise<string | null> {
    if (!(await this.hasError())) return null;
    const text = await this.errorState.locator('.error-text').textContent();
    return text?.trim() || null;
  }

  async hasEmptyState(): Promise<boolean> {
    return this.emptyState.isVisible();
  }

  async clickCancel() {
    await this.cancelButton.click();
  }

  async clickDone() {
    await this.doneButton.click();
  }

  async isDoneDisabled(): Promise<boolean> {
    return this.doneButton.isDisabled();
  }

  async getDoneButtonText(): Promise<string> {
    const text = await this.doneButton.textContent();
    return text?.trim() || '';
  }

  async clickBack() {
    await this.backButton.click();
  }
}
