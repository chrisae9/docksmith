import type {
  CheckResponse,
  OperationsResponse,
  DockerConfigResponse,
  APIResponse,
  ScriptsResponse,
  RegistryTagsAPIResponse,
  SetLabelsRequest,
  SetLabelsResponse,
  ExplorerResponse,
  ContainerInfo,
  LabelOperationResult,
  NotifySettings,
  NotifySettingsPayload,
  NotifyCronValidation,
  NotifyService,
  NotifyServiceInput,
} from '../types/api';

const API_BASE = '/api';

// Request time budgets. The default bounds ordinary reads/writes; longer tiers
// cover endpoints that do synchronous Docker or registry work server-side.
const DEFAULT_TIMEOUT_MS = 15_000;
const LONG_TIMEOUT_MS = 120_000;
const PRUNE_TIMEOUT_MS = 300_000;
const BATCH_TIMEOUT_MS = 600_000;

interface APIFetchOptions extends RequestInit {
  // Abort the request after this many ms (0 disables the timeout).
  timeoutMs?: number;
}

// Generic fetch wrapper with error handling and a request deadline
async function fetchAPI<T>(endpoint: string, options?: APIFetchOptions): Promise<APIResponse<T>> {
  const { timeoutMs = DEFAULT_TIMEOUT_MS, signal: callerSignal, ...init } = options ?? {};
  const controller = new AbortController();
  let timedOut = false;
  const timer = timeoutMs > 0
    ? window.setTimeout(() => { timedOut = true; controller.abort(); }, timeoutMs)
    : null;
  const onCallerAbort = () => controller.abort();
  if (callerSignal?.aborted) controller.abort();
  callerSignal?.addEventListener('abort', onCallerAbort);

  // The timer stays armed until the body is parsed (cleared in finally), so a
  // response that sends headers and then stalls its body still times out.
  try {
    const headers = new Headers(init.headers);
    if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...init,
      headers,
      signal: controller.signal,
    });

    // Check if response is OK before attempting to parse JSON
    if (!response.ok) {
      // Try to parse error response as JSON, fall back to status text
      try {
        const errorData = await response.json();
        return errorData as APIResponse<T>;
      } catch (parseErr) {
        // Let a timeout during the body read surface as a timeout
        if (timedOut) throw parseErr;
        // Response is not JSON (e.g., 502 Bad Gateway, network error)
        return {
          success: false,
          error: `HTTP ${response.status}: ${response.statusText}`,
          timestamp: new Date().toISOString(),
          version: '',
        } as APIResponse<T>;
      }
    }

    const data = await response.json();
    return data as APIResponse<T>;
  } catch (err) {
    if (timedOut) {
      return {
        success: false,
        error: 'Request timed out',
        timestamp: new Date().toISOString(),
        version: '',
      } as APIResponse<T>;
    }
    throw err;
  } finally {
    if (timer !== null) window.clearTimeout(timer);
    callerSignal?.removeEventListener('abort', onCallerAbort);
  }
}

// Docker configuration
export async function getDockerConfig(): Promise<DockerConfigResponse> {
  return fetchAPI('/docker-config');
}

// Container discovery and update checking
export async function checkContainers(): Promise<CheckResponse> {
  return fetchAPI('/check', { timeoutMs: LONG_TIMEOUT_MS });
}

// Get cached container status (from background checker)
export async function getContainerStatus(timeoutMs?: number): Promise<CheckResponse> {
  return fetchAPI('/status', { timeoutMs });
}

// Trigger a background update check (returns immediately; poll /status for progress)
export async function triggerCheck(): Promise<APIResponse<unknown>> {
  return fetchAPI('/trigger-check', { method: 'POST' });
}

// Recheck a single container (synchronous, bypasses cache)
export async function recheckContainer(containerName: string): Promise<APIResponse<ContainerInfo>> {
  return fetchAPI(`/container/${encodeURIComponent(containerName)}/recheck`, { timeoutMs: LONG_TIMEOUT_MS });
}

// Operations history
export async function getOperations(params?: {
  limit?: number;
  status?: string;
  container?: string;
  cursor?: string;
  type?: string;
  date_from?: string;
  date_to?: string;
}): Promise<OperationsResponse> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set('limit', params.limit.toString());
  if (params?.status) searchParams.set('status', params.status);
  if (params?.container) searchParams.set('container', params.container);
  if (params?.cursor) searchParams.set('cursor', params.cursor);
  if (params?.type) searchParams.set('type', params.type);
  if (params?.date_from) searchParams.set('date_from', params.date_from);
  if (params?.date_to) searchParams.set('date_to', params.date_to);

  const query = searchParams.toString();
  return fetchAPI(`/operations${query ? `?${query}` : ''}`);
}

