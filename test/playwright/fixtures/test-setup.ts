/**
 * Docksmith Playwright Test Setup
 *
 * This module provides a robust API helper for testing Docksmith's backend.
 *
 * ## Features
 *
 * ### Rate Limit Handling
 * - Automatic retry on HTTP 429 or "Rate limit" text responses
 * - Honors `Retry-After` header when present
 * - Exponential backoff with jitter (0.5x-1.5x) to prevent thundering herd
 * - Maximum delay cap (15s) to stay within test timeouts
 *
 * ### Network Error Resilience
 * - Retries on transient errors (ECONNRESET, ETIMEDOUT, ECONNREFUSED, etc.)
 * - Retries on server errors (5xx)
 * - Standalone sleep function avoids "Target closed" errors
 *
 * ### Response Handling
 * - Checks HTTP status before parsing
 * - Handles empty bodies (204 No Content)
 * - Content-type aware JSON parsing
 * - Detailed error messages with status codes and URLs
 *
 * ### Polling Utilities
 * - `waitForStatus` - Wait for container to reach specific status
 * - `waitForStatusNot` - Wait for status to change from a value
 * - `waitForVersion` - Wait for container version
 * - `waitForOperation` - Wait for operation to complete
 * - `waitForCondition` - Generic polling with custom condition
 *
 * ## Usage
 *
 * ```typescript
 * import { test, expect, TEST_CONTAINERS } from '../fixtures/test-setup';
 *
 * test('example', async ({ api }) => {
 *   // All API methods automatically handle rate limits and retries
 *   const status = await api.status();
 *   expect(status.success).toBe(true);
 *
 *   // Smart polling for container state
 *   await api.waitForStatus('my-container', 'UP_TO_DATE');
 * });
 * ```
 *
 * ## Environment Variables
 *
 * - `DOCKSMITH_URL` - API base URL (default: http://localhost:8080)
 * - `TEST_ENV=ci` - Use CI test containers from test/integration/environments/
 * - `TEST_CONTAINER_*` - Override individual test container names
 */

import { test as base, expect, type Page, type APIRequestContext, type APIResponse } from '@playwright/test';

// API base URL (same as what we're testing against)
export const API_BASE = process.env.DOCKSMITH_URL || 'http://localhost:8080';

// Test container names - configurable via environment for CI
// Local dev: defaults to real containers visible to production Docksmith
// CI: set TEST_ENV=ci to use test containers from test/integration/environments/
const isCI = process.env.TEST_ENV === 'ci' || process.env.CI === 'true';

