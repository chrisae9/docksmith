import type {
  CheckResponse,
  OperationsResponse,
  HistoryResponse,
  HealthCheckResponse,
  DockerConfigResponse,
  APIResponse,
  ScriptsResponse,
  ScriptAssignmentsResponse,
  RegistryTagsAPIResponse,
  SetLabelsRequest,
  SetLabelsResponse,
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

// Health check
export async function checkHealth(): Promise<HealthCheckResponse> {
  return fetchAPI('/health');
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

// Get single operation by ID
export async function getOperation(operationId: string): Promise<APIResponse<unknown>> {
  return fetchAPI(`/operations/${operationId}`);
}

// Check and update history
export async function getHistory(params?: {
  limit?: number;
  type?: 'check' | 'update';
  container?: string;
}): Promise<HistoryResponse> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set('limit', params.limit.toString());
  if (params?.type) searchParams.set('type', params.type);
  if (params?.container) searchParams.set('container', params.container);

  const query = searchParams.toString();
  return fetchAPI(`/history${query ? `?${query}` : ''}`);
}

// Trigger container update
export async function triggerUpdate(containerName: string, targetVersion?: string): Promise<APIResponse<{
  operation_id: string;
  container_name: string;
  target_version: string;
  status: string;
}>> {
  return fetchAPI('/update', {
    method: 'POST',
    body: JSON.stringify({
      container_name: containerName,
      target_version: targetVersion || '',
    }),
  });
}

// Trigger batch container update (grouped by stack)
export async function triggerBatchUpdate(containers: Array<{
  name: string;
  target_version: string;
  stack: string;
}>): Promise<APIResponse<{
  operations: Array<{
    stack: string;
    containers: string[];
    operation_id?: string;
    status: string;
    error?: string;
  }>;
  status: string;
}>> {
  return fetchAPI('/update/batch', {
    method: 'POST',
    body: JSON.stringify({
      containers,
    }),
  });
}

// Get rollback information
export async function getRollbackInfo(operationId: string): Promise<APIResponse<unknown>> {
  return fetchAPI('/rollback', {
    method: 'POST',
    body: JSON.stringify({
      operation_id: operationId,
    }),
  });
}

// Script Management APIs

// Get list of available scripts
export async function getScripts(): Promise<APIResponse<ScriptsResponse>> {
  return fetchAPI('/scripts');
}

// Get script assignments
export async function getScriptAssignments(): Promise<APIResponse<ScriptAssignmentsResponse>> {
  return fetchAPI('/scripts/assigned');
}

// Assign script to container
export async function assignScript(containerName: string, scriptPath: string): Promise<APIResponse<{
  success: boolean;
  container: string;
  script: string;
  message: string;
}>> {
  return fetchAPI('/scripts/assign', {
    method: 'POST',
    body: JSON.stringify({
      container_name: containerName,
      script_path: scriptPath,
    }),
  });
}

// Unassign script from container
export async function unassignScript(containerName: string): Promise<APIResponse<{
  success: boolean;
  container: string;
  message: string;
}>> {
  return fetchAPI(`/scripts/assign/${containerName}`, {
    method: 'DELETE',
  });
}

// Set ignore flag for container
export async function setIgnore(containerName: string, ignore: boolean): Promise<APIResponse<{
  success: boolean;
  container: string;
  ignore: boolean;
  message: string;
}>> {
  return fetchAPI('/settings/ignore', {
    method: 'POST',
    body: JSON.stringify({
      container_name: containerName,
      ignore,
    }),
  });
}

// Set allow-latest flag for container
export async function setAllowLatest(containerName: string, allowLatest: boolean): Promise<APIResponse<{
  success: boolean;
  container: string;
  allow_latest: boolean;
  message: string;
}>> {
  return fetchAPI('/settings/allow-latest', {
    method: 'POST',
    body: JSON.stringify({
      container_name: containerName,
      allow_latest: allowLatest,
    }),
  });
}

// Label management (atomic: compose + restart)
export interface LabelOperationResult {
  success: boolean;
  container: string;
  operation: string;
  labels_modified?: Record<string, string>;
  labels_removed?: string[];
  compose_file: string;
  restarted: boolean;
  pre_check_ran: boolean;
  pre_check_passed?: boolean;
  message?: string;
}

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

// Restart operations
export interface RestartResponse {
  success: boolean;
  message: string;
  container_names: string[];
  dependents_restarted?: string[];
  dependents_blocked?: string[];
  errors?: string[];
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

// Restart a single container (legacy - no SSE progress)
export async function restartContainer(containerName: string, force = false): Promise<APIResponse<RestartResponse>> {
  const url = force ? `/restart/container/${containerName}?force=true` : `/restart/container/${containerName}`;
  return fetchAPI(url, {
    method: 'POST',
  });
}

// Restart all containers in a stack
export async function restartStack(stackName: string): Promise<APIResponse<RestartResponse>> {
  return fetchAPI(`/restart/stack/${stackName}`, {
    method: 'POST',
  });
}

// Registry tags (for regex testing UI)
// Note: imageRef is not encoded because the backend uses a wildcard path pattern {imageRef...}
// that expects literal slashes in the path (e.g., /registry/tags/linuxserver/syncthing)
export async function getRegistryTags(imageRef: string): Promise<RegistryTagsAPIResponse> {
  return fetchAPI(`/registry/tags/${imageRef}`);
}

// Trigger background check (uses cached registry data)
export async function triggerBackgroundCheck(): Promise<CheckResponse> {
  return fetchAPI('/trigger-check', {
    method: 'POST',
  });
}
