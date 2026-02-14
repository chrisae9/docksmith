import type { LogEntry } from '../constants/progress';

// Operation type union (all 17 types)
export type OperationType = 'restart' | 'update' | 'rollback' | 'start' | 'stop' | 'remove' | 'stackRestart' | 'stackStop' | 'fixMismatch' | 'batchFixMismatch' | 'mixed' | 'batchLabel' | 'batchStart' | 'batchStop' | 'batchRestart' | 'batchRemove' | 'labelRollback';

// Restart operation info (includes save settings)
export interface RestartOperation {
  type: 'restart';
  containerName: string;
  force?: boolean;
  saveSettings?: boolean;
  labelChanges?: {
    ignore?: boolean;
    allow_latest?: boolean;
    version_pin_major?: boolean;
    version_pin_minor?: boolean;
    version_pin_patch?: boolean;
    tag_regex?: string;
    script?: string;
    restart_after?: string;
  };
}

// Update operation info
export interface UpdateOperation {
  type: 'update';
  containers: Array<{
    name: string;
    target_version: string;
    stack: string;
    force?: boolean;
    change_type?: number;
    old_resolved_version?: string;
    new_resolved_version?: string;
  }>;
}

// Rollback operation info
export interface RollbackOperation {
  type: 'rollback';
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
  force?: boolean;
}

// Start operation info
export interface StartOperation {
  type: 'start';
  containerName: string;
}

// Stop operation info
export interface StopOperation {
  type: 'stop';
  containerName: string;
}

// Remove operation info
export interface RemoveOperation {
  type: 'remove';
  containerName: string;
  force?: boolean;
}

// Stack restart operation info (restart multiple containers in a stack)
export interface StackRestartOperation {
  type: 'stackRestart';
  stackName: string;
  containers: string[];
  force?: boolean;
}

// Stack stop operation info (stop multiple containers in a stack)
export interface StackStopOperation {
  type: 'stackStop';
  stackName: string;
  containers: string[];
}

// Fix mismatch operation info (sync container to compose file)
export interface FixMismatchOperation {
  type: 'fixMismatch';
  containerName: string;
}

// Batch fix mismatch operation info (sync multiple containers to compose files)
export interface BatchFixMismatchOperation {
  type: 'batchFixMismatch';
  containerNames: string[];
}

// Mixed operation info (both updates and mismatches selected together)
export interface MixedOperation {
  type: 'mixed';
  updates: Array<{
    name: string;
    target_version: string;
    stack: string;
    force?: boolean;
  }>;
  mismatches: string[];
}

// Batch label operation info (apply label changes to multiple containers)
export interface BatchLabelOperation {
  type: 'batchLabel';
  containers: string[];
  labelOp: {
    ignore?: boolean;
    allow_latest?: boolean;
    version_pin_major?: boolean;
    version_pin_minor?: boolean;
    version_pin_patch?: boolean;
    tag_regex?: string;
    script?: string;
  };
}

// Batch start operation info (start multiple containers via batch endpoint)
export interface BatchStartOperation {
  type: 'batchStart';
  containers: string[];
}

// Batch stop operation info (stop multiple containers via batch endpoint)
export interface BatchStopOperation {
  type: 'batchStop';
  containers: string[];
}

// Batch restart operation info (restart multiple containers via batch endpoint)
export interface BatchRestartOperation {
  type: 'batchRestart';
  containers: string[];
}

// Batch remove operation info (remove multiple containers via batch endpoint)
export interface BatchRemoveOperation {
  type: 'batchRemove';
  containers: string[];
}

// Label rollback operation info (reverse label changes from a previous batch)
export interface LabelRollbackOperation {
  type: 'labelRollback';
  batchGroupId: string;
  containers: string[];
  containerNames?: string[]; // Optional: rollback specific containers only
}

