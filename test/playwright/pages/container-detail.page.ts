import { type Page, type Locator, expect } from '@playwright/test';

export class ContainerDetailPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly containerName: Locator;
  readonly statusBadge: Locator;
  readonly changeTypeBadge: Locator;
  readonly syncWarningBanner: Locator;
  readonly syncButton: Locator;
  readonly tabNav: Locator;
  readonly overviewTab: Locator;
  readonly configTab: Locator;
  readonly logsTab: Locator;
  readonly inspectTab: Locator;
  readonly ignoreCheckbox: Locator;
  readonly allowLatestCheckbox: Locator;
  readonly versionPinControl: Locator;
  readonly tagFilterRow: Locator;
  readonly preUpdateScriptRow: Locator;
  readonly restartDependenciesRow: Locator;
  readonly configFooter: Locator;
  readonly saveRestartButton: Locator;
  readonly cancelButton: Locator;
  readonly updateButton: Locator;
  readonly labelsSection: Locator;

  constructor(page: Page) {
    this.page = page;
    // Header elements - using the actual container-page structure
    this.backButton = page.locator('.container-page .back-btn');
    this.containerName = page.locator('.container-page .header-info h1');
    this.statusBadge = page.locator('.container-page .status-badge');
    this.changeTypeBadge = page.locator('.container-page .change-badge');
    this.syncWarningBanner = page.locator('.container-page .sync-warning');
    this.syncButton = page.locator('.container-page .sync-warning .sync-btn');

    // Tab navigation
    this.tabNav = page.locator('.container-page .tab-nav');
    this.overviewTab = this.tabNav.getByRole('button', { name: /Overview/i });
    this.configTab = this.tabNav.getByRole('button', { name: /Config/i });
    this.logsTab = this.tabNav.getByRole('button', { name: /Logs/i });
    this.inspectTab = this.tabNav.getByRole('button', { name: /Inspect/i });

    // Config tab elements - use label text matching
    this.ignoreCheckbox = page.locator('.container-page .checkbox-row').filter({ hasText: 'Ignore Container' }).locator('input[type="checkbox"]');
    this.allowLatestCheckbox = page.locator('.container-page .checkbox-row').filter({ hasText: 'Allow :latest' }).locator('input[type="checkbox"]');
    this.versionPinControl = page.locator('.container-page .segmented-row .segmented-control');
    this.tagFilterRow = page.locator('.container-page .nav-row-with-help').filter({ hasText: 'Tag Filter' });
    this.preUpdateScriptRow = page.locator('.container-page .precheck-row-with-help');
    this.restartDependenciesRow = page.locator('.container-page .nav-row-with-help').filter({ hasText: 'Restart Dependencies' });

    // Config footer buttons (only visible when changes pending)
    this.configFooter = page.locator('.container-page .config-footer');
    this.saveRestartButton = this.configFooter.locator('.button-primary');
    this.cancelButton = this.configFooter.locator('.button-secondary');

    // Update button in version card (overview tab)
    this.updateButton = page.locator('.container-page .version-card .update-btn');

    // Labels section in overview tab
    this.labelsSection = page.locator('.container-page .info-section').filter({ has: page.locator('h3:has-text("Labels")') });
  }

  async navigate(containerName: string) {
    await this.page.goto(`/container/${encodeURIComponent(containerName)}`);
    await this.waitForLoaded();
  }

  async waitForLoaded(timeout = 30000) {
    // Wait for page header to show container name
    await expect(this.containerName).toBeVisible({ timeout });
    // Wait for tab navigation to appear
    await expect(this.tabNav).toBeVisible({ timeout });
    // Make sure loading spinner is gone
    await expect(this.page.locator('.container-page .main-loading')).toBeHidden({ timeout });
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

  // Tab navigation
  async clickOverviewTab() {
    await this.overviewTab.click();
  }

  async clickConfigTab() {
    await this.configTab.click();
  }

  async clickLogsTab() {
    await this.logsTab.click();
  }

  async clickInspectTab() {
    await this.inspectTab.click();
  }

  // Config tab methods
  async toggleIgnore() {
    // Click on the row's label area to toggle, not the checkbox directly
    const row = this.page.locator('.container-page .checkbox-row').filter({ hasText: 'Ignore Container' });
    await row.locator('.row-label-area').click();
  }

  async isIgnoreChecked(): Promise<boolean> {
    return this.ignoreCheckbox.isChecked();
  }

  async toggleAllowLatest() {
    const row = this.page.locator('.container-page .checkbox-row').filter({ hasText: 'Allow :latest' });
    await row.locator('.row-label-area').click();
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
    await this.tagFilterRow.locator('.nav-row-content').click();
  }

  async getTagFilterValue(): Promise<string> {
    const value = this.tagFilterRow.locator('.nav-value');
    const text = await value.textContent();
    // Remove the arrow "›" and trim
    return text?.replace('›', '').trim() || 'None';
  }

  async clickPreUpdateScript() {
    await this.preUpdateScriptRow.locator('.precheck-nav-area').click();
  }

  async getPreUpdateScriptValue(): Promise<string> {
    const value = this.preUpdateScriptRow.locator('.nav-value');
    const text = await value.textContent();
    return text?.replace('›', '').trim() || 'None';
  }

  async clickRestartDependencies() {
    await this.restartDependenciesRow.locator('.nav-row-content').click();
  }

  async getRestartDependenciesValue(): Promise<string> {
    const value = this.restartDependenciesRow.locator('.nav-value');
    const text = await value.textContent();
    return text?.replace('›', '').trim() || 'None';
  }

  // Labels (in overview tab)
  async getLabels(): Promise<Record<string, string>> {
    const labels: Record<string, string> = {};
    const items = this.page.locator('.container-page .label-list li');
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
    const item = this.page.locator(`.container-page .label-list li:has(.label-key:has-text("${labelKey}"))`);
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.label-value').textContent();
    return value?.trim() || null;
  }

  async hasUnsavedChanges(): Promise<boolean> {
    // Check if the config footer is visible (only shows when changes pending)
    return this.configFooter.isVisible().catch(() => false);
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

  // Version information (in overview tab, from version-card or info-grid)
  async getCurrentVersion(): Promise<string | null> {
    // Check version card first
    const versionCard = this.page.locator('.container-page .version-card .version-current');
    if (await versionCard.count() > 0) {
      const text = await versionCard.textContent();
      return text?.trim() || null;
    }
    return null;
  }

  async getLatestVersion(): Promise<string | null> {
    // Check version card first
    const versionCard = this.page.locator('.container-page .version-card .version-latest');
    if (await versionCard.count() > 0) {
      const text = await versionCard.textContent();
      return text?.trim() || null;
    }
    return null;
  }

  async getImage(): Promise<string | null> {
    // Image is shown in the Container info section
    const item = this.page.locator('.container-page .info-item').filter({ hasText: 'Image' });
    const count = await item.count();
    if (count === 0) return null;
    const value = await item.locator('.info-value').textContent();
    return value?.trim() || null;
  }

  // Header action buttons
  async clickStartButton() {
    await this.page.locator('.container-page .action-btn[title="Start"]').click();
  }

  async clickStopButton() {
    await this.page.locator('.container-page .action-btn[title="Stop"]').click();
  }

  async clickRestartButton() {
    await this.page.locator('.container-page .action-btn[title="Restart"]').click();
  }

  async clickRemoveButton() {
    await this.page.locator('.container-page .action-btn[title="Remove"]').click();
  }

  async confirmRemove() {
    await this.page.locator('.container-page .confirm-remove .action-btn[title="Confirm remove"]').click();
  }

  async cancelRemove() {
    await this.page.locator('.container-page .confirm-remove .action-btn[title="Cancel"]').click();
  }
}
