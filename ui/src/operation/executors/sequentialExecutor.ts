import type {
  OperationInfo,
  StackStopOperation,
  BatchFixMismatchOperation,
} from '../types';
import type { ExecutorContext, OperationExecutor } from './types';
import { stopContainer, fixComposeMismatch } from '../../api/client';
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

    addLog(dispatch, runId, `Stopping ${containerNames.length} container(s) in stack "${stackName}"`, 'info', 'fa-layer-group');

    let successCount = 0;
    let failedCount = 0;

    // Stop each container sequentially
    for (const containerName of containerNames) {
      dispatch({ type: 'CONTAINER_UPDATE', runId, containerName, updates: { status: 'in_progress', message: 'Stopping...' } });
      addLog(dispatch, runId, `Stopping ${containerName}...`, 'stage', 'fa-stop');

      try {
        const response = await stopContainer(containerName);

        if (response.success) {
          const opId = response.data?.operation_id;
          if (opId) {
            dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName, operationId: opId });
            setSearchParams({ id: opId }, { replace: true });
          }
          dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Stopped successfully' });
          addLog(dispatch, runId, `${containerName}: Stopped successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          const errorMsg = response.error || 'Failed to stop container';
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
          addLog(dispatch, runId, `${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
        addLog(dispatch, runId, `${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
        failedCount++;
      }
    }

    // Final summary
    if (failedCount > 0) {
      if (successCount > 0) {
        addLog(dispatch, runId, `Stack stop completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        dispatch({ type: 'SET_STATUS', runId, status: 'success' }); // Partial success
      } else {
        addLog(dispatch, runId, `Stack stop failed: all ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      }
    } else {
      addLog(dispatch, runId, `Stack stop completed: ${successCount} container(s) stopped successfully`, 'success', 'fa-circle-check');
      dispatch({ type: 'SET_STATUS', runId, status: 'success' });
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