// Clear history
export async function clearHistory(olderThanDays?: number): Promise<APIResponse<{ deleted: number }>> {
  return fetchAPI('/history/clear', {
    method: 'DELETE',
    body: JSON.stringify({ older_than_days: olderThanDays || 0 }),
  });
}

// Settings
export async function getSetting(key: string): Promise<APIResponse<{ key: string; value: string }>> {
  return fetchAPI(`/settings/${encodeURIComponent(key)}`);
}

export async function setSetting(key: string, value: string): Promise<APIResponse<void>> {
  return fetchAPI(`/settings/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
  });
}

// Trigger batch container update (grouped by stack)
export async function triggerBatchUpdate(containers: Array<{
  name: string;
  target_version: string;
  stack: string;
  force?: boolean;
  change_type?: number;
  old_resolved_version?: string;
  new_resolved_version?: string;
}>): Promise<APIResponse<{
  operations: Array<{
    stack: string;
    containers: string[];
    operation_id?: string;
    status: string;
    error?: string;
  }>;
  batch_group_id: string;
  status: string;
}>> {
  return fetchAPI('/update/batch', {
    method: 'POST',
    body: JSON.stringify({
      containers,
    }),
  });
}

// Get all operations in a batch group
export async function getOperationsByGroup(groupId: string): Promise<APIResponse<{
  batch_group_id: string;
  operations: import('../types/api').UpdateOperation[];
  count: number;
}>> {
  return fetchAPI(`/operations/group/${groupId}`);
}

// Rollback specific containers from an operation
export async function rollbackContainers(
  operationId: string,
  containerNames: string[],
  force = false
): Promise<APIResponse<{
  operation_id: string;
  message: string;
}>> {
  return fetchAPI('/rollback/containers', {
    method: 'POST',
    body: JSON.stringify({
      operation_id: operationId,
      container_names: containerNames,
      force,
    }),
  });
}

// Script Management APIs

// Get list of available scripts
export async function getScripts(): Promise<APIResponse<ScriptsResponse>> {
  return fetchAPI('/scripts');
}

// Label management (atomic: compose + restart)

// Get labels for a container
export async function getContainerLabels(containerName: string): Promise<APIResponse<{
  container: string;
  labels: Record<string, string>;
}>> {
  return fetchAPI(`/labels/${containerName}`);
}

// Set labels atomically (updates compose + restarts container)
export async function setLabels(
  containerName: string,
  options: Omit<SetLabelsRequest, 'container'>
): Promise<SetLabelsResponse> {
  return fetchAPI('/labels/set', {
    method: 'POST',
    body: JSON.stringify({
      container: containerName,
      ...options,
    }),
  });
}

// Remove labels atomically (updates compose + restarts container)
export async function removeLabels(
  containerName: string,
  labelNames: string[],
  options?: {
    no_restart?: boolean;
    force?: boolean;
  }
): Promise<APIResponse<LabelOperationResult>> {
  return fetchAPI('/labels/remove', {
    method: 'POST',
    body: JSON.stringify({
      container: containerName,
      label_names: labelNames,
      ...options,
    }),
  });
}

// Start a restart operation via orchestrator (returns operation_id for SSE tracking)
export interface StartRestartResponse {
  operation_id: string;
  container_name: string;
  force: boolean;
  status: string;
}

export async function startRestart(containerName: string, force = false): Promise<APIResponse<StartRestartResponse>> {
  const url = force ? `/restart/start/${containerName}?force=true` : `/restart/start/${containerName}`;
  return fetchAPI(url, {
    method: 'POST',
  });
}

// Start a stack restart operation via orchestrator (single operation, dependency ordering)
export async function startStackRestart(
  stackName: string,
  containers: string[],
  force = false
): Promise<APIResponse<StartRestartResponse>> {
  const url = force
    ? `/restart/stack/start/${stackName}?force=true`
    : `/restart/stack/start/${stackName}`;
  return fetchAPI(url, {
    method: 'POST',
    body: JSON.stringify({ containers }),
  });
}

// Registry tags (for regex testing UI)
// Note: imageRef is not encoded because the backend uses a wildcard path pattern {imageRef...}
// that expects literal slashes in the path (e.g., /registry/tags/linuxserver/syncthing)
export async function getRegistryTags(imageRef: string): Promise<RegistryTagsAPIResponse> {
  return fetchAPI(`/registry/tags/${imageRef}`, { timeoutMs: LONG_TIMEOUT_MS });
}

// Explorer data (containers, images, networks, volumes)
export async function getExplorerData(options?: { includeVolumeSizes?: boolean }): Promise<ExplorerResponse> {
  return fetchAPI(`/explorer${options?.includeVolumeSizes ? '?include_volume_sizes=true' : ''}`);
}

// Container operations

// Get container logs
export async function getContainerLogs(
  name: string,
  options?: { tail?: number; timestamps?: boolean }
): Promise<APIResponse<{ container: string; logs: string; tail: string }>> {
  const params = new URLSearchParams();
  if (options?.tail) params.set('tail', options.tail.toString());
  if (options?.timestamps) params.set('timestamps', 'true');
  const query = params.toString();
  return fetchAPI(`/containers/${encodeURIComponent(name)}/logs${query ? `?${query}` : ''}`);
}

// Inspect container (detailed info)
export async function inspectContainer(
  name: string
): Promise<APIResponse<import('../types/api').ContainerInspect>> {
  return fetchAPI(`/containers/${encodeURIComponent(name)}/inspect`);
}

// Stop a running container
export async function stopContainer(
  name: string,
  timeout?: number
): Promise<APIResponse<{ container: string; status: string; message: string; operation_id?: string }>> {
  const params = timeout ? `?timeout=${timeout}` : '';
  return fetchAPI(`/containers/${encodeURIComponent(name)}/stop${params}`, {
    method: 'POST',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Start a stopped container
export async function startContainer(
  name: string
): Promise<APIResponse<{ container: string; status: string; message: string; operation_id?: string }>> {
  return fetchAPI(`/containers/${encodeURIComponent(name)}/start`, {
    method: 'POST',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Remove a container
export async function removeContainer(
  name: string,
  options?: { force?: boolean; volumes?: boolean }
): Promise<APIResponse<{ container: string; status: string; message: string; operation_id?: string; force: boolean; volumes: boolean }>> {
  const params = new URLSearchParams();
  if (options?.force) params.set('force', 'true');
  if (options?.volumes) params.set('volumes', 'true');
  const query = params.toString();
  return fetchAPI(`/containers/${encodeURIComponent(name)}${query ? `?${query}` : ''}`, {
    method: 'DELETE',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Remove a network
export async function removeNetwork(
  id: string
): Promise<APIResponse<{ message: string }>> {
  return fetchAPI(`/networks/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Remove a volume
export async function removeVolume(
  name: string,
  options?: { force?: boolean }
): Promise<APIResponse<{ message: string }>> {
  const params = new URLSearchParams();
  if (options?.force) params.set('force', 'true');
  const query = params.toString();
  return fetchAPI(`/volumes/${encodeURIComponent(name)}${query ? `?${query}` : ''}`, {
    method: 'DELETE',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Remove an image
export async function removeImage(
  id: string,
  options?: { force?: boolean }
): Promise<APIResponse<{ message: string; deleted: unknown[] }>> {
  const params = new URLSearchParams();
  if (options?.force) params.set('force', 'true');
  const query = params.toString();
  return fetchAPI(`/images/${encodeURIComponent(id)}${query ? `?${query}` : ''}`, {
    method: 'DELETE',
    timeoutMs: LONG_TIMEOUT_MS,
  });
}

// Prune response types
export interface PruneResponse {
  message: string;
  items_deleted: string[];
  space_reclaimed?: number;
}

// Prune stopped containers
export async function pruneContainers(): Promise<APIResponse<PruneResponse>> {
  return fetchAPI('/prune/containers', { method: 'POST', timeoutMs: PRUNE_TIMEOUT_MS });
}

// Prune unused images
export async function pruneImages(
  options?: { all?: boolean }
): Promise<APIResponse<PruneResponse>> {
  const params = new URLSearchParams();
  if (options?.all) params.set('all', 'true');
  const query = params.toString();
  return fetchAPI(`/prune/images${query ? `?${query}` : ''}`, { method: 'POST', timeoutMs: PRUNE_TIMEOUT_MS });
}

// Prune unused networks
export async function pruneNetworks(): Promise<APIResponse<PruneResponse>> {
  return fetchAPI('/prune/networks', { method: 'POST', timeoutMs: PRUNE_TIMEOUT_MS });
}

// Prune unused volumes
export async function pruneVolumes(): Promise<APIResponse<PruneResponse>> {
  return fetchAPI('/prune/volumes', { method: 'POST', timeoutMs: PRUNE_TIMEOUT_MS });
}

// Batch label operations - apply labels to multiple containers
export async function batchSetLabels(
  operations: Array<Omit<SetLabelsRequest, 'no_restart' | 'force'>>
): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    error?: string;
    operation_id?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/labels/batch', {
    method: 'POST',
    body: JSON.stringify({ operations }),
  });
}

// Rollback label changes from a previous operation or batch
export async function rollbackLabels(params: {
  batch_group_id?: string;
  operation_ids?: string[];
  container_names?: string[];
  force?: boolean;
}): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    error?: string;
    operation_id?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/labels/rollback', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Batch start - start multiple containers with a shared batch_group_id
export async function batchStartContainers(
  containers: string[]
): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    operation_id?: string;
    error?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/containers/batch/start', {
    method: 'POST',
    body: JSON.stringify({ containers }),
    timeoutMs: BATCH_TIMEOUT_MS,
  });
}

