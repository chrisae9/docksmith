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
  latest_resolved_version?: string;
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
  compose_image?: string; // Image specified in compose file (for COMPOSE_MISMATCH)
  env_controlled?: boolean; // True if image is controlled by .env variable
  env_var_name?: string; // Name of the controlling env var (e.g., "OPENCLAW_IMAGE")
  note?: string; // Informational note (e.g., ghost tag warning)
  id: string;
  stack?: string;
  service?: string;
  dependencies?: string[];
  labels?: Record<string, string>;
  compose_labels?: Record<string, string>; // Docksmith labels from compose file
  labels_out_of_sync?: boolean; // True if compose labels differ from running container
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
  // Status endpoint specific fields
  last_cache_refresh?: string; // ISO timestamp of when cache was last cleared (cache refresh)
  last_background_run?: string; // ISO timestamp of when background check last ran
  checking?: boolean;
  next_check?: string; // ISO timestamp
  check_interval?: string; // Duration string like "5m0s"
  cache_ttl?: string; // Duration string like "1h0m0s"
}

// Batch Container Detail (matches storage.BatchContainerDetail)
export interface BatchContainerDetail {
  container_name: string;
  stack_name?: string;
  old_version: string;
  new_version: string;
  change_type?: number;
  old_resolved_version?: string;
  new_resolved_version?: string;
  old_digest?: string;
  status?: string;   // Per-container status: pending, restarting, complete, failed
  message?: string;  // Human-readable status message
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
  batch_details?: BatchContainerDetail[];
  batch_group_id?: string;
  created_at: string;
  updated_at: string;
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

// Docker Registry Info
export interface DockerRegistryInfo {
  registries: string[];
  config_path: string;
  host_config_path?: string;
  running_in_docker: boolean;
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
  NoChange: 0,
  PatchChange: 1,
  MinorChange: 2,
  MajorChange: 3,
  Downgrade: 4,
  UnknownChange: 5,
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

// Set Labels Request (matches api.SetLabelsRequest)
export interface SetLabelsRequest {
  container: string;
  ignore?: boolean;
  allow_latest?: boolean;
  version_pin_major?: boolean;
  version_pin_minor?: boolean;
  version_pin_patch?: boolean;
  tag_regex?: string;
  version_min?: string;
  version_max?: string;
  script?: string;
  restart_after?: string;
  no_restart?: boolean;
  force?: boolean;
}

// Label Operation Result (matches api.LabelOperationResult)
export interface LabelOperationResult {
  success: boolean;
  container: string;
  operation: string;
  operation_id?: string;
  labels_modified?: Record<string, string>;
  labels_removed?: string[];
  compose_file: string;
  restarted: boolean;
  pre_check_ran: boolean;
  pre_check_passed?: boolean;
  message?: string;
}

// Registry Tags Response
export interface RegistryTagsResponse {
  image_ref: string;
  tags: string[];
  count: number;
}

// Explorer Types

// Container item for explorer view (simplified from ContainerInfo)
export interface ContainerExplorerItem {
  id: string;
  name: string;
  image: string;
  state: string;
  health_status: string;
  stack?: string;
  created: number;
  networks: string[]; // Network names this container is connected to
}

// Docker image info for explorer
export interface ImageInfo {
  id: string;
  tags: string[];
  size: number;
  created: number;
  in_use: boolean;
  dangling: boolean;
}

// Docker network info for explorer
export interface NetworkInfo {
  id: string;
  name: string;
  driver: string;
  scope: string;
  containers: string[];
  is_default: boolean;
  created: number; // Unix timestamp when network was created
}

// Docker volume info for explorer
export interface VolumeInfo {
  name: string;
  driver: string;
  mount_point: string;
  containers: string[];
  size: number; // -1 if unknown
  created: number; // Unix timestamp when volume was created
}

// Combined explorer data with stack grouping
export interface ExplorerData {
  container_stacks: Record<string, ContainerExplorerItem[]>;
  standalone_containers: ContainerExplorerItem[];
  images: ImageInfo[];
  networks: NetworkInfo[];
  volumes: VolumeInfo[];
}

// Container inspection result
export interface ContainerInspect {
  id: string;
  name: string;
  image: string;
  created: string;
  state: ContainerState;
  config: ContainerConfig;
  network_settings: ContainerNetworkSettings;
  mounts: ContainerMount[];
  host_config: ContainerHostConfig;
  labels: Record<string, string>;
}

export interface ContainerState {
  status: string;
  running: boolean;
  paused: boolean;
  restarting: boolean;
  oom_killed: boolean;
  dead: boolean;
  pid: number;
  exit_code: number;
  error: string;
  started_at: string;
  finished_at: string;
  health?: string;
}

export interface ContainerConfig {
  hostname: string;
  user: string;
  env: string[];
  cmd: string[];
  entrypoint: string[];
  working_dir: string;
  exposed_ports: Record<string, boolean>;
  labels: Record<string, string>;
}

export interface ContainerNetworkSettings {
  ip_address: string;
  gateway: string;
  mac_address: string;
  ports: Record<string, PortBinding[]>;
  networks: Record<string, ContainerNetwork>;
}

export interface PortBinding {
  host_ip: string;
  host_port: string;
}

export interface ContainerNetwork {
  network_id: string;
  endpoint_id: string;
  gateway: string;
  ip_address: string;
  mac_address: string;
  aliases: string[];
}

export interface ContainerMount {
  type: string;
  source: string;
  destination: string;
  mode: string;
  rw: boolean;
}

export interface ContainerHostConfig {
  binds: string[];
  network_mode: string;
  restart_policy: RestartPolicy;
  port_bindings: Record<string, PortBinding[]>;
  memory: number;
  memory_swap: number;
  cpu_shares: number;
  cpu_period: number;
  cpu_quota: number;
  privileged: boolean;
  readonly_rootfs: boolean;
}

export interface RestartPolicy {
  name: string;
  maximum_retry_count: number;
}

// Container logs response
export interface ContainerLogsResponse {
  container: string;
  logs: string;
  tail: string;
}

// Container operation response
export interface ContainerOperationResponse {
  container: string;
  status: string;
  message: string;
  force?: boolean;
  volumes?: boolean;
}

// Unified container item — merges ContainerExplorerItem (live Docker state)
// with ContainerInfo (update checker cache) for the unified Containers tab
export interface UnifiedContainerItem {
  // From ContainerExplorerItem (live Docker state)
  id: string;
  name: string;
  image: string;
  state: string;          // running, paused, exited, dead, created
  health_status: string;
  stack?: string;
  created: number;
  networks: string[];

