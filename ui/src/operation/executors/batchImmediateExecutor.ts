import type {
  OperationInfo,
  OperationAction,
  BatchLabelOperation,
  LabelRollbackOperation,
} from '../types';
import type { ExecutorContext, OperationExecutor } from './types';
import type { APIResponse } from '../../types/api';
import { describeLabelOp } from '../utils';
import {
  batchStartContainers,
  batchStopContainers,
  batchRestartContainers,
  batchRemoveContainers,
  batchSetLabels,
  rollbackLabels,
} from '../../api/client';
import { addLog } from './log';

type BatchResult = { container: string; success: boolean; operation_id?: string; error?: string };
type BatchResponse = APIResponse<{ batch_group_id?: string; results: BatchResult[] }>;

function processBatchResults(
  dispatch: React.Dispatch<OperationAction>,
  runId: string,
  results: BatchResult[],
  successMessage: string,
  verb: string,
) {
  let successCount = 0;
  let failedCount = 0;

  for (const result of results) {
    if (result.operation_id) {
      dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName: result.container, operationId: result.operation_id });
    }
    if (result.success) {
      dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName: result.container, message: successMessage });
      successCount++;
    } else {
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName: result.container, message: result.error || 'Failed', error: result.error });
      failedCount++;
    }
  }

  if (failedCount > 0 && successCount === 0) {
    addLog(dispatch, runId, `All ${failedCount} container(s) failed to ${verb}`, 'error', 'fa-circle-xmark');
    dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
  } else if (failedCount > 0) {
    addLog(dispatch, runId, `${verb.charAt(0).toUpperCase() + verb.slice(1)} completed: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
    dispatch({ type: 'SET_STATUS', runId, status: 'success' });
  } else {
    addLog(dispatch, runId, `All ${successCount} container(s) ${verb === 'remove' ? 'removed' : verb + 'ed'} successfully`, 'success', 'fa-circle-check');
    dispatch({ type: 'SET_STATUS', runId, status: 'success' });
  }
}

export class BatchImmediateExecutor implements OperationExecutor {
  async execute(info: OperationInfo, ctx: ExecutorContext): Promise<void> {
    switch (info.type) {
      case 'batchStart':
        return this.runBatchOp(ctx, info.containers, (n) => batchStartContainers(n), 'Starting', 'fa-play', 'Started successfully', 'start');
      case 'batchStop':
        return this.runBatchOp(ctx, info.containers, (n) => batchStopContainers(n), 'Stopping', 'fa-stop', 'Stopped successfully', 'stop');
      case 'batchRestart':
        return this.runBatchOp(ctx, info.containers, (n) => batchRestartContainers(n), 'Restarting', 'fa-rotate', 'Restarted successfully', 'restart');
      case 'batchRemove':
        return this.runBatchOp(ctx, info.containers, (n) => batchRemoveContainers(n, true), 'Removing', 'fa-trash', 'Removed successfully', 'remove');
      case 'batchLabel':
        return this.runBatchLabel(info, ctx);
      case 'labelRollback':
        return this.runLabelRollback(info, ctx);
      default:
        throw new Error(`BatchImmediateExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runBatchOp(
    ctx: ExecutorContext,
    containerNames: string[],
    apiFn: (names: string[]) => Promise<BatchResponse>,
    gerund: string,
    icon: string,
    successMessage: string,
    verb: string,
  ): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;

    addLog(dispatch, runId, `${gerund} ${containerNames.length} container(s)`, 'info', icon);

    try {
      const response = await apiFn(containerNames);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || `Batch ${verb} failed` } });
        addLog(dispatch, runId, `Batch ${verb} failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      processBatchResults(dispatch, runId, response.data?.results || [], successMessage, verb);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runBatchLabel(info: BatchLabelOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containers: containerNames, labelOp } = info;
    const opDescription = describeLabelOp(labelOp);

    addLog(dispatch, runId, `Applying "${opDescription}" to ${containerNames.length} container(s)`, 'info', 'fa-tags');

    try {
      const operations = containerNames.map(name => ({ container: name, ...labelOp }));
      const response = await batchSetLabels(operations);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Batch label operation failed' } });
        addLog(dispatch, runId, `Batch label operation failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      // Label operations return immediately — the actual label change + restart
      // runs asynchronously in a background goroutine. Track via SSE/poller.
      const results = response.data?.results || [];
      let startedCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          // Operation was started, not completed — leave as in_progress
          if (result.operation_id) {
            dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName: result.container, operationId: result.operation_id });
          }
          dispatch({ type: 'CONTAINER_UPDATE', runId, containerName: result.container, updates: { status: 'in_progress', message: `Applying ${opDescription}...` } });
          addLog(dispatch, runId, `${result.container}: ${opDescription} started`, 'info', 'fa-spinner');
          startedCount++;
        } else {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: result.container, message: result.error || 'Failed', error: result.error });
          addLog(dispatch, runId, `${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && startedCount === 0) {
        addLog(dispatch, runId, `All ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      } else if (startedCount > 0) {
        addLog(dispatch, runId, `${startedCount} operation(s) started, tracking progress...`, 'info', 'fa-spinner');
        // Don't set status — let poller/SSE handle completion
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runLabelRollback(info: LabelRollbackOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { batchGroupId, containers: containerNames, containerNames: specificContainers } = info;

    const targetDesc = specificContainers?.length
      ? `${specificContainers.length} container(s)`
      : `${containerNames.length} container(s)`;
    addLog(dispatch, runId, `Rolling back label changes for ${targetDesc}`, 'info', 'fa-rotate-left');

    try {
      const response = await rollbackLabels({
        batch_group_id: batchGroupId,
        container_names: specificContainers,
      });

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Label rollback failed' } });
        addLog(dispatch, runId, `Label rollback failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const rollbackGroupId = response.data?.batch_group_id;
      if (rollbackGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId: rollbackGroupId });
        setSearchParams({ group: rollbackGroupId }, { replace: true });
      }

      // Label rollback returns immediately — actual work runs async. Track via SSE/poller.
      const results = response.data?.results || [];
      let startedCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          // Operation was started, not completed — leave as in_progress
          if (result.operation_id) {
            dispatch({ type: 'SET_CONTAINER_OP_ID', runId, containerName: result.container, operationId: result.operation_id });
          }
          dispatch({ type: 'CONTAINER_UPDATE', runId, containerName: result.container, updates: { status: 'in_progress', message: 'Restoring labels...' } });
          addLog(dispatch, runId, `${result.container}: Label rollback started`, 'info', 'fa-spinner');
          startedCount++;
        } else {
          dispatch({ type: 'CONTAINER_FAILED', runId, containerName: result.container, message: result.error || 'Failed', error: result.error });
          addLog(dispatch, runId, `${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && startedCount === 0) {
        addLog(dispatch, runId, `All ${failedCount} container(s) failed to rollback`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      } else if (startedCount > 0) {
        addLog(dispatch, runId, `${startedCount} rollback(s) started, tracking progress...`, 'info', 'fa-spinner');
        // Don't set status — let poller/SSE handle completion
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }
}