export const TEST_CONTAINERS = isCI ? {
  // CI mode: use containers from test/integration/environments/
  NGINX_BASIC: 'test-nginx-basic',
  REDIS_BASIC: 'test-redis-basic',
  POSTGRES_BASIC: 'test-postgres-basic',
  LABELS_IGNORED: 'test-labels-ignored',
  LABELS_LATEST: 'test-labels-latest',
  LABELS_PRE_PASS: 'test-labels-pre-pass',
  LABELS_PRE_FAIL: 'test-labels-pre-fail',
  LABELS_RESTART_DEPS: 'test-labels-restart-deps',
  LABELS_DEPENDENT_1: 'test-labels-dependent-1',
  LABELS_DEPENDENT_2: 'test-labels-dependent-2',
  LABELS_NGINX: 'test-labels-nginx',
  LABELS_ALPINE: 'test-labels-alpine',
  LABELS_POSTGRES: 'test-labels-postgres',
  LABELS_REDIS: 'test-labels-redis',
  LABELS_NODE: 'test-labels-node',
  COMPOSE_MISMATCH: 'test-nginx-mismatch',
} : {
  // Local dev mode: use real containers visible to Docksmith
  // These should be containers that exist and are safe for testing
  NGINX_BASIC: process.env.TEST_CONTAINER_NGINX || 'ntfy',
  REDIS_BASIC: process.env.TEST_CONTAINER_REDIS || 'bazarr',
  POSTGRES_BASIC: process.env.TEST_CONTAINER_POSTGRES || 'calibre',
  LABELS_IGNORED: process.env.TEST_CONTAINER_IGNORED || 'factorio',
  LABELS_LATEST: process.env.TEST_CONTAINER_LATEST || 'mosquitto',
  LABELS_PRE_PASS: process.env.TEST_CONTAINER_PRE_PASS || 'plex',
  LABELS_PRE_FAIL: process.env.TEST_CONTAINER_PRE_FAIL || 'prowlarr',
  LABELS_RESTART_DEPS: process.env.TEST_CONTAINER_RESTART_DEPS || 'gluetun',
  LABELS_DEPENDENT_1: process.env.TEST_CONTAINER_DEP1 || 'sonarr',
  LABELS_DEPENDENT_2: process.env.TEST_CONTAINER_DEP2 || 'torrent',
  LABELS_NGINX: process.env.TEST_CONTAINER_NGINX2 || 'plex',
  LABELS_ALPINE: process.env.TEST_CONTAINER_ALPINE || 'tautulli',
  LABELS_POSTGRES: process.env.TEST_CONTAINER_POSTGRES2 || 'calibre',
  LABELS_REDIS: process.env.TEST_CONTAINER_REDIS2 || 'kavita',
  LABELS_NODE: process.env.TEST_CONTAINER_NODE || 'whoami',
  // Compose mismatch test container - uses test-nginx-mismatch from test/integration/environments/compose-mismatch
  COMPOSE_MISMATCH: process.env.TEST_CONTAINER_MISMATCH || 'test-nginx-mismatch',
};

// Extend Playwright test with custom fixtures
export interface TestFixtures {
  api: APIHelper;
}

// Custom test that includes our API helper
export const test = base.extend<TestFixtures>({
  api: async ({ page }, use) => {
    const api = new APIHelper(page);
    await use(api);
  },
});

export { expect };

/**
 * Standalone sleep function that doesn't depend on page lifecycle.
 * This avoids "Target closed" errors when using page.waitForTimeout during retries.
 */
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Add jitter to a delay to prevent thundering herd effect in parallel tests.
 * Returns a value between 0.5x and 1.5x of the base delay.
 */
function addJitter(baseDelayMs: number): number {
  const jitterFactor = 0.5 + Math.random(); // 0.5 to 1.5
  return Math.round(baseDelayMs * jitterFactor);
}

/**
 * Check if an error is a transient network error that should be retried.
 */
function isTransientError(error: unknown): boolean {
  if (error instanceof Error) {
    const message = error.message.toLowerCase();
    return (
      message.includes('econnreset') ||
      message.includes('etimedout') ||
      message.includes('econnrefused') ||
      message.includes('socket hang up') ||
      message.includes('network') ||
      message.includes('timeout')
    );
  }
  return false;
}

/**
 * API Helper for Docksmith backend calls
 */
export class APIHelper {
  private page: Page;
  private baseUrl: string;

  constructor(page: Page) {
    this.page = page;
    this.baseUrl = API_BASE;
  }

  /**
   * Get the API request context
   */
  private get request(): APIRequestContext {
    return this.page.request;
  }