// Batch stop - stop multiple containers with a shared batch_group_id
export async function batchStopContainers(
  containers: string[],
  timeout?: number
): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    operation_id?: string;
    error?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/containers/batch/stop', {
    method: 'POST',
    body: JSON.stringify({ containers, timeout }),
    timeoutMs: BATCH_TIMEOUT_MS,
  });
}

// Batch restart - restart multiple containers with a shared batch_group_id
export async function batchRestartContainers(
  containers: string[],
  timeout?: number
): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    operation_id?: string;
    error?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/containers/batch/restart', {
    method: 'POST',
    body: JSON.stringify({ containers, timeout }),
    timeoutMs: BATCH_TIMEOUT_MS,
  });
}

// Batch remove - remove multiple containers with a shared batch_group_id
export async function batchRemoveContainers(
  containers: string[],
  force?: boolean
): Promise<APIResponse<{
  results: Array<{
    container: string;
    success: boolean;
    operation_id?: string;
    error?: string;
  }>;
  batch_group_id: string;
}>> {
  return fetchAPI('/containers/batch/remove', {
    method: 'POST',
    body: JSON.stringify({ containers, force }),
    timeoutMs: BATCH_TIMEOUT_MS,
  });
}

// Fix compose mismatch - sync container to compose file specification
export async function fixComposeMismatch(containerName: string): Promise<APIResponse<{
  operation_id: string;
  container_name: string;
  status: string;
  message: string;
}>> {
  return fetchAPI(`/fix-compose-mismatch/${encodeURIComponent(containerName)}`, {
    method: 'POST',
  });
}

