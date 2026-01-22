import { type Page, type Locator, expect } from '@playwright/test';

export class UpdateProgressPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly stageIcon: Locator;
  readonly stageMessage: Locator;
  readonly stageDescription: Locator;
  readonly progressBar: Locator;
  readonly progressPercent: Locator;
  readonly statsCards: Locator;
  readonly containerList: Locator;
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
    this.statsCards = page.locator('.progress-stats');
    this.containerList = page.locator('.container-list');
    this.activityLog = page.locator('.activity-log');
    this.doneButton = page.locator('.page-footer button');
  }

  async waitForPageLoaded(timeout = 10000) {
    // Operation progress page (unified for updates, rollback, restart)
    await expect(this.page.locator('.operation-progress-page, .progress-page')).toBeVisible({ timeout });
    await expect(this.containerList).toBeVisible({ timeout });
  }

  async waitForCompletion(timeout = 120000) {
    // Wait for the done button to be enabled (not disabled)
    await expect(this.doneButton).toBeEnabled({ timeout });
  }

  async getStatus(): Promise<'in-progress' | 'complete' | 'failed'> {
    const buttonText = await this.doneButton.textContent();
    if (buttonText?.includes('Done')) {
      // Check if there are any failed containers
      const failedCount = await this.getFailedCount();
      if (failedCount > 0) {
        return 'failed';
      }
      return 'complete';
    }
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

  async getSuccessCount(): Promise<number> {
    const card = this.statsCards.locator('.stat-card.success');
    const count = await card.count();
    if (count === 0) return 0;
    const value = await card.locator('.stat-value').textContent();
    return parseInt(value || '0', 10);
  }

  async getFailedCount(): Promise<number> {
    const card = this.statsCards.locator('.stat-card.error');
    const count = await card.count();
    if (count === 0) return 0;
    const value = await card.locator('.stat-value').textContent();
    return parseInt(value || '0', 10);
  }

  async getTotalCount(): Promise<number> {
    const card = this.statsCards.locator('.stat-card').first();
    const value = await card.locator('.stat-value').textContent();
    // Format is "completed/total"
    const match = value?.match(/\d+\/(\d+)/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async getCompletedCount(): Promise<number> {
    const card = this.statsCards.locator('.stat-card').first();
    const value = await card.locator('.stat-value').textContent();
    // Format is "completed/total"
    const match = value?.match(/(\d+)\/\d+/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async getElapsedTime(): Promise<number> {
    const card = this.statsCards.locator('.stat-card').last();
    const value = await card.locator('.stat-value').textContent();
    // Format is "Xs"
    const match = value?.match(/(\d+)s/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async getContainerStatus(name: string): Promise<string> {
    const container = this.containerList.locator(`.container-item:has-text("${name}")`);
    const classes = await container.getAttribute('class');
    if (classes?.includes('status-success')) return 'success';
    if (classes?.includes('status-failed')) return 'failed';
    if (classes?.includes('status-in_progress')) return 'in_progress';
    return 'pending';
  }

  async getContainerMessage(name: string): Promise<string | null> {
    const container = this.containerList.locator(`.container-item:has-text("${name}")`);
    const message = container.locator('.container-message');
    const count = await message.count();
    if (count === 0) return null;
    const text = await message.textContent();
    return text?.trim() || null;
  }

  async getContainerError(name: string): Promise<string | null> {
    const container = this.containerList.locator(`.container-item:has-text("${name}")`);
    const error = container.locator('.container-error');
    const count = await error.count();
    if (count === 0) return null;
    const text = await error.textContent();
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

  async hasError(): Promise<boolean> {
    const failedCount = await this.getFailedCount();
    return failedCount > 0;
  }

  async getError(): Promise<string | null> {
    if (!(await this.hasError())) return null;

    // Find the first failed container's error
    const failedContainer = this.containerList.locator('.container-item.status-failed').first();
    const count = await failedContainer.count();
    if (count === 0) return null;

    const error = failedContainer.locator('.container-error');
    const errorCount = await error.count();
    if (errorCount === 0) return null;

    const text = await error.textContent();
    return text?.trim() || null;
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
    return status === 'complete' || status === 'failed';
  }
}
