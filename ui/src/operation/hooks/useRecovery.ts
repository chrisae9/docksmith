import { useEffect, useRef } from 'react';
import { getOperationsByGroup } from '../../api/client';
import type { OperationState, OperationAction, ContainerState, OperationType } from '../types';

interface UseRecoveryOptions {
  urlOperationId: string | null;
  urlBatchGroupId: string | null;
  operationInfo: OperationState['operationInfo'];
  phase: OperationState['phase'];
  runId: string;
  dispatch: React.Dispatch<OperationAction>;
}

export function useRecovery({
  urlOperationId,
  urlBatchGroupId,
  operationInfo,
  phase,
  runId,
  dispatch,
}: UseRecoveryOptions) {
  const hasRun = useRef(false);

  useEffect(() => {
    // Only run in recovery mode: no operationInfo (page refresh), but has URL params
    if (operationInfo || (!urlOperationId && !urlBatchGroupId)) return;
    // Don't fire recovery if an operation is already active (executor cleared location.state
    // but the reducer already has the operation in progress)
    if (phase !== 'idle') return;
    if (hasRun.current) return;
    hasRun.current = true;

    const maxRetries = 2;

    const runWithRetry = async (attempt: number) => {
      try {
        if (urlBatchGroupId) {
          await recoverBatchGroup(urlBatchGroupId, runId, dispatch);
        } else if (urlOperationId) {
          await recoverSingleOperation(urlOperationId, runId, dispatch);
        }
      } catch (err) {
        // Network error — retry with backoff
        if (attempt < maxRetries) {
          await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
          return runWithRetry(attempt + 1);
        }
        // Exhausted retries — show error
        const errMsg = err instanceof Error ? err.message : 'Unknown error';
        dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Failed to recover after ${maxRetries + 1} attempts: ${errMsg}`, type: 'error', icon: 'fa-circle-xmark' } });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      }
    };

    runWithRetry(0);
  }, [urlOperationId, urlBatchGroupId, operationInfo, phase, runId, dispatch]);
}

async function recoverBatchGroup(
  groupId: string,
  runId: string,
  dispatch: React.Dispatch<OperationAction>,
) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Recovering batch group status...', type: 'info', icon: 'fa-sync' } });

  try {
    const response = await getOperationsByGroup(groupId);

    if (!response.success || !response.data) {
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Batch group not found', type: 'error', icon: 'fa-circle-xmark' } });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      return; // Non-retryable — op genuinely not found
    }

    const ops = response.data.operations;
    if (ops.length === 0) {
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'No operations found in batch group', type: 'error', icon: 'fa-circle-xmark' } });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      return;
    }

    // Find earliest start time
    const startTimes = ops.map(op => op.started_at ? new Date(op.started_at).getTime() : Date.now());
    const startTime = Math.min(...startTimes);

    // Build container list and containerToOpId map
    const containerList: ContainerState[] = [];
    const containerToOpId = new Map<string, string>();

    for (const op of ops) {
      if (op.batch_details && op.batch_details.length > 0) {
        for (const detail of op.batch_details) {
          // Use per-container status from batch_details when available
          const detailStatus = detail.status === 'complete' ? 'success' as const
            : detail.status === 'failed' ? 'failed' as const
            : op.status === 'complete' ? 'success' as const
            : op.status === 'failed' ? 'failed' as const
            : 'in_progress' as const;
          const detailMessage = detail.message
            || (detailStatus === 'success' ? 'Completed' : detailStatus === 'failed' ? (op.error_message || 'Failed') : 'In progress');
          containerList.push({
            name: detail.container_name,
            status: detailStatus,
            message: detailMessage,
            percent: detailStatus === 'success' ? 100 : detailStatus === 'failed' ? 100 : 50,
            versionFrom: detail.old_version,
            versionTo: detail.new_version,
            operationId: op.operation_id,
          });
          containerToOpId.set(detail.container_name, op.operation_id);
        }
      } else {
        containerList.push({
          name: op.container_name || 'Unknown',
          status: op.status === 'complete' ? 'success' : op.status === 'failed' ? 'failed' : 'in_progress',
          message: op.status === 'complete' ? 'Completed' : op.status === 'failed' ? (op.error_message || 'Failed') : 'In progress',
          percent: op.status === 'complete' ? 100 : 50,
          operationId: op.operation_id,
        });
        containerToOpId.set(op.container_name, op.operation_id);
      }
    }

    // Check overall status
    const allComplete = ops.every(op => op.status === 'complete');
    const anyFailed = ops.some(op => op.status === 'failed');
    const allDone = ops.every(op => op.status === 'complete' || op.status === 'failed');

    if (allComplete) {
      const completedTimes = ops.filter(op => op.completed_at).map(op => new Date(op.completed_at!).getTime());
      const endTime = completedTimes.length > 0 ? Math.max(...completedTimes) : Date.now();

      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'completed',
          containers: containerList,
          containerToOpId,
          batchGroupId: groupId,
          recoveredOperation: ops[0],
          startTime,
          endTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'All operations completed successfully', type: 'success', icon: 'fa-circle-check' } });
    } else if (allDone && anyFailed) {
      const completedTimes = ops.filter(op => op.completed_at).map(op => new Date(op.completed_at!).getTime());
      const endTime = completedTimes.length > 0 ? Math.max(...completedTimes) : Date.now();

      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'failed',
          containers: containerList,
          containerToOpId,
          batchGroupId: groupId,
          recoveredOperation: ops[0],
          startTime,
          endTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Some operations failed', type: 'error', icon: 'fa-circle-xmark' } });
    } else {
      // Still in progress — load state and let useOperationPoller handle the rest
      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'running',
          containers: containerList,
          containerToOpId,
          batchGroupId: groupId,
          recoveredOperation: ops[0],
          startTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Batch update in progress (${ops.length} operations)`, type: 'info', icon: 'fa-spinner' } });
    }
  } catch (err) {
    // Re-throw network errors so caller can retry
    if (err instanceof TypeError || (err instanceof Error && err.message.includes('fetch'))) {
      throw err;
    }
    const errMsg = err instanceof Error ? err.message : 'Unknown error';
    dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Failed to recover batch group: ${errMsg}`, type: 'error', icon: 'fa-circle-xmark' } });
    dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
  }
}

// Map backend operation_type to frontend OperationType.
function resolveOperationType(backendType: string): OperationType {
  const typeMap: Record<string, OperationType> = {
    single: 'update',
    batch: 'update',
    rollback: 'rollback',
    restart: 'restart',
    stop: 'stop',
    start: 'start',
    remove: 'remove',
    fix_mismatch: 'fixMismatch',
    label_change: 'batchLabel',
    batch_start: 'batchStart',
    batch_stop: 'batchStop',
    batch_restart: 'batchRestart',
    batch_remove: 'batchRemove',
  };
  return typeMap[backendType] || 'update';
}

async function recoverSingleOperation(
  opId: string,
  runId: string,
  dispatch: React.Dispatch<OperationAction>,
) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Recovering operation status...', type: 'info', icon: 'fa-sync' } });

  try {
    const response = await fetch(`/api/operations/${opId}`);
    if (!response.ok) {
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Failed to fetch operation: ${response.status}`, type: 'error', icon: 'fa-circle-xmark' } });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      return;
    }

    const data = await response.json();
    if (!data.success || !data.data) {
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Operation not found', type: 'error', icon: 'fa-circle-xmark' } });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      return;
    }

    const op = data.data;
    const startTime = op.started_at ? new Date(op.started_at).getTime() : Date.now();
    const operationType = resolveOperationType(op.operation_type);

    // Build container list — use batch_details if available, else single container
    const containers: ContainerState[] = [];
    const containerToOpId = new Map<string, string>();

    if (op.batch_details && op.batch_details.length > 0) {
      for (const detail of op.batch_details) {
        const detailStatus = detail.status === 'complete' ? 'success' as const
          : detail.status === 'failed' ? 'failed' as const
          : op.status === 'complete' ? 'success' as const
          : op.status === 'failed' ? 'failed' as const
          : 'in_progress' as const;
        containers.push({
          name: detail.container_name,
          status: detailStatus,
          message: detail.message || op.error_message || op.status,
          percent: detailStatus === 'success' || detailStatus === 'failed' ? 100 : 50,
          versionFrom: detail.old_version,
          versionTo: detail.new_version,
          operationId: opId,
        });
        containerToOpId.set(detail.container_name, opId);
      }
    } else {
      const containerName = op.container_name || 'Unknown';
      containers.push({
        name: containerName,
        status: op.status === 'complete' ? 'success' : op.status === 'failed' ? 'failed' : 'in_progress',
        message: op.error_message || op.status,
        percent: op.status === 'complete' ? 100 : 50,
      });
    }

    if (op.status === 'complete') {
      const endTime = op.completed_at ? new Date(op.completed_at).getTime() : Date.now();
      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'completed',
          containers,
          containerToOpId,
          operationType,
          operationId: opId,
          recoveredOperation: op,
          startTime,
          endTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Operation completed successfully', type: 'success', icon: 'fa-circle-check' } });
    } else if (op.status === 'failed') {
      const endTime = op.completed_at ? new Date(op.completed_at).getTime() : Date.now();
      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'failed',
          containers,
          containerToOpId,
          operationType,
          operationId: opId,
          recoveredOperation: op,
          startTime,
          endTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Operation failed', type: 'error', icon: 'fa-circle-xmark' } });
    } else if (op.status === 'pending_restart') {
      // Self-restart in progress — load as running + sawPendingRestart so useSelfRestartRecovery takes over
      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'running',
          containers,
          containerToOpId,
          operationType,
          operationId: opId,
          recoveredOperation: op,
          startTime,
          sawPendingRestart: true,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Self-restart in progress, waiting for completion...', type: 'info', icon: 'fa-rotate' } });
    } else {
      // in_progress — load and let SSE/poller handle the rest
      dispatch({
        type: 'RECOVERY_LOADED', runId, state: {
          phase: 'running',
          containers,
          containerToOpId,
          operationType,
          operationId: opId,
          recoveredOperation: op,
          startTime,
        },
      });
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Operation in progress: ${op.status}`, type: 'info', icon: 'fa-spinner' } });
    }
  } catch (err) {
    // Re-throw network errors so caller can retry
    if (err instanceof TypeError || (err instanceof Error && err.message.includes('fetch'))) {
      throw err;
    }
    const errMsg = err instanceof Error ? err.message : 'Unknown error';
    dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Failed to recover operation: ${errMsg}`, type: 'error', icon: 'fa-circle-xmark' } });
    dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
  }
}