// Discriminated union of all operation info types
export type OperationInfo = RestartOperation | UpdateOperation | RollbackOperation | StartOperation | StopOperation | RemoveOperation | StackRestartOperation | StackStopOperation | FixMismatchOperation | BatchFixMismatchOperation | MixedOperation | BatchLabelOperation | BatchStartOperation | BatchStopOperation | BatchRestartOperation | BatchRemoveOperation | LabelRollbackOperation;

// Container state for the reducer
export interface ContainerState {
  name: string;
  status: 'pending' | 'in_progress' | 'success' | 'failed';
  stage?: string;
  percent: number;
  message?: string;
  error?: string;
  operationId?: string;
  badge?: string;
  versionFrom?: string;
  versionTo?: string;
}

// Operation phase (derived from container states)
export type OperationPhase = 'idle' | 'running' | 'completed' | 'failed' | 'partial';

// Error classification
export type ErrorKind = 'transport' | 'timeout' | 'backend' | 'precheck' | 'recovery';
export interface OperationError {
  kind: ErrorKind;
  raw: string;
  message: string;
  retryable: boolean;
  forceable: boolean;
}

// Full reducer state
export interface OperationState {
  runId: string;
  phase: OperationPhase;
  operationType: OperationType | null;
  operationInfo: OperationInfo | null;
  containers: ContainerState[];
  logs: LogEntry[];
  operationId: string | null;
  batchGroupId: string | null;
  startTime: number | null;
  endTime: number | null;
  currentStage: string | null;
  currentPercent: number;
  canForceRetry: boolean;
  forceRetryMessage: string;
  canRetry: boolean;
  recoveredOperation: any | null;
  expectedDependents: string[];
  dependentsRestarted: string[];
  dependentsBlocked: string[];
  containerToOpId: Map<string, string>;
  processedEvents: Set<string>;
  maxPercent: Map<string, number>;
  lastLoggedStage: Map<string, string>;
  sawPendingRestart: boolean;
}

// All reducer action types
export type OperationAction =
  | { type: 'INIT'; runId: string; operationType: OperationType; operationInfo: OperationInfo; containers: ContainerState[] }
  | { type: 'CONTAINER_UPDATE'; runId: string; containerName: string; updates: Partial<ContainerState> }
  | { type: 'CONTAINER_COMPLETED'; runId: string; containerName: string; message?: string }
  | { type: 'CONTAINER_FAILED'; runId: string; containerName: string; message: string; error?: string }
  | { type: 'CONTAINERS_WHERE_UPDATE'; runId: string; predicate: (c: ContainerState) => boolean; updates: Partial<ContainerState> | ((c: ContainerState) => Partial<ContainerState>) }
  | { type: 'SET_OPERATION_ID'; runId: string; operationId: string }
  | { type: 'SET_BATCH_GROUP_ID'; runId: string; batchGroupId: string }
  | { type: 'SET_STATUS'; runId: string; status: 'in_progress' | 'success' | 'failed' }
  | { type: 'SET_END_TIME'; runId: string; endTime: number }
  | { type: 'ADD_LOG'; runId: string; entry: LogEntry }
  | { type: 'SET_FORCE_RETRY'; runId: string; canForceRetry: boolean; message: string }
  | { type: 'SET_CAN_RETRY'; runId: string; canRetry: boolean }
  | { type: 'SET_STAGE'; runId: string; stage: string; percent: number }
  | { type: 'SET_DEPENDENTS'; runId: string; expected?: string[]; restarted?: string[]; blocked?: string[] }
  | { type: 'SET_CONTAINER_OP_ID'; runId: string; containerName: string; operationId: string }
  | { type: 'SET_SAW_PENDING_RESTART'; runId: string; value: boolean }
  | { type: 'RECOVERY_LOADED'; runId: string; state: Partial<OperationState> }
  | { type: 'SSE_EVENT'; runId: string; event: any; containerName: string | null }
  | { type: 'POLL_UPDATE'; runId: string; containerName: string; status: 'success' | 'failed' | 'in_progress'; message?: string; error?: string }
  | { type: 'RETRY'; newRunId: string };
