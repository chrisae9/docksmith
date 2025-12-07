import { type Page, type Locator, expect } from '@playwright/test';

export class SettingsPage {
  readonly page: Page;
  readonly pageTitle: Locator;
  readonly errorBanner: Locator;
  readonly lastBackgroundCheck: Locator;
  readonly lastCacheUpdate: Locator;
  readonly backgroundRefreshBtn: Locator;
  readonly cacheRefreshBtn: Locator;
  readonly totalChecked: Locator;
  readonly updatesFound: Locator;
  readonly upToDate: Locator;
  readonly localImages: Locator;
  readonly failed: Locator;
  readonly ignored: Locator;
  readonly checkInterval: Locator;
  readonly cacheTTL: Locator;
  readonly registriesList: Locator;

  constructor(page: Page) {
    this.page = page;
    this.pageTitle = page.locator('.settings-page h1');
    this.errorBanner = page.locator('.error-banner');
    this.lastBackgroundCheck = page.locator('.setting-row:has(.setting-label:has-text("Last Background Check")) .setting-value');
    this.lastCacheUpdate = page.locator('.setting-row:has(.setting-label:has-text("Last Cache Update")) .setting-value');
    this.backgroundRefreshBtn = page.locator('.settings-btn:has(.btn-title:has-text("Background Refresh"))');
    this.cacheRefreshBtn = page.locator('.settings-btn.cache-btn, .settings-btn:has(.btn-title:has-text("Cache Refresh"))');
    // Stats grid
    this.totalChecked = page.locator('.stat-item:has(.stat-label:has-text("Total Checked")) .stat-value');
    this.updatesFound = page.locator('.stat-item:has(.stat-label:has-text("Updates Found")) .stat-value');
    this.upToDate = page.locator('.stat-item:has(.stat-label:has-text("Up to Date")) .stat-value');
    this.localImages = page.locator('.stat-item:has(.stat-label:has-text("Local Images")) .stat-value');
    this.failed = page.locator('.stat-item:has(.stat-label:has-text("Failed")) .stat-value');
    this.ignored = page.locator('.stat-item:has(.stat-label:has-text("Ignored")) .stat-value');
    // Environment variables
    this.checkInterval = page.locator('.setting-row:has(.setting-label:has-text("CHECK_INTERVAL")) .setting-value');
    this.cacheTTL = page.locator('.setting-row:has(.setting-label:has-text("CACHE_TTL")) .setting-value');
    // Registries
    this.registriesList = page.locator('.settings-section:has(.section-title:has-text("Authenticated Registries")) .setting-row');
  }

  async waitForLoaded(timeout = 30000) {
    await expect(this.pageTitle).toHaveText('Settings', { timeout });
    // Wait for stats to load (at least Total Checked)
    await expect(this.totalChecked).toBeVisible({ timeout });
  }

  async hasError(): Promise<boolean> {
    return this.errorBanner.isVisible();
  }

  async getError(): Promise<string | null> {
    if (!(await this.hasError())) return null;
    const text = await this.errorBanner.textContent();
    return text?.trim() || null;
  }

  async getLastBackgroundCheck(): Promise<string> {
    const text = await this.lastBackgroundCheck.textContent();
    return text?.trim() || '';
  }

  async getLastCacheUpdate(): Promise<string> {
    const text = await this.lastCacheUpdate.textContent();
    return text?.trim() || '';
  }

  async clickBackgroundRefresh() {
    await this.backgroundRefreshBtn.click();
  }

  async clickCacheRefresh() {
    await this.cacheRefreshBtn.click();
  }

  async isBackgroundRefreshDisabled(): Promise<boolean> {
    return this.backgroundRefreshBtn.isDisabled();
  }

  async isCacheRefreshDisabled(): Promise<boolean> {
    return this.cacheRefreshBtn.isDisabled();
  }

  // Stats getters
  async getTotalChecked(): Promise<number> {
    const text = await this.totalChecked.textContent();
    return parseInt(text || '0', 10);
  }

  async getUpdatesFound(): Promise<number> {
    const text = await this.updatesFound.textContent();
    return parseInt(text || '0', 10);
  }

  async getUpToDate(): Promise<number> {
    const text = await this.upToDate.textContent();
    return parseInt(text || '0', 10);
  }

  async getLocalImages(): Promise<number> {
    const text = await this.localImages.textContent();
    return parseInt(text || '0', 10);
  }

  async getFailed(): Promise<number> {
    const text = await this.failed.textContent();
    return parseInt(text || '0', 10);
  }

  async getIgnored(): Promise<number> {
    const text = await this.ignored.textContent();
    return parseInt(text || '0', 10);
  }

  // Environment variables
  async getCheckInterval(): Promise<string> {
    const text = await this.checkInterval.textContent();
    return text?.trim() || '';
  }

  async getCacheTTL(): Promise<string> {
    const text = await this.cacheTTL.textContent();
    return text?.trim() || '';
  }

  // Registries
  async getRegistryCount(): Promise<number> {
    return this.registriesList.count();
  }

  async getRegistries(): Promise<string[]> {
    const count = await this.registriesList.count();
    const registries: string[] = [];
    for (let i = 0; i < count; i++) {
      const row = this.registriesList.nth(i);
      const label = await row.locator('.setting-label').textContent();
      if (label && !label.includes('No authenticated')) {
        registries.push(label.trim());
      }
    }
    return registries;
  }
}