  /**
   * Robust API request with rate limit handling, network error retries, and proper response parsing.
   *
   * Features:
   * - Retries on rate limit (429) with exponential backoff + jitter
   * - Honors Retry-After header when present (capped at maxDelayMs)
   * - Retries on transient network errors (ECONNRESET, ETIMEDOUT, etc.)
   * - Checks HTTP status before parsing
   * - Handles empty bodies gracefully
   * - Uses standalone sleep to avoid page lifecycle issues
   * - Caps delays to ensure retries complete within test timeouts
   */
  private async parseWithRateLimitRetry<T>(
    makeRequest: () => Promise<APIResponse>,
    maxRetries = 3,
    initialDelayMs = 3000,
    maxDelayMs = 15000
  ): Promise<T> {
    let lastError: Error | null = null;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      let response: APIResponse;
      let text: string;

      // Wrap request in try/catch to handle network errors
      try {
        response = await makeRequest();
        text = await response.text();
      } catch (error) {
        // Handle transient network errors with retry
        if (isTransientError(error) && attempt < maxRetries) {
          const baseDelay = initialDelayMs * Math.pow(2, attempt);
          const delay = Math.min(addJitter(baseDelay), maxDelayMs);
          console.log(`Network error (${error instanceof Error ? error.message : 'unknown'}), waiting ${delay}ms before retry ${attempt + 1}/${maxRetries}`);
          await sleep(delay);
          lastError = error instanceof Error ? error : new Error(String(error));
          continue;
        }
        // Non-transient error or max retries exceeded
        throw error;
      }

      const status = response.status();

      // Check if rate limited (429 or text starting with "Rate limit")
      if (status === 429 || text.startsWith('Rate limit')) {
        if (attempt < maxRetries) {
          // Honor Retry-After header if present, otherwise use exponential backoff
          let delay: number;
          const retryAfter = response.headers()['retry-after'];
          if (retryAfter) {
            // Retry-After can be seconds or HTTP date
            const retrySeconds = parseInt(retryAfter, 10);
            if (!isNaN(retrySeconds)) {
              delay = retrySeconds * 1000;
            } else {
              // Try parsing as HTTP date
              const retryDate = new Date(retryAfter);
              delay = Math.max(0, retryDate.getTime() - Date.now());
            }
          } else {
            delay = initialDelayMs * Math.pow(2, attempt);
          }
          // Add jitter and cap at maxDelayMs to stay within test timeouts
          delay = Math.min(addJitter(delay), maxDelayMs);
          console.log(`Rate limited (attempt ${attempt + 1}/${maxRetries}), waiting ${delay}ms`);
          await sleep(delay);
          continue;
        }
        throw new Error(`Rate limited after ${maxRetries} retries: ${text.substring(0, 200)}`);
      }

      // Check for server errors (5xx) - these might be transient
      if (status >= 500 && attempt < maxRetries) {
        const baseDelay = initialDelayMs * Math.pow(2, attempt);
        const delay = Math.min(addJitter(baseDelay), maxDelayMs);
        console.log(`Server error ${status}, waiting ${delay}ms before retry ${attempt + 1}/${maxRetries}`);
        await sleep(delay);
        lastError = new Error(`Server error ${status}: ${text.substring(0, 200)}`);
        continue;
      }

      // Handle empty body (e.g., 204 No Content)
      if (!text || text.trim() === '') {
        // For 2xx with empty body, return empty success object
        if (status >= 200 && status < 300) {
          return { success: true } as T;
        }
        throw new Error(`Empty response with status ${status}`);
      }

      // Check content type before parsing
      const contentType = response.headers()['content-type'] || '';

      // Try to parse as JSON
      if (contentType.includes('application/json') || text.trim().startsWith('{') || text.trim().startsWith('[')) {
        try {
          return JSON.parse(text) as T;
        } catch (parseError) {
          // JSON parse failed - include status and URL for debugging
          const url = response.url();
          throw new Error(
            `Failed to parse JSON response (status ${status}, url: ${url}): ${text.substring(0, 200)}`
          );
        }
      }

      // Non-JSON response
      if (status >= 200 && status < 300) {
        // Success but not JSON - wrap in success object
        return { success: true, data: text } as T;
      }

      // Error response that's not JSON
      throw new Error(`HTTP ${status}: ${text.substring(0, 200)}`);
    }

