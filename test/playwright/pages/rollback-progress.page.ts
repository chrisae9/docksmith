import { type Page, type Locator, expect } from '@playwright/test';

export class RollbackProgressPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly stageIcon: Locator;
  readonly stageMessage: Locator;
  readonly stageDescription: Locator;
  readonly progressBar: Locator;
  readonly progressPercent: Locator;
  readonly infoSection: Locator;
  readonly errorBanner: Locator;
  readonly activityLog: Locator;
  readonly doneButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.stageIcon = page.locator('.stage-icon');
    this.stageMessage = page.locator('.stage-message');
    this.stageDescription = page.locator('.stage-description');
    this.progressBar = page.locator('.progress-bar-container');
    this.progressPercent = page.locator('.progress-bar-text');
    this.infoSection = page.locator('.info-section');
    this.errorBanner = page.locator('.error-banner');
    this.activityLog = page.locator('.activity-log');
    this.doneButton = page.locator('.page-footer button');
  }

  async waitForPageLoaded(timeout = 10000) {
    // Unified operation progress page
    await expect(this.page.locator('.operation-progress-page, .progress-page')).toBeVisible({ timeout });
  }

  async waitForCompletion(timeout = 120000) {
    // Wait for the done button to be enabled (not disabled)
    await expect(this.doneButton).toBeEnabled({ timeout });
  }

  async getStatus(): Promise<'in-progress' | 'success' | 'failed'> {
    const iconClasses = await this.stageIcon.getAttribute('class');
    if (iconClasses?.includes('error')) return 'failed';
    if (iconClasses?.includes('success')) return 'success';
    return 'in-progress';
  }

  async getStageMessage(): Promise<string> {
    const text = await this.stageMessage.textContent();
    return text?.trim() || '';
  }

  async getProgress(): Promise<number> {
    const count = await this.progressPercent.count();
    if (count === 0) return 0;
    const text = await this.progressPercent.textContent();
    const match = text?.match(/(\d+)%/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async getContainerName(): Promise<string | null> {
    const row = this.infoSection.locator('.info-row:has(.info-label:has-text("Container"))');
    const count = await row.count();
    if (count === 0) return null;
    const value = await row.locator('.info-value').textContent();
    return value?.trim() || null;
  }

  async getCurrentVersion(): Promise<string | null> {
    const row = this.infoSection.locator('.info-row:has(.info-label:has-text("Current Version"))');
    const count = await row.count();
    if (count === 0) return null;
    const value = await row.locator('.info-value').textContent();
    return value?.trim() || null;
  }

  async getRollingBackTo(): Promise<string | null> {
    const row = this.infoSection.locator('.info-row:has(.info-label:has-text("Rolling Back To"))');
    const count = await row.count();
    if (count === 0) return null;
    const value = await row.locator('.info-value').textContent();
    return value?.trim() || null;
  }

  async getInfoStatus(): Promise<string | null> {
    const row = this.infoSection.locator('.info-row:has(.info-label:has-text("Status"))');
    const count = await row.count();
    if (count === 0) return null;
    const badge = row.locator('.status-badge');
    const text = await badge.textContent();
    return text?.trim() || null;
  }

  async getElapsedSeconds(): Promise<number> {
    const row = this.infoSection.locator('.info-row:has(.info-label:has-text("Elapsed"))');
    const count = await row.count();
    if (count === 0) return 0;
    const text = await row.locator('.info-value').textContent();
    const match = text?.match(/(\d+)s/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async hasError(): Promise<boolean> {
    return this.errorBanner.isVisible();
  }

  async getError(): Promise<string | null> {
    if (!(await this.hasError())) return null;
    const text = await this.errorBanner.locator('span').textContent();
    return text?.trim() || null;
  }

  async getLogEntries(): Promise<Array<{ type: string; message: string }>> {
    const entries = this.activityLog.locator('.log-entry');
    const count = await entries.count();
    const logs: Array<{ type: string; message: string }> = [];

    for (let i = 0; i < count; i++) {
      const entry = entries.nth(i);
      const classes = await entry.getAttribute('class');
      const message = await entry.locator('.log-message').textContent();

      let type = 'info';
      if (classes?.includes('log-success')) type = 'success';
      if (classes?.includes('log-error')) type = 'error';
      if (classes?.includes('log-stage')) type = 'stage';

      logs.push({
        type,
        message: message?.trim() || '',
      });
    }
    return logs;
  }

  async clickDone() {
    await expect(this.doneButton).toBeEnabled();
    await this.doneButton.click();
  }

  async clickBack() {
    await expect(this.backButton).toBeEnabled();
    await this.backButton.click();
  }

  async isComplete(): Promise<boolean> {
    const status = await this.getStatus();
    return status === 'success' || status === 'failed';
  }
}
