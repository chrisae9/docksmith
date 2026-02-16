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
} from '../types/api';

const API_BASE = '/api';

// Generic fetch wrapper with error handling
async function fetchAPI<T>(endpoint: string, options?: RequestInit): Promise<APIResponse<T>> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    ...options,
  });

  // Check if response is OK before attempting to parse JSON
  if (!response.ok) {
    // Try to parse error response as JSON, fall back to status text
    try {
      const errorData = await response.json();
      return errorData as APIResponse<T>;
    } catch {
      // Response is not JSON (e.g., 502 Bad Gateway, network error)
      return {
        success: false,
        error: `HTTP ${response.status}: ${response.statusText}`,
      } as APIResponse<T>;
    }
  }

  const data = await response.json();
  return data as APIResponse<T>;
}

// Docker configuration
export async function getDockerConfig(): Promise<DockerConfigResponse> {
  return fetchAPI('/docker-config');
}

// Container discovery and update checking
export async function checkContainers(): Promise<CheckResponse> {
  return fetchAPI('/check');
}

// Get cached container status (from background checker)
export async function getContainerStatus(): Promise<CheckResponse> {
  return fetchAPI('/status');
}

// Recheck a single container (synchronous, bypasses cache)
export async function recheckContainer(containerName: string): Promise<APIResponse<ContainerInfo>> {
  return fetchAPI(`/container/${encodeURIComponent(containerName)}/recheck`);
}

// Operations history
export async function getOperations(params?: {
  limit?: number;
  status?: string;
  container?: string;
}): Promise<OperationsResponse> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set('limit', params.limit.toString());
  if (params?.status) searchParams.set('status', params.status);
  if (params?.container) searchParams.set('container', params.container);

  const query = searchParams.toString();
  return fetchAPI(`/operations${query ? `?${query}` : ''}`);
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
  return fetchAPI(`/registry/tags/${imageRef}`);
}

// Explorer data (containers, images, networks, volumes)
export async function getExplorerData(): Promise<ExplorerResponse> {
  return fetchAPI('/explorer');
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
  });
}

// Start a stopped container
export async function startContainer(
  name: string
): Promise<APIResponse<{ container: string; status: string; message: string; operation_id?: string }>> {
  return fetchAPI(`/containers/${encodeURIComponent(name)}/start`, {
    method: 'POST',
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
  });
}

// Remove a network
export async function removeNetwork(
  id: string
): Promise<APIResponse<{ message: string }>> {
  return fetchAPI(`/networks/${encodeURIComponent(id)}`, {
    method: 'DELETE',
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
  return fetchAPI('/prune/containers', { method: 'POST' });
}

// Prune unused images
export async function pruneImages(
  options?: { all?: boolean }
): Promise<APIResponse<PruneResponse>> {
  const params = new URLSearchParams();
  if (options?.all) params.set('all', 'true');
  const query = params.toString();
  return fetchAPI(`/prune/images${query ? `?${query}` : ''}`, { method: 'POST' });
}

// Prune unused networks
export async function pruneNetworks(): Promise<APIResponse<PruneResponse>> {
  return fetchAPI('/prune/networks', { method: 'POST' });
}

// Prune unused volumes
export async function pruneVolumes(): Promise<APIResponse<PruneResponse>> {
  return fetchAPI('/prune/volumes', { method: 'POST' });
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
