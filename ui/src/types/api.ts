// TypeScript types that match Go JSON structures EXACTLY
// This ensures zero drift between backend and frontend

// API Response wrapper (matches output.Response)
export interface APIResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
  timestamp: string;
  version: string;
}

// Container Info (matches update.ContainerInfo)
export interface ContainerInfo {
  container_name: string;
  image: string;
  current_tag?: string;
  current_version?: string;
  current_suffix?: string;
  latest_version?: string;
  current_digest?: string;
  latest_digest?: string;
  available_tags?: string[];
  change_type: number; // version.ChangeType
  status: string; // update.UpdateStatus
  error?: string;
  is_local: boolean;
  recommended_tag?: string;
  using_latest_tag: boolean;
  pre_update_check?: string;
  pre_update_check_fail?: string;
  pre_update_check_pass: boolean;
  health_status?: string; // Health status: "healthy", "unhealthy", "starting", "none"
  id: string;
  stack?: string;
  service?: string;
  dependencies?: string[];
  labels?: Record<string, string>;
}

// Stack (matches update.Stack)
export interface Stack {
  name: string;
  containers: ContainerInfo[];
  has_updates: boolean;
  all_updatable: boolean;
  update_priority?: string;
}

// Discovery Result (matches update.DiscoveryResult)
export interface DiscoveryResult {
  containers: ContainerInfo[];
  stacks: Record<string, Stack>;
  standalone_containers: ContainerInfo[];
  update_order: string[];
  total_checked: number;
  updates_found: number;
  up_to_date: number;
  local_images: number;
  failed: number;
  ignored: number;
}

// Update Operation (matches storage.UpdateOperation)
export interface UpdateOperation {
  id: number;
  operation_id: string;
  container_id: string;
  container_name: string;
  stack_name?: string;
  operation_type: string;
  status: string;
  old_version?: string;
  new_version: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
  dependents_affected?: string[];
  rollback_occurred: boolean;
  created_at: string;
  updated_at: string;
}

// Compose Backup (matches storage.ComposeBackup)
export interface ComposeBackup {
  id: number;
  operation_id: string;
  container_name: string;
  stack_name?: string;
  compose_file_path: string;
  backup_file_path: string;
  backup_timestamp: string;
  created_at: string;
}

// Script (matches scripts.Script)
export interface Script {
  name: string;
  path: string;
  relative_path: string;
  executable: boolean;
  size: number;
  modified_time: string;
}

// Script Assignment (matches scripts.Assignment)
export interface ScriptAssignment {
  container_name: string;
  script_path: string;
  enabled: boolean;
  ignore: boolean;
  allow_latest: boolean;
  assigned_at: string;
  assigned_by?: string;
  updated_at: string;
}

// Scripts Response
export interface ScriptsResponse {
  scripts: Script[];
  count: number;
}

// Script Assignments Response
export interface ScriptAssignmentsResponse {
  assignments: ScriptAssignment[];
  count: number;
}

// History Entry (matches api.HistoryEntry)
export interface HistoryEntry {
  timestamp: string;
  type: 'check' | 'update';
  container_name: string;
  image?: string;
  current_version?: string;
  latest_version?: string;
  from_version?: string;
  to_version?: string;
  status: string;
  operation?: string;
  success?: boolean;
  error?: string;
}

// Health Check Response
export interface HealthResponse {
  status: string;
  services: {
    docker: boolean;
    storage: boolean;
  };
}

// Update Status Constants (matches update.UpdateStatus)
export const UpdateStatus = {
  Unknown: 'UNKNOWN',
  UpToDate: 'UP_TO_DATE',
  UpToDatePinnable: 'UP_TO_DATE_PINNABLE',
  UpdateAvailable: 'UPDATE_AVAILABLE',
  UpdateAvailableBlocked: 'UPDATE_AVAILABLE_BLOCKED',
  LocalImage: 'LOCAL_IMAGE',
  CheckFailed: 'CHECK_FAILED',
  MetadataUnavailable: 'METADATA_UNAVAILABLE',
  ComposeMismatch: 'COMPOSE_MISMATCH',
  Ignored: 'IGNORED',
} as const;

// Change Type Constants (matches version.ChangeType)
export const ChangeType = {
  Unknown: 0,
  NoChange: 1,
  PatchChange: 2,
  MinorChange: 3,
  MajorChange: 4,
  Downgrade: 5,
} as const;

// Helper function to get change type name
export function getChangeTypeName(changeType: number): string {
  switch (changeType) {
    case ChangeType.NoChange:
      return 'rebuild';
    case ChangeType.PatchChange:
      return 'patch';
    case ChangeType.MinorChange:
      return 'minor';
    case ChangeType.MajorChange:
      return 'major';
    case ChangeType.Downgrade:
      return 'downgrade';
    default:
      return 'unknown';
  }
}

// API Response types
export type CheckResponse = APIResponse<DiscoveryResult>;
export type OperationsResponse = APIResponse<{ operations: UpdateOperation[]; count: number }>;
export type HistoryResponse = APIResponse<{ history: HistoryEntry[]; count: number }>;
export type BackupsResponse = APIResponse<{ backups: ComposeBackup[]; count: number }>;
export type HealthCheckResponse = APIResponse<HealthResponse>;
