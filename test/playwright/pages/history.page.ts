import { type Page, type Locator, expect } from '@playwright/test';

export class HistoryPage {
  readonly page: Page;
  readonly searchInput: Locator;
  readonly statusFilterAll: Locator;
  readonly statusFilterSuccess: Locator;
  readonly statusFilterFailed: Locator;
  readonly typeFilterSelect: Locator;
  readonly operationCards: Locator;
  readonly loadingSpinner: Locator;
  readonly emptyMessage: Locator;
  readonly rollbackConfirmDialog: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.locator('.search-input');
    this.statusFilterAll = page.locator('.segmented-control button:has-text("All")');
    this.statusFilterSuccess = page.locator('.segmented-control button:has-text("Success")');
    this.statusFilterFailed = page.locator('.segmented-control button:has-text("Failed")');
    this.typeFilterSelect = page.locator('.type-filter-select');
    this.operationCards = page.locator('.operation-card');
    this.loadingSpinner = page.locator('.spinner, .loading');
    this.emptyMessage = page.locator('.empty');
    this.rollbackConfirmDialog = page.locator('.confirm-dialog');
  }

  async waitForLoaded(timeout = 30000) {
    // Wait for page title to show History
    await expect(this.page.locator('h1')).toContainText('History', { timeout });
    // Wait for loading to complete - either operations or empty message
    await this.page.waitForFunction(() => {
      const ops = document.querySelectorAll('.operation-card');
      const empty = document.querySelector('.empty');
      return ops.length > 0 || empty !== null;
    }, { timeout });
  }

  async getOperationCount(): Promise<number> {
    return this.operationCards.count();
  }

  async search(query: string) {
    await this.searchInput.fill(query);
  }

  async clearSearch() {
    await this.searchInput.clear();
  }

  async setStatusFilter(filter: 'all' | 'success' | 'failed') {
    switch (filter) {
      case 'all':
        await this.statusFilterAll.click();
        break;
      case 'success':
        await this.statusFilterSuccess.click();
        break;
      case 'failed':
        await this.statusFilterFailed.click();
        break;
    }
  }

  async setTypeFilter(type: 'all' | 'single' | 'batch' | 'stack' | 'rollback' | 'restart' | 'label_change') {
    await this.typeFilterSelect.selectOption(type);
  }

  async getOperationCard(index: number): Promise<Locator> {
    return this.operationCards.nth(index);
  }

  async expandOperation(index: number) {
    const card = this.operationCards.nth(index);
    await card.click();
  }

  async isOperationExpanded(index: number): Promise<boolean> {
    const card = this.operationCards.nth(index);
    const classes = await card.getAttribute('class');
    return classes?.includes('expanded') || false;
  }

  async getOperationContainerName(index: number): Promise<string> {
    const card = this.operationCards.nth(index);
    const container = card.locator('.op-container');
    const text = await container.textContent();
    return text?.trim() || '';
  }

  async getOperationStatus(index: number): Promise<string> {
    const card = this.operationCards.nth(index);
    const classes = await card.getAttribute('class');
    if (classes?.includes('status-success')) return 'success';
    if (classes?.includes('status-failed')) return 'failed';
    if (classes?.includes('status-rollback')) return 'rollback';
    return 'pending';
  }

  async getOperationType(index: number): Promise<string | null> {
    const card = this.operationCards.nth(index);
    const typeBadge = card.locator('.op-type-badge');
    const count = await typeBadge.count();
    if (count === 0) return null;
    const text = await typeBadge.first().textContent();
    return text?.trim().toLowerCase() || null;
  }

  async getOperationVersion(index: number): Promise<string | null> {
    const card = this.operationCards.nth(index);
    const version = card.locator('.op-version');
    const count = await version.count();
    if (count === 0) return null;
    const text = await version.textContent();
    return text?.trim() || null;
  }

  async copyOperationId(index: number) {
    const card = this.operationCards.nth(index);
    const copyBtn = card.locator('.op-copy-btn');
    await copyBtn.click();
  }

  async clickRollback(index: number) {
    // First expand the card if not already
    if (!(await this.isOperationExpanded(index))) {
      await this.expandOperation(index);
    }
    const card = this.operationCards.nth(index);
    const rollbackBtn = card.locator('.rollback-btn');
    await rollbackBtn.click();
  }

  async hasRollbackButton(index: number): Promise<boolean> {
    // First expand the card if not already
    if (!(await this.isOperationExpanded(index))) {
      await this.expandOperation(index);
    }
    const card = this.operationCards.nth(index);
    const rollbackBtn = card.locator('.rollback-btn');
    return rollbackBtn.isVisible();
  }

  // Rollback confirmation dialog
  async isRollbackConfirmVisible(): Promise<boolean> {
    return this.rollbackConfirmDialog.isVisible();
  }

  async confirmRollback() {
    await this.rollbackConfirmDialog.locator('.confirm-proceed').click();
  }

  async confirmForceRollback() {
    await this.rollbackConfirmDialog.locator('.confirm-force').click();
  }

  async cancelRollback() {
    await this.rollbackConfirmDialog.locator('.confirm-cancel').click();
  }

  async clickContainerLink(index: number) {
    const card = this.operationCards.nth(index);
    const link = card.locator('.container-link').first();
    await link.click();
  }
}
