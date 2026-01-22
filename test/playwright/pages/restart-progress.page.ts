import { type Page, type Locator, expect } from '@playwright/test';

export class RestartProgressPage {
  readonly page: Page;
  readonly backButton: Locator;
  readonly stageIcon: Locator;
  readonly stageMessage: Locator;
  readonly stageDescription: Locator;
  readonly containerNameDisplay: Locator;
  readonly forceBadge: Locator;
  readonly elapsedTime: Locator;
  readonly resultsSection: Locator;
  readonly activityLog: Locator;
  readonly doneButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.backButton = page.locator('.back-button');
    this.stageIcon = page.locator('.stage-icon');
    this.stageMessage = page.locator('.stage-message');
    this.stageDescription = page.locator('.stage-description');
    this.containerNameDisplay = page.locator('.restart-container-name');
    this.forceBadge = page.locator('.force-badge');
    this.elapsedTime = page.locator('.restart-elapsed');
    this.resultsSection = page.locator('.restart-results');
    this.activityLog = page.locator('.activity-log');
    // Use specific selector within operation progress page
    this.doneButton = page.locator('.operation-progress-page footer button, .progress-page footer button, .page-footer button');
  }

  async waitForPageLoaded(timeout = 10000) {
    // Wait for URL to change to /operation (unified progress page)
    await this.page.waitForURL('**/operation', { timeout });
    // Unified operation progress page
    await expect(this.page.locator('.operation-progress-page, .progress-page')).toBeVisible({ timeout });
  }

  async waitForCompletion(timeout = 60000) {
    // Wait for the done button to be enabled and show "Done" (not "Restarting...")
    await expect(this.doneButton).toHaveText('Done', { timeout });
  }

  async getStatus(): Promise<'in-progress' | 'complete' | 'failed'> {
    const stageClasses = await this.stageIcon.getAttribute('class');
    if (stageClasses?.includes('failed')) return 'failed';
    if (stageClasses?.includes('success') || stageClasses?.includes('warning')) {
      return 'complete';
    }
    return 'in-progress';
  }

  async getStageMessage(): Promise<string> {
    const text = await this.stageMessage.textContent();
    return text?.trim() || '';
  }

  async getContainerName(): Promise<string> {
    const text = await this.containerNameDisplay.textContent();
    return text?.trim() || '';
  }

  async isForceRestart(): Promise<boolean> {
    return this.forceBadge.isVisible();
  }

  async getElapsedSeconds(): Promise<number> {
    const text = await this.elapsedTime.textContent();
    const match = text?.match(/(\d+)s/);
    return match ? parseInt(match[1], 10) : 0;
  }

  async isSuccess(): Promise<boolean> {
    const count = await this.resultsSection.locator('.result-success').count();
    return count > 0;
  }

  async hasWarnings(): Promise<boolean> {
    const count = await this.resultsSection.locator('.result-warning').count();
    return count > 0;
  }

  async isFailed(): Promise<boolean> {
    const count = await this.resultsSection.locator('.result-error').count();
    return count > 0;
  }

  async getError(): Promise<string | null> {
    const errorElement = this.resultsSection.locator('.result-error');
    const count = await errorElement.count();
    if (count === 0) return null;
    const text = await errorElement.textContent();
    return text?.trim() || null;
  }

  async getDependentsRestarted(): Promise<string[]> {
    const info = this.resultsSection.locator('.dependents-info.success');
    const count = await info.count();
    if (count === 0) return [];

    const text = await info.textContent();
    // Extract container names from "N dependent(s) restarted: name1, name2"
    const match = text?.match(/restarted:\s*(.+)/);
    if (!match) return [];
    return match[1].split(',').map(s => s.trim());
  }

  async getDependentsBlocked(): Promise<string[]> {
    const info = this.resultsSection.locator('.dependents-info.warning');
    const count = await info.count();
    if (count === 0) return [];

    const text = await info.textContent();
    // Extract container names from "N dependent(s) blocked: name1, name2"
    const match = text?.match(/blocked:\s*(.+)/);
    if (!match) return [];
    return match[1].split(',').map(s => s.trim());
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
    await expect(this.doneButton).toHaveText('Done');
    await this.doneButton.click();
  }

  async clickBack() {
    await this.backButton.click();
  }

  async isComplete(): Promise<boolean> {
    const status = await this.getStatus();
    return status === 'complete' || status === 'failed';
  }
}