    throw lastError || new Error('Request failed after max retries');
  }

  // ==================== Health & Status ====================

  /**
   * Check if Docksmith API is healthy
   */
  async health(): Promise<{ success: boolean }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/health`)
    );
  }

  /**
   * Get Docker configuration
   */
  async dockerConfig(): Promise<any> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/docker-config`)
    );
  }

  /**
   * Get container status list
   */
  async status(): Promise<{
    success: boolean;
    data?: {
      containers: ContainerInfo[];
      last_check?: string;
    };
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/status`)
    );
  }

  // ==================== Container Operations ====================

  /**
   * Trigger a background check for updates
   */
  async triggerCheck(): Promise<{ success: boolean }> {
    return this.parseWithRateLimitRetry<{ success: boolean }>(
      () => this.request.post(`${this.baseUrl}/api/trigger-check`)
    );
  }

  /**
   * Synchronous check for updates
   */
  async check(): Promise<{ success: boolean }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/check`)
    );
  }

  /**
   * Update a single container
   */
  async update(containerName: string, targetVersion: string, force = false): Promise<{
    success: boolean;
    data?: { operation_id: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/update`, {
        data: {
          container_name: containerName,
          target_version: targetVersion,
          force,
        },
      })
    );
  }

  /**
   * Batch update multiple containers
   */
  async batchUpdate(containers: Array<{ name: string; target_version: string }>): Promise<{
    success: boolean;
    data?: { operations: any[] };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/update/batch`, {
        data: { containers },
      })
    );
  }

  /**
   * Rollback an operation
   */
  async rollback(operationId: string): Promise<{
    success: boolean;
    data?: { operation_id: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/rollback`, {
        data: { operation_id: operationId },
      })
    );
  }

  /**
   * Fix a compose mismatch for a container
   */
  async fixComposeMismatch(containerName: string): Promise<{
    success: boolean;
    data?: { operation_id: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/fix-compose-mismatch/${containerName}`)
    );
  }

  /**
   * Restart a container
   */
  async restart(containerName: string, force = false): Promise<{
    success: boolean;
    data?: {
      dependents_restarted?: string[];
      dependents_blocked?: string[];
    };
    error?: string;
  }> {
    const url = force
      ? `${this.baseUrl}/api/restart/container/${containerName}?force=true`
      : `${this.baseUrl}/api/restart/container/${containerName}`;
    return this.parseWithRateLimitRetry(
      () => this.request.post(url)
    );
  }

  // ==================== Stop and Remove ====================

  /**
   * Stop a running container
   */
  async stopContainer(containerName: string, timeout = 10): Promise<{
    success: boolean;
    data?: { operation_id: string; container: string; status: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/containers/${containerName}/stop?timeout=${timeout}`)
    );
  }

  /**
   * Start a stopped container
   */
  async startContainer(containerName: string): Promise<{
    success: boolean;
    data?: { container: string; status: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/containers/${containerName}/start`)
    );
  }

  /**
   * Remove a container
   */
  async removeContainer(containerName: string, force = false, removeVolumes = false): Promise<{
    success: boolean;
    data?: { operation_id: string; container: string; status: string };
    error?: string;
  }> {
    const params = new URLSearchParams();
    if (force) params.set('force', 'true');
    if (removeVolumes) params.set('volumes', 'true');
    const queryString = params.toString();
    const url = queryString
      ? `${this.baseUrl}/api/containers/${containerName}?${queryString}`
      : `${this.baseUrl}/api/containers/${containerName}`;
    return this.parseWithRateLimitRetry(
      () => this.request.delete(url)
    );
  }

  // ==================== Labels ====================

  /**
   * Get labels for a container
   */
  async getLabels(containerName: string): Promise<{
    success: boolean;
    data?: { labels: Record<string, string> };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/labels/${containerName}`)
    );
  }

  /**
   * Set labels on a container
   */
  async setLabels(containerName: string, labels: LabelSetOptions): Promise<{
    success: boolean;
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/labels/set`, {
        data: {
          container: containerName,
          ...labels,
        },
      })
    );
  }

  /**
   * Remove labels from a container
   */
  async removeLabels(containerName: string, labelNames: string[], options?: { force?: boolean; no_restart?: boolean }): Promise<{
    success: boolean;
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/labels/remove`, {
        data: {
          container: containerName,
          label_names: labelNames,
          ...options,
        },
      })
    );
  }

  // ==================== Scripts ====================

  /**
   * Get list of available scripts
   */
  async getScripts(): Promise<{
    success: boolean;
    data?: { scripts: Script[]; count: number };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/scripts`)
    );
  }

  /**
   * Get script assignments
   */
  async getScriptsAssigned(): Promise<{
    success: boolean;
    data?: { assignments: ScriptAssignment[]; count: number };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/scripts/assigned`)
    );
  }

  /**
   * Assign a script to a container
   */
  async assignScript(containerName: string, scriptPath: string): Promise<{
    success: boolean;
    data?: { container: string; script: string; message: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/scripts/assign`, {
        data: {
          container_name: containerName,
          script_path: scriptPath,
        },
      })
    );
  }

  /**
   * Unassign a script from a container
   */
  async unassignScript(containerName: string): Promise<{
    success: boolean;
    data?: { container: string; message: string };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.delete(`${this.baseUrl}/api/scripts/assign/${containerName}`)
    );
  }

  // ==================== Policies ====================

  /**
   * Get rollback policies
   */
  async getPolicies(): Promise<{
    success: boolean;
    data?: { global_policy: any };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/policies`)
    );
  }

  // ==================== Registry ====================

  /**
   * Get available tags for an image from registry
   */
  async getRegistryTags(imageRef: string): Promise<{
    success: boolean;
    data?: { image_ref: string; tags: string[]; count: number };
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/registry/tags/${encodeURIComponent(imageRef)}`)
    );
  }

  // ==================== Additional Restart Operations ====================

  /**
   * Start a restart operation (SSE-based, returns operation ID)
   */
  async restartStart(containerName: string, force = false): Promise<{
    success: boolean;
    data?: { operation_id: string; container_name: string; status: string };
    error?: string;
  }> {
    const url = force
      ? `${this.baseUrl}/api/restart/start/${containerName}?force=true`
      : `${this.baseUrl}/api/restart/start/${containerName}`;
    return this.parseWithRateLimitRetry(
      () => this.request.post(url)
    );
  }

  /**
   * Restart all containers in a stack
   */
  async restartStack(stackName: string): Promise<{
    success: boolean;
    data?: RestartResponse;
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.post(`${this.baseUrl}/api/restart/stack/${stackName}`)
    );
  }

  // ==================== Operations & History ====================

  /**
   * Get operations list
   */
  async getOperations(limit = 10): Promise<{
    success: boolean;
    data?: {
      operations: Operation[];
      count: number;
    };
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/operations?limit=${limit}`)
    );
  }

  /**
   * Get a specific operation by ID
   */
  async getOperation(operationId: string): Promise<{
    success: boolean;
    data?: Operation;
    error?: string;
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/operations/${operationId}`)
    );
  }

  /**
   * Get history entries
   */
  async getHistory(limit = 10): Promise<{
    success: boolean;
    data?: {
      entries: any[];
      count: number;
    };
  }> {
    return this.parseWithRateLimitRetry(
      () => this.request.get(`${this.baseUrl}/api/history?limit=${limit}`)
    );
  }

  // ==================== Helper Methods ====================

  /**
   * Get container info by name
   */
  async getContainer(name: string): Promise<ContainerInfo | null> {
    const status = await this.status();
    if (!status.success || !status.data) return null;
    return status.data.containers.find(c => c.container_name === name) || null;
  }

  /**
   * Get container status string
   */
  async getContainerStatus(name: string): Promise<string | null> {
    const container = await this.getContainer(name);
    return container?.status || null;
  }

  /**
   * Get container current version
   */
  async getContainerVersion(name: string): Promise<string | null> {
    const container = await this.getContainer(name);
    return container?.current_version || null;
  }

  /**
   * Wait for container to reach a specific status using smart polling.
   * Uses standalone sleep to avoid page lifecycle issues.
   */
  async waitForStatus(containerName: string, expectedStatus: string, timeout = 30000): Promise<boolean> {
    const startTime = Date.now();
    const pollInterval = 2000;
    while (Date.now() - startTime < timeout) {
      const status = await this.getContainerStatus(containerName);
      if (status === expectedStatus) return true;
      await sleep(pollInterval);
    }
    return false;
  }

  /**
   * Wait for container status to change from a specific value.
   * Uses standalone sleep to avoid page lifecycle issues.
   */
  async waitForStatusNot(containerName: string, notStatus: string, timeout = 30000): Promise<string | null> {
    const startTime = Date.now();
    const pollInterval = 2000;
    while (Date.now() - startTime < timeout) {
      const status = await this.getContainerStatus(containerName);
      if (status !== notStatus) return status;
      await sleep(pollInterval);
    }
    return null;
  }

  /**
   * Wait for container to reach a specific version.
   * Uses standalone sleep to avoid page lifecycle issues.
   */
  async waitForVersion(containerName: string, expectedVersion: string, timeout = 60000): Promise<boolean> {
    const startTime = Date.now();
    const pollInterval = 2000;
    while (Date.now() - startTime < timeout) {
      const version = await this.getContainerVersion(containerName);
      if (version === expectedVersion) return true;
      await sleep(pollInterval);
    }
    return false;
  }

  /**
   * Wait for an operation to complete.
   * Uses standalone sleep to avoid page lifecycle issues.
   */
  async waitForOperation(operationId: string, timeout = 120000): Promise<Operation | null> {
    const startTime = Date.now();
    const pollInterval = 3000;
    while (Date.now() - startTime < timeout) {
      const result = await this.getOperation(operationId);
      if (result.success && result.data) {
        const op = result.data;
        if (op.status === 'complete' || op.status === 'failed') {
          return op;
        }
      }
      await sleep(pollInterval);
    }
    return null;
  }

  /**
   * Trigger check and wait for refresh.
   * Uses standalone sleep to avoid page lifecycle issues.
   */
  async triggerCheckAndWait(waitMs = 5000): Promise<void> {
    await this.triggerCheck();
    await sleep(waitMs);
  }

  /**
   * Wait for container to reach a specific status with a callback for custom matching.
   * This is useful for more complex conditions (e.g., status is one of several values).
   */
  async waitForCondition<T>(
    checkFn: () => Promise<T>,
    conditionFn: (result: T) => boolean,
    options: { timeout?: number; pollInterval?: number; description?: string } = {}
  ): Promise<T | null> {
    const { timeout = 30000, pollInterval = 2000, description = 'condition' } = options;
    const startTime = Date.now();
    let lastResult: T | null = null;

    while (Date.now() - startTime < timeout) {
      try {
        lastResult = await checkFn();
        if (conditionFn(lastResult)) {
          return lastResult;
        }
      } catch (error) {
        // Log but continue polling
        console.log(`Error while waiting for ${description}: ${error}`);
      }
      await sleep(pollInterval);
    }
    return lastResult;
  }
}