  // From ContainerInfo (update checker cache) — all optional
  container_name?: string;
  current_tag?: string;
  current_version?: string;
  latest_version?: string;
  latest_resolved_version?: string;
  change_type?: number;
  update_status?: string;
  recommended_tag?: string;
  using_latest_tag?: boolean;
  env_controlled?: boolean;
  env_var_name?: string;
  compose_image?: string;
  pre_update_check_pass?: boolean;
  pre_update_check_fail?: string;
  is_local?: boolean;
  error?: string;
  labels?: Record<string, string>;
  compose_labels?: Record<string, string>;
  labels_out_of_sync?: boolean;
  dependencies?: string[];
  service?: string;
  note?: string;

  has_update_data: boolean;  // true if matched in /api/status
}

// API Response types
export type CheckResponse = APIResponse<DiscoveryResult>;
export type OperationsResponse = APIResponse<{ operations: UpdateOperation[]; count: number }>;
export type HistoryResponse = APIResponse<{ history: HistoryEntry[]; count: number }>;
export type HealthCheckResponse = APIResponse<HealthResponse>;
export type DockerConfigResponse = APIResponse<DockerRegistryInfo>;
export type SetLabelsResponse = APIResponse<LabelOperationResult>;
export type RegistryTagsAPIResponse = APIResponse<RegistryTagsResponse>;
export type ExplorerResponse = APIResponse<ExplorerData>;
