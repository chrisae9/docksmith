import { type Page, type Locator, expect } from '@playwright/test';

export class TagFilterPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly pageTitle: Locator;
  readonly containerConfigCard: Locator;
  readonly regexInput: Locator;
  readonly clearInputButton: Locator;
  readonly errorMessage: Locator;
  readonly successMessage: Locator;
  readonly presetButtons: Locator;
  readonly tagsList: Locator;
  readonly matchCounter: Locator;
  readonly loadingSpinner: Locator;
  readonly cancelButton: Locator;
  readonly doneButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.pageTitle = page.locator('.page-header h1');
    this.containerConfigCard = page.locator('.container-config-card');
    this.regexInput = page.locator('.regex-input');
    this.clearInputButton = page.locator('.clear-input-button');
    this.errorMessage = page.locator('.error-message');
    this.successMessage = page.locator('.success-message');
    this.presetButtons = page.locator('.preset-button');
    this.tagsList = page.locator('.tags-list');
    this.matchCounter = page.locator('.match-counter');
    this.loadingSpinner = page.locator('.spinner');
    this.cancelButton = page.locator('.page-footer .button-secondary');
    this.doneButton = page.locator('.page-footer .button-primary');
  }

  async navigate(containerName: string) {
    await this.page.goto(`/container/${encodeURIComponent(containerName)}/tag-filter`);
    await this.waitForLoaded();
  }

  async waitForLoaded(timeout = 30000) {
    await expect(this.pageTitle).toHaveText('Tag Filter', { timeout });
    await expect(this.regexInput).toBeVisible({ timeout });
  }

  async waitForTagsLoaded(timeout = 30000) {
    await expect(this.loadingSpinner).toBeHidden({ timeout });
  }

  async setPattern(pattern: string) {
    await this.regexInput.fill(pattern);
  }

  async getPattern(): Promise<string> {
    return this.regexInput.inputValue();
  }

  async clearPattern() {
    const isVisible = await this.clearInputButton.isVisible();
    if (isVisible) {
      await this.clearInputButton.click();
    } else {
      await this.regexInput.clear();
    }
  }

  async isPatternValid(): Promise<boolean> {
    const hasError = await this.errorMessage.isVisible();
    return !hasError;
  }

  async getErrorMessage(): Promise<string | null> {
    const isVisible = await this.errorMessage.isVisible();
    if (!isVisible) return null;
    const text = await this.errorMessage.textContent();
    return text?.trim() || null;
  }

  async getSuccessMessage(): Promise<string | null> {
    const isVisible = await this.successMessage.isVisible();
    if (!isVisible) return null;
    const text = await this.successMessage.textContent();
    return text?.trim() || null;
  }

  async getPresetCount(): Promise<number> {
    return this.presetButtons.count();
  }

  async clickPreset(label: string) {
    await this.page.locator(`.preset-button:has-text("${label}")`).click();
  }

  async getMatchCount(): Promise<{ matched: number; total: number }> {
    const text = await this.matchCounter.textContent();
    // Format: "X of Y tags match"
    const match = text?.match(/(\d+)\s+of\s+(\d+)/);
    if (!match) return { matched: 0, total: 0 };
    return {
      matched: parseInt(match[1], 10),
      total: parseInt(match[2], 10),
    };
  }

  async getMatchedTagsCount(): Promise<number> {
    const tags = await this.page.locator('.tag-item.matches').count();
    return tags;
  }

  async getUnmatchedTagsCount(): Promise<number> {
    const tags = await this.page.locator('.tag-item.no-match').count();
    return tags;
  }

  async hasCurrentTagBadge(): Promise<boolean> {
    const badge = this.page.locator('.current-badge');
    return badge.isVisible();
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

  async clickBack() {
    await this.backButton.click();
  }
}
