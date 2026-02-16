import type {
  OperationInfo,
  UpdateOperation,
  MixedOperation,
} from '../types';
import type { ExecutorContext, OperationExecutor } from './types';
import { triggerBatchUpdate, fixComposeMismatch } from '../../api/client';
import { addLog } from './log';

export class BatchTrackedExecutor implements OperationExecutor {
  async execute(info: OperationInfo, ctx: ExecutorContext): Promise<void> {
    switch (info.type) {
      case 'update':
        return this.runUpdate(info, ctx);
      case 'mixed':
        return this.runMixed(info, ctx);
      default:
        throw new Error(`BatchTrackedExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runUpdate(info: UpdateOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;

    addLog(dispatch, runId, `Starting update of ${info.containers.length} container(s)`, 'info', 'fa-rocket');

    // Group by stack for logging
    const stackGroups = new Map<string, string[]>();
    for (const c of info.containers) {
      const stack = c.stack || '__standalone__';
      if (!stackGroups.has(stack)) {
        stackGroups.set(stack, []);
      }
      stackGroups.get(stack)!.push(c.name);
    }

    for (const [stack, containerNames] of stackGroups) {
      const stackName = stack === '__standalone__' ? 'Standalone' : stack;
      addLog(dispatch, runId, `${stackName}: ${containerNames.join(', ')}`, 'info', 'fa-layer-group');
    }

    try {
      const response = await triggerBatchUpdate(info.containers);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: 'Batch update failed', error: response.error } });
        addLog(dispatch, runId, `Batch update failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        dispatch({ type: 'SET_CAN_RETRY', runId, canRetry: true });
        return;
      }

      // Save batch group ID to URL for refresh recovery
      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      // Track operation IDs and mark container states
      const operations = response.data?.operations || [];

      for (const op of operations) {
        if (op.status === 'started' && op.operation_id) {
          for (const containerName of op.containers) {
            dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName, operationId: op.operation_id });
          }
          dispatch({
            type: 'CONTAINERS_WHERE_UPDATE',
            runId,
            predicate: c => op.containers.includes(c.name),
            updates: { operationId: op.operation_id },
          });
          addLog(dispatch, runId, `Operation started for ${op.containers.join(', ')}`, 'info', 'fa-play');
        } else if (op.status === 'failed') {
          for (const containerName of op.containers) {
            dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: 'Failed to start', error: op.error });
          }
          addLog(dispatch, runId, `Failed to start: ${op.error}`, 'error', 'fa-circle-xmark');
        }
      }

      // SSE + polling hooks handle the rest (progress tracking and completion)
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: 'Error', error: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runMixed(info: MixedOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { updates, mismatches } = info;
    const totalCount = updates.length + mismatches.length;

    addLog(dispatch, runId, `Processing ${totalCount} item(s): ${updates.length} update(s) and ${mismatches.length} mismatch fix(es)...`, 'info', 'fa-layer-group');

    let successCount = 0;
    let failedCount = 0;

    // First, run updates via batch update API
    if (updates.length > 0) {
      addLog(dispatch, runId, `Starting ${updates.length} update(s)...`, 'info', 'fa-download');

      try {
        const response = await triggerBatchUpdate(updates);

        if (!response.success) {
          throw new Error(response.error || 'Failed to start batch update');
        }

        // Track operations
        if (response.data?.operations) {
          for (const stackOp of response.data.operations) {
            if (stackOp.operation_id) {
              for (const containerName of stackOp.containers) {
                dispatch({ type: 'CONTAINER_UPDATE', runId, containerName, updates: { operationId: stackOp.operation_id } });
                dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName, operationId: stackOp.operation_id });
              }
            } else if (stackOp.error) {
              for (const containerName of stackOp.containers) {
                failedCount++;
                dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: stackOp.error || 'Failed to start', error: stackOp.error });
                addLog(dispatch, runId, `${containerName} failed to start: ${stackOp.error || 'Unknown error'}`, 'error', 'fa-circle-xmark');
              }
            }
          }
        }

        // Save batch group ID for recovery
        const batchGroupId = response.data?.batch_group_id;
        if (batchGroupId) {
          dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
          setSearchParams({ group: batchGroupId }, { replace: true });
        }

        // Poll for update completions — updates must finish before mismatches start
        const pendingUpdates = new Set(updates.map(u => u.name));

        let pollCount = 0;
        const maxPolls = 300;

        while (pendingUpdates.size > 0 && pollCount < maxPolls) {
          await new Promise(resolve => setTimeout(resolve, 2000));
          pollCount++;

          for (const name of [...pendingUpdates]) {
            try {
              // We need to look up the operation ID from the response data
              let opId: string | undefined;
              for (const stackOp of (response.data?.operations || [])) {
                if (stackOp.operation_id && stackOp.containers.includes(name)) {
                  opId = stackOp.operation_id;
                  break;
                }
              }
              if (!opId) {
                pendingUpdates.delete(name);
                continue;
              }

              const opResponse = await fetch(`/api/operations/${opId}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;
                if (op.status === 'complete') {
                  pendingUpdates.delete(name);
                  successCount++;
                  dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName: name, message: 'Updated' });
                  addLog(dispatch, runId, `${name} updated successfully`, 'success', 'fa-circle-check');
                } else if (op.status === 'failed') {
                  pendingUpdates.delete(name);
                  failedCount++;
                  dispatch({ type: 'CONTAINER_FAILED', runId, containerName: name, message: op.error_message || 'Update failed', error: op.error_message });
                  addLog(dispatch, runId, `${name} update failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
                }
              }
            } catch {
              // Continue polling
            }
          }
        }

        // Handle timeouts
        for (const name of pendingUpdates) {
          failedCount++;
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: name, message: 'Timed out' });
          addLog(dispatch, runId, `${name} timed out`, 'error', 'fa-clock');
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Batch update failed';
        addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
        // Mark all pending updates as failed
        for (const u of updates) {
          failedCount++;
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: u.name, message: errorMsg, error: errorMsg });
        }
      }
    }

    // Then, run mismatch fixes sequentially (each must complete before next)
    if (mismatches.length > 0) {
      addLog(dispatch, runId, `Starting ${mismatches.length} mismatch fix(es)...`, 'info', 'fa-rotate');

      for (const containerName of mismatches) {
        dispatch({ type: 'CONTAINER_UPDATE', runId, containerName, updates: { status: 'in_progress', message: 'Syncing to compose file...' } });

        try {
          const response = await fixComposeMismatch(containerName);

          if (!response.success || !response.data?.operation_id) {
            throw new Error(response.error || 'Failed to start');
          }

          // Poll for completion (sequential — must wait for each)
          let completed = false;
          let pollCount = 0;
          const maxPolls = 120;

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 2000));
            pollCount++;

            try {
              const opResponse = await fetch(`/api/operations/${response.data.operation_id}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;
                if (op.status === 'complete') {
                  completed = true;
                  successCount++;
                  dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Fixed' });
                  addLog(dispatch, runId, `${containerName} fixed successfully`, 'success', 'fa-circle-check');
                } else if (op.status === 'failed') {
                  completed = true;
                  failedCount++;
                  dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: op.error_message || 'Fix failed', error: op.error_message });
                  addLog(dispatch, runId, `${containerName} fix failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
                }
              }
            } catch {
              // Continue polling
            }
          }

          if (!completed) {
            failedCount++;
            dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: 'Timed out' });
            addLog(dispatch, runId, `${containerName} timed out`, 'error', 'fa-clock');
          }
        } catch (err) {
          failedCount++;
          const errorMsg = err instanceof Error ? err.message : 'Unknown error';
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
          addLog(dispatch, runId, `${containerName} fix failed: ${errorMsg}`, 'error', 'fa-circle-xmark');
        }
      }
    }

    // Final status
    dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
    if (failedCount > 0) {
      if (successCount > 0) {
        addLog(dispatch, runId, `Completed with errors: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
      } else {
        addLog(dispatch, runId, `All ${failedCount} operation(s) failed`, 'error', 'fa-circle-xmark');
      }
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    } else {
      addLog(dispatch, runId, `All ${successCount} operation(s) completed successfully`, 'success', 'fa-circle-check');
      dispatch({ type: 'SET_STATUS', runId, status: 'success' });
    }
  }
}
