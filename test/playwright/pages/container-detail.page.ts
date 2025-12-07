import { type Page, type Locator, expect } from '@playwright/test';

export class ContainerDetailPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly containerName: Locator;
  readonly statusBadge: Locator;
  readonly changeTypeBadge: Locator;
  readonly syncWarningBanner: Locator;
  readonly syncButton: Locator;
  readonly settingsSection: Locator;
  readonly ignoreCheckbox: Locator;
  readonly allowLatestCheckbox: Locator;
  readonly versionPinControl: Locator;
  readonly tagFilterRow: Locator;
  readonly preUpdateScriptRow: Locator;
  readonly restartDependenciesRow: Locator;
  readonly labelsSection: Locator;
  readonly saveRestartButton: Locator;
  readonly restartButton: Locator;
  readonly updateButton: Locator;
  readonly cancelButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.containerName = page.locator('.page-header h1');
    this.statusBadge = page.locator('.status-badge');
    this.changeTypeBadge = page.locator('.change-badge');
    this.syncWarningBanner = page.locator('.labels-sync-warning');
    this.syncButton = page.locator('.labels-sync-warning button');
    this.settingsSection = page.locator('.settings-section');
    // Use accessibility-based selectors for checkboxes
    this.ignoreCheckbox = page.getByRole('checkbox', { name: 'Ignore Container' });
    this.allowLatestCheckbox = page.getByRole('checkbox', { name: 'Allow :latest Tag' });
    this.versionPinControl = page.locator('.segmented-control');
    this.tagFilterRow = page.locator('.nav-row:has-text("Tag Filter")');
    this.preUpdateScriptRow = page.locator('.nav-row:has-text("Pre-Update Script")');
    this.restartDependenciesRow = page.locator('.nav-row:has-text("Restart Dependencies")');
    this.labelsSection = page.locator('.labels-section');
    // Use CSS class selectors for footer buttons to avoid conflicts with segmented controls
    this.saveRestartButton = page.locator('.page-footer .button-primary:has-text("Save & Restart")');
    this.restartButton = page.locator('.page-footer .button-secondary:has-text("Restart")');
    // Update button has specific color classes based on change type
    this.updateButton = page.locator('.page-footer .button-patch, .page-footer .button-minor, .page-footer .button-major, .page-footer .button-accent:has-text("Update")');
    this.cancelButton = page.locator('.page-footer .button-secondary:has-text("Cancel")');
  }

  async navigate(containerName: string) {
    await this.page.goto(`/container/${encodeURIComponent(containerName)}`);
    await this.waitForLoaded();
  }

  async waitForLoaded(timeout = 30000) {
    // Wait for page header to show container name
    await expect(this.containerName).toBeVisible({ timeout });
    // Wait for settings to load
    await expect(this.settingsSection).toBeVisible({ timeout });
    // Make sure loading is done
    await expect(this.page.locator('.loading-inline')).toBeHidden({ timeout });
  }

  async getContainerName(): Promise<string> {
    const text = await this.containerName.textContent();
    return text?.trim() || '';
  }

  async getStatus(): Promise<string> {
    const text = await this.statusBadge.textContent();
    return text?.trim() || '';
  }

  async getChangeType(): Promise<string | null> {
    const count = await this.changeTypeBadge.count();
    if (count === 0) return null;
    const text = await this.changeTypeBadge.textContent();
    return text?.trim() || null;
  }

  async isOutOfSyncBannerVisible(): Promise<boolean> {
    return this.syncWarningBanner.isVisible();
  }

  async clickSyncButton() {
    await this.syncButton.click();
  }

  async toggleIgnore() {
    await this.ignoreCheckbox.click();
  }

  async isIgnoreChecked(): Promise<boolean> {
    return this.ignoreCheckbox.isChecked();
  }

  async toggleAllowLatest() {
    await this.allowLatestCheckbox.click();
  }

  async isAllowLatestChecked(): Promise<boolean> {
    return this.allowLatestCheckbox.isChecked();
  }

  async setVersionPin(pin: 'none' | 'patch' | 'minor' | 'major') {
    const capitalizedPin = pin.charAt(0).toUpperCase() + pin.slice(1);
    await this.versionPinControl.locator(`button:has-text("${capitalizedPin}")`).click();
  }

  async getVersionPin(): Promise<string> {
    const activeSegment = this.versionPinControl.locator('.segment.active');
    const text = await activeSegment.textContent();
    return text?.trim().toLowerCase() || 'none';
  }

  async clickTagFilter() {
    await this.tagFilterRow.click();
  }

  async getTagFilterValue(): Promise<string> {
    const value = this.tagFilterRow.locator('.nav-value');
    const text = await value.textContent();
    // Remove the arrow "›" and trim
    return text?.replace('›', '').trim() || 'None';
  }

  async clickPreUpdateScript() {
    await this.preUpdateScriptRow.click();
  }

  async getPreUpdateScriptValue(): Promise<string> {
    const value = this.preUpdateScriptRow.locator('.nav-value');
    const text = await value.textContent();
    return text?.replace('›', '').trim() || 'None';
  }

  async clickRestartDependencies() {
    await this.restartDependenciesRow.click();
  }

  async getRestartDependenciesValue(): Promise<string> {
    const value = this.restartDependenciesRow.locator('.nav-value');
    const text = await value.textContent();
    return text?.replace('›', '').trim() || 'None';
  }

  async getLabels(): Promise<Record<string, string>> {
    const labels: Record<string, string> = {};
    const items = this.labelsSection.locator('.label-item');
    const count = await items.count();

    for (let i = 0; i < count; i++) {
      const item = items.nth(i);
      const key = await item.locator('.label-key').textContent();
      const value = await item.locator('.label-value').textContent();
      if (key) {
        labels[key.trim()] = value?.trim() || '';
      }
    }
    return labels;
  }

  async getLabelValue(labelKey: string): Promise<string | null> {
    const item = this.labelsSection.locator(`.label-item:has(.label-key:has-text("${labelKey}"))`);
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.label-value').textContent();
    return value?.trim() || null;
  }

  async hasUnsavedChanges(): Promise<boolean> {
    // Check if the changes warning banner is visible (more reliable than button check)
    const warningBanner = this.page.locator('.changes-warning-banner');
    const bannerVisible = await warningBanner.isVisible().catch(() => false);
    if (bannerVisible) return true;

    // Fallback: check for Save & Restart button using role selector
    const saveButton = this.page.getByRole('button', { name: /Save & Restart/i });
    return saveButton.isVisible().catch(() => false);
  }

  async clickRestart() {
    await this.restartButton.click();
  }

  async clickSaveRestart() {
    await this.saveRestartButton.click();
  }

  async clickUpdate() {
    await this.updateButton.click();
  }

  async clickCancel() {
    await this.cancelButton.click();
  }

  async clickBack() {
    await this.backButton.click();
  }

  // Version information
  async getCurrentVersion(): Promise<string | null> {
    const item = this.page.locator('.detail-item:has(.detail-label:has-text("Current Version"))');
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.detail-value').textContent();
    return value?.trim() || null;
  }

  async getLatestVersion(): Promise<string | null> {
    const item = this.page.locator('.detail-item:has(.detail-label:has-text("Latest Version"))');
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.detail-value').textContent();
    return value?.trim() || null;
  }

  async getImage(): Promise<string | null> {
    const item = this.page.locator('.detail-item:has(.detail-label:has-text("Repository"))');
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.detail-value').textContent();
    return value?.trim() || null;
  }
}
