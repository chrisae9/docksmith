import type {
  OperationInfo,
  StackStopOperation,
  BatchFixMismatchOperation,
} from '../types';
import type { ExecutorContext, OperationExecutor } from './types';
import { batchStopContainers, batchRemoveContainers, fixComposeMismatch } from '../../api/client';
import { addLog } from './log';

export class SequentialExecutor implements OperationExecutor {
  async execute(info: OperationInfo, ctx: ExecutorContext): Promise<void> {
    switch (info.type) {
      case 'stackStop':
        return this.runStackStop(info, ctx);
      case 'batchFixMismatch':
        return this.runBatchFixMismatch(info, ctx);
      default:
        throw new Error(`SequentialExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runStackStop(info: StackStopOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { stackName, containers: containerNames } = info;

    addLog(dispatch, runId, `Stopping and removing ${containerNames.length} container(s) in stack "${stackName}"`, 'info', 'fa-layer-group');

    try {
      // Populate containerToOpId with sentinels so the SSE hook's name-fallback
      // (which fires when containerToOpId is empty) doesn't pick up stop events
      // and prematurely mark containers as complete.
      for (const name of containerNames) {
        dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName: name, operationId: `pending-${runId}` });
      }

      // Step 1: Batch stop all containers
      addLog(dispatch, runId, 'Stopping containers...', 'stage', 'fa-stop');
      for (const name of containerNames) {
        dispatch({ type: 'CONTAINER_UPDATE', runId, containerName: name, updates: { status: 'in_progress', message: 'Stopping...' } });
      }

      const stopResponse = await batchStopContainers(containerNames);

      if (!stopResponse.success) {
        const errorMsg = stopResponse.error || 'Batch stop failed';
        for (const name of containerNames) {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: name, message: errorMsg, error: errorMsg });
        }
        addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      // Check stop results — track which containers stopped successfully
      const stopResults = stopResponse.data?.results || [];
      const stoppedNames: string[] = [];
      let stopFailed = 0;
      for (const result of stopResults) {
        if (result.success) {
          stoppedNames.push(result.container);
          addLog(dispatch, runId, `${result.container}: Stopped`, 'info', 'fa-circle-check');
        } else {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: result.container, message: result.error || 'Failed to stop', error: result.error });
          addLog(dispatch, runId, `${result.container}: ${result.error || 'Failed to stop'}`, 'error', 'fa-circle-xmark');
          stopFailed++;
        }
      }

      if (stoppedNames.length === 0) {
        addLog(dispatch, runId, 'All containers failed to stop', 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      // Step 2: Batch remove stopped containers
      addLog(dispatch, runId, 'Removing containers...', 'stage', 'fa-trash');
      for (const name of stoppedNames) {
        dispatch({ type: 'CONTAINER_UPDATE', runId, containerName: name, updates: { status: 'in_progress', message: 'Removing...' } });
      }

      const removeResponse = await batchRemoveContainers(stoppedNames, true);

      if (!removeResponse.success) {
        const errorMsg = removeResponse.error || 'Batch remove failed';
        for (const name of stoppedNames) {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: name, message: errorMsg, error: errorMsg });
        }
        addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      // Use the remove batch_group_id for history grouping
      const batchGroupId = removeResponse.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      // Process remove results
      const removeResults = removeResponse.data?.results || [];
      let removeSuccess = 0;
      let removeFailed = 0;
      for (const result of removeResults) {
        if (result.operation_id) {
          dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName: result.container, operationId: result.operation_id });
        }
        if (result.success) {
          dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName: result.container, message: 'Removed' });
          removeSuccess++;
        } else {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: result.container, message: result.error || 'Failed to remove', error: result.error });
          addLog(dispatch, runId, `${result.container}: ${result.error || 'Failed to remove'}`, 'error', 'fa-circle-xmark');
          removeFailed++;
        }
      }

      const totalFailed = stopFailed + removeFailed;
      if (totalFailed > 0) {
        addLog(dispatch, runId, `Stack down completed with issues: ${removeSuccess} removed, ${totalFailed} failed`, 'warning', 'fa-triangle-exclamation');
        dispatch({ type: 'SET_STATUS', runId, status: removeSuccess > 0 ? 'success' : 'failed' });
      } else {
        addLog(dispatch, runId, `Stack down completed: ${removeSuccess} container(s) removed`, 'success', 'fa-circle-check');
        dispatch({ type: 'SET_STATUS', runId, status: 'success' });
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      for (const name of containerNames) {
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName: name, message: errorMsg, error: errorMsg });
      }
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runBatchFixMismatch(info: BatchFixMismatchOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containerNames } = info;

    addLog(dispatch, runId, `Fixing ${containerNames.length} compose mismatch(es)...`, 'info', 'fa-rotate');

    let successCount = 0;
    let failedCount = 0;

    for (const containerName of containerNames) {
      dispatch({ type: 'CONTAINER_UPDATE', runId, containerName, updates: { status: 'in_progress', message: 'Syncing container to compose file...' } });
      addLog(dispatch, runId, `Fixing ${containerName}...`, 'info', 'fa-rotate');

      try {
        const response = await fixComposeMismatch(containerName);

        if (!response.success || !response.data?.operation_id) {
          throw new Error(response.error || 'Failed to start fix mismatch operation');
        }

        const opId = response.data.operation_id;
        dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName, operationId: opId });
        setSearchParams({ id: opId }, { replace: true });

        // Poll for completion (inline polling — sequential operations)
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
                dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Fixed successfully' });
                addLog(dispatch, runId, `${containerName} fixed successfully`, 'success', 'fa-circle-check');
              } else if (op.status === 'failed') {
                completed = true;
                failedCount++;
                dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: op.error_message || 'Unknown error', error: op.error_message });
                addLog(dispatch, runId, `${containerName} failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
              }
            }
          } catch {
            // Continue polling on error
          }
        }

        if (!completed) {
          failedCount++;
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: 'Timed out waiting for completion' });
          addLog(dispatch, runId, `${containerName} timed out`, 'error', 'fa-clock');
        }
      } catch (err) {
        failedCount++;
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
        addLog(dispatch, runId, `${containerName} failed: ${errorMsg}`, 'error', 'fa-circle-xmark');
      }
    }

    // Final status
    if (failedCount > 0) {
      if (successCount > 0) {
        addLog(dispatch, runId, `Completed with errors: ${successCount} fixed, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      } else {
        addLog(dispatch, runId, `All ${failedCount} fix(es) failed`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      }
    } else {
      addLog(dispatch, runId, `All ${successCount} compose mismatch(es) fixed successfully`, 'success', 'fa-circle-check');
      dispatch({ type: 'SET_STATUS', runId, status: 'success' });
    }
  }
}
