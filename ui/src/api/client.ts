import type {
  CheckResponse,
  OperationsResponse,
  HistoryResponse,
  BackupsResponse,
  HealthCheckResponse,
  APIResponse,
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

  const data = await response.json();
  return data as APIResponse<T>;
}

// Health check
export async function checkHealth(): Promise<HealthCheckResponse> {
  return fetchAPI('/health');
}

// Container discovery and update checking
export async function checkContainers(): Promise<CheckResponse> {
  return fetchAPI('/check');
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

// Compose backups
export async function getBackups(params?: {
  limit?: number;
  container?: string;
}): Promise<BackupsResponse> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set('limit', params.limit.toString());
  if (params?.container) searchParams.set('container', params.container);

  const query = searchParams.toString();
  return fetchAPI(`/backups${query ? `?${query}` : ''}`);
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
