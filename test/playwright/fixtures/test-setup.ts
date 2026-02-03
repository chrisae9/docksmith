import { test as base, expect, type Page, type APIRequestContext } from '@playwright/test';

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

  // ==================== Health & Status ====================

  /**
   * Check if Docksmith API is healthy
   */
  async health(): Promise<{ success: boolean }> {
    const response = await this.request.get(`${this.baseUrl}/api/health`);
    return response.json();
  }

  /**
   * Get Docker configuration
   */
  async dockerConfig(): Promise<any> {
    const response = await this.request.get(`${this.baseUrl}/api/docker-config`);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/status`);
    return response.json();
  }

  // ==================== Container Operations ====================

  /**
   * Trigger a background check for updates
   */
  async triggerCheck(): Promise<{ success: boolean }> {
    const response = await this.request.post(`${this.baseUrl}/api/trigger-check`);
    return response.json();
  }

  /**
   * Synchronous check for updates
   */
  async check(): Promise<{ success: boolean }> {
    const response = await this.request.get(`${this.baseUrl}/api/check`);
    return response.json();
  }

  /**
   * Update a single container
   */
  async update(containerName: string, targetVersion: string, force = false): Promise<{
    success: boolean;
    data?: { operation_id: string };
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/update`, {
      data: {
        container_name: containerName,
        target_version: targetVersion,
        force,
      },
    });
    return response.json();
  }

  /**
   * Batch update multiple containers
   */
  async batchUpdate(containers: Array<{ name: string; target_version: string }>): Promise<{
    success: boolean;
    data?: { operations: any[] };
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/update/batch`, {
      data: { containers },
    });
    return response.json();
  }

  /**
   * Rollback an operation
   */
  async rollback(operationId: string): Promise<{
    success: boolean;
    data?: { operation_id: string };
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/rollback`, {
      data: { operation_id: operationId },
    });
    return response.json();
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
    const response = await this.request.post(url);
    return response.json();
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
    const response = await this.request.post(
      `${this.baseUrl}/api/containers/${containerName}/stop?timeout=${timeout}`
    );
    return response.json();
  }

  /**
   * Start a stopped container
   */
  async startContainer(containerName: string): Promise<{
    success: boolean;
    data?: { container: string; status: string };
    error?: string;
  }> {
    const response = await this.request.post(
      `${this.baseUrl}/api/containers/${containerName}/start`
    );
    return response.json();
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
    const response = await this.request.delete(url);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/labels/${containerName}`);
    return response.json();
  }

  /**
   * Set labels on a container
   */
  async setLabels(containerName: string, labels: LabelSetOptions): Promise<{
    success: boolean;
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/labels/set`, {
      data: {
        container: containerName,
        ...labels,
      },
    });
    return response.json();
  }

  /**
   * Remove labels from a container
   */
  async removeLabels(containerName: string, labelNames: string[], options?: { force?: boolean; no_restart?: boolean }): Promise<{
    success: boolean;
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/labels/remove`, {
      data: {
        container: containerName,
        label_names: labelNames,
        ...options,
      },
    });
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/scripts`);
    return response.json();
  }

  /**
   * Get script assignments
   */
  async getScriptsAssigned(): Promise<{
    success: boolean;
    data?: { assignments: ScriptAssignment[]; count: number };
    error?: string;
  }> {
    const response = await this.request.get(`${this.baseUrl}/api/scripts/assigned`);
    return response.json();
  }

  /**
   * Assign a script to a container
   */
  async assignScript(containerName: string, scriptPath: string): Promise<{
    success: boolean;
    data?: { container: string; script: string; message: string };
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/scripts/assign`, {
      data: {
        container_name: containerName,
        script_path: scriptPath,
      },
    });
    return response.json();
  }

  /**
   * Unassign a script from a container
   */
  async unassignScript(containerName: string): Promise<{
    success: boolean;
    data?: { container: string; message: string };
    error?: string;
  }> {
    const response = await this.request.delete(`${this.baseUrl}/api/scripts/assign/${containerName}`);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/policies`);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/registry/tags/${encodeURIComponent(imageRef)}`);
    return response.json();
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
    const response = await this.request.post(url);
    return response.json();
  }

  /**
   * Restart all containers in a stack
   */
  async restartStack(stackName: string): Promise<{
    success: boolean;
    data?: RestartResponse;
    error?: string;
  }> {
    const response = await this.request.post(`${this.baseUrl}/api/restart/stack/${stackName}`);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/operations?limit=${limit}`);
    return response.json();
  }

  /**
   * Get a specific operation by ID
   */
  async getOperation(operationId: string): Promise<{
    success: boolean;
    data?: Operation;
    error?: string;
  }> {
    const response = await this.request.get(`${this.baseUrl}/api/operations/${operationId}`);
    return response.json();
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
    const response = await this.request.get(`${this.baseUrl}/api/history?limit=${limit}`);
    return response.json();
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
   * Wait for container to reach a specific status
   */
  async waitForStatus(containerName: string, expectedStatus: string, timeout = 30000): Promise<boolean> {
    const startTime = Date.now();
    while (Date.now() - startTime < timeout) {
      const status = await this.getContainerStatus(containerName);
      if (status === expectedStatus) return true;
      await this.page.waitForTimeout(2000);
    }
    return false;
  }

  /**
   * Wait for container status to change from a specific value
   */
  async waitForStatusNot(containerName: string, notStatus: string, timeout = 30000): Promise<string | null> {
    const startTime = Date.now();
    while (Date.now() - startTime < timeout) {
      const status = await this.getContainerStatus(containerName);
      if (status !== notStatus) return status;
      await this.page.waitForTimeout(2000);
    }
    return null;
  }

  /**
   * Wait for container to reach a specific version
   */
  async waitForVersion(containerName: string, expectedVersion: string, timeout = 60000): Promise<boolean> {
    const startTime = Date.now();
    while (Date.now() - startTime < timeout) {
      const version = await this.getContainerVersion(containerName);
      if (version === expectedVersion) return true;
      await this.page.waitForTimeout(2000);
    }
    return false;
  }

  /**
   * Wait for an operation to complete
   */
  async waitForOperation(operationId: string, timeout = 120000): Promise<Operation | null> {
    const startTime = Date.now();
    while (Date.now() - startTime < timeout) {
      const result = await this.getOperation(operationId);
      if (result.success && result.data) {
        const op = result.data;
        if (op.status === 'complete' || op.status === 'failed') {
          return op;
        }
      }
      await this.page.waitForTimeout(3000);
    }
    return null;
  }

  /**
   * Trigger check and wait for refresh
   */
  async triggerCheckAndWait(waitMs = 5000): Promise<void> {
    await this.triggerCheck();
    await this.page.waitForTimeout(waitMs);
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