// Notifications
export async function getNotifySettings(): Promise<APIResponse<NotifySettingsPayload>> {
  return fetchAPI('/notifications/settings');
}

export async function updateNotifySettings(settings: NotifySettings): Promise<APIResponse<NotifySettingsPayload>> {
  return fetchAPI('/notifications/settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  });
}

export async function validateNotifyCron(cron: string): Promise<APIResponse<NotifyCronValidation>> {
  return fetchAPI('/notifications/validate-cron', {
    method: 'POST',
    body: JSON.stringify({ cron }),
  });
}

export async function listNotifyServices(): Promise<APIResponse<NotifyService[]>> {
  return fetchAPI('/notifications/services');
}

export async function createNotifyService(service: NotifyServiceInput): Promise<APIResponse<NotifyService>> {
  return fetchAPI('/notifications/services', {
    method: 'POST',
    body: JSON.stringify(service),
  });
}

export async function updateNotifyService(id: number, service: NotifyServiceInput): Promise<APIResponse<NotifyService>> {
  return fetchAPI(`/notifications/services/${id}`, {
    method: 'PUT',
    body: JSON.stringify(service),
  });
}

export async function deleteNotifyService(id: number): Promise<APIResponse<{ deleted: boolean }>> {
  return fetchAPI(`/notifications/services/${id}`, { method: 'DELETE' });
}

export async function testNotifyService(id: number): Promise<APIResponse<{ sent: boolean }>> {
  return fetchAPI(`/notifications/services/${id}/test`, { method: 'POST', timeoutMs: LONG_TIMEOUT_MS });
}