// ==================== Types ====================

export interface ContainerInfo {
  container_name: string;
  status: string;
  image: string;
  current_tag?: string;
  current_version?: string;
  latest_version?: string;
  recommended_tag?: string;
  change_type?: number;
  stack?: string;
  service?: string;
  labels?: Record<string, string>;
  compose_labels?: Record<string, string>;
  labels_out_of_sync?: boolean;
  dependencies?: string[];
  pre_update_check_fail?: string;
}

export interface Operation {
  operation_id: string;
  operation_type: string;
  type?: string; // deprecated, use operation_type
  status: string;
  container_name?: string;
  stack_name?: string;
  old_version?: string;
  new_version?: string;
  error_message?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface LabelSetOptions {
  ignore?: boolean;
  allow_latest?: boolean;
  version_pin_major?: boolean;
  version_pin_minor?: boolean;
  version_pin_patch?: boolean;
  version_min?: string;
  version_max?: string;
  tag_regex?: string;
  script?: string;
  restart_after?: string;
  force?: boolean;
  no_restart?: boolean;
}

export interface Script {
  name: string;
  path: string;
  description?: string;
}

export interface ScriptAssignment {
  container: string;
  script: string;
  source?: string;
}

export interface RestartResponse {
  success: boolean;
  message: string;
  container_names: string[];
  dependents_restarted?: string[];
  dependents_blocked?: string[];
  errors?: string[];
}
