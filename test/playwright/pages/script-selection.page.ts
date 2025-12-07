import { type Page, type Locator, expect } from '@playwright/test';

export class ScriptSelectionPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly pageTitle: Locator;
  readonly containerConfigCard: Locator;
  readonly selectedScriptDisplay: Locator;
  readonly clearSelectionButton: Locator;
  readonly searchInput: Locator;
  readonly clearSearchButton: Locator;
  readonly scriptItems: Locator;
  readonly noneOption: Locator;
  readonly loadingSpinner: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly infoBox: Locator;
  readonly cancelButton: Locator;
  readonly doneButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.pageTitle = page.locator('.page-header h1');
    this.containerConfigCard = page.locator('.container-config-card');
    this.selectedScriptDisplay = page.locator('.selected-script-display');
    this.clearSelectionButton = page.locator('.selection-summary .clear-button');
    this.searchInput = page.locator('.search-input');
    this.clearSearchButton = page.locator('.clear-search');
    this.scriptItems = page.locator('.script-item');
    this.noneOption = page.locator('.script-item.none-option');
    this.loadingSpinner = page.locator('.spinner');
    this.errorState = page.locator('.error-state');
    this.emptyState = page.locator('.empty-state');
    this.infoBox = page.locator('.info-box');
    this.cancelButton = page.locator('.page-footer .button-secondary');
    this.doneButton = page.locator('.page-footer .button-primary');
  }

  async navigate(containerName: string) {
    await this.page.goto(`/container/${encodeURIComponent(containerName)}/script-selection`);
    await this.waitForLoaded();
  }

  async waitForLoaded(timeout = 30000) {
    await expect(this.pageTitle).toHaveText('Select Script', { timeout });
    await expect(this.loadingSpinner).toBeHidden({ timeout });
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

  async getScriptCount(): Promise<number> {
    // Exclude the none-option from count
    return this.page.locator('.script-item:not(.none-option)').count();
  }

  async selectNone() {
    await this.noneOption.click();
  }

  async selectScript(scriptName: string) {
    await this.page.locator(`.script-item:has(.script-name:has-text("${scriptName}"))`).click();
  }

  async selectScriptByPath(path: string) {
    await this.page.locator(`.script-item:has(.script-path:has-text("${path}"))`).click();
  }

  async isScriptSelected(scriptName: string): Promise<boolean> {
    const item = this.page.locator(`.script-item:has(.script-name:has-text("${scriptName}"))`);
    const classes = await item.getAttribute('class');
    return classes?.includes('selected') || false;
  }

  async isNoneSelected(): Promise<boolean> {
    const classes = await this.noneOption.getAttribute('class');
    return classes?.includes('selected') || false;
  }

  async getSelectedScript(): Promise<string | null> {
    const isVisible = await this.selectedScriptDisplay.isVisible();
    if (!isVisible) return null;
    const text = await this.selectedScriptDisplay.locator('.script-name-display').textContent();
    return text?.trim() || null;
  }

  async clearSelection() {
    const isVisible = await this.clearSelectionButton.isVisible();
    if (isVisible) {
      await this.clearSelectionButton.click();
    } else {
      await this.selectNone();
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

  async hasNoScripts(): Promise<boolean> {
    return this.infoBox.isVisible();
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
