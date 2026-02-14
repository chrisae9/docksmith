import type {
  OperationInfo,
  OperationAction,
  BatchStartOperation,
  BatchStopOperation,
  BatchRestartOperation,
  BatchRemoveOperation,
  BatchLabelOperation,
  LabelRollbackOperation,
} from '../types';
import type { LogEntry } from '../../constants/progress';
import type { ExecutorContext, OperationExecutor } from './types';
import { describeLabelOp } from '../utils';
import {
  batchStartContainers,
  batchStopContainers,
  batchRestartContainers,
  batchRemoveContainers,
  batchSetLabels,
  rollbackLabels,
} from '../../api/client';

function addLog(dispatch: React.Dispatch<OperationAction>, runId: string, message: string, type: LogEntry['type'] = 'info', icon?: string) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message, type, icon } });
}

function processBatchResults(
  dispatch: React.Dispatch<OperationAction>,
  runId: string,
  results: Array<{ container: string; success: boolean; operation_id?: string; error?: string }>,
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
        return this.runBatchStart(info, ctx);
      case 'batchStop':
        return this.runBatchStop(info, ctx);
      case 'batchRestart':
        return this.runBatchRestart(info, ctx);
      case 'batchRemove':
        return this.runBatchRemove(info, ctx);
      case 'batchLabel':
        return this.runBatchLabel(info, ctx);
      case 'labelRollback':
        return this.runLabelRollback(info, ctx);
      default:
        throw new Error(`BatchImmediateExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runBatchStart(info: BatchStartOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containers: containerNames } = info;

    addLog(dispatch, runId, `Starting ${containerNames.length} container(s)`, 'info', 'fa-play');

    try {
      const response = await batchStartContainers(containerNames);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Batch start failed' } });
        addLog(dispatch, runId, `Batch start failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      processBatchResults(dispatch, runId, response.data?.results || [], 'Started successfully', 'start');
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runBatchStop(info: BatchStopOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containers: containerNames } = info;

    addLog(dispatch, runId, `Stopping ${containerNames.length} container(s)`, 'info', 'fa-stop');

    try {
      const response = await batchStopContainers(containerNames);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Batch stop failed' } });
        addLog(dispatch, runId, `Batch stop failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      processBatchResults(dispatch, runId, response.data?.results || [], 'Stopped successfully', 'stop');
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runBatchRestart(info: BatchRestartOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containers: containerNames } = info;

    addLog(dispatch, runId, `Restarting ${containerNames.length} container(s)`, 'info', 'fa-rotate');

    try {
      const response = await batchRestartContainers(containerNames);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Batch restart failed' } });
        addLog(dispatch, runId, `Batch restart failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      processBatchResults(dispatch, runId, response.data?.results || [], 'Restarted successfully', 'restart');
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: errorMsg } });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
    }
  }

  private async runBatchRemove(info: BatchRemoveOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containers: containerNames } = info;

    addLog(dispatch, runId, `Removing ${containerNames.length} container(s)`, 'info', 'fa-trash');

    try {
      const response = await batchRemoveContainers(containerNames, true);

      if (!response.success) {
        dispatch({ type: 'CONTAINERS_WHERE_UPDATE', runId, predicate: () => true, updates: { status: 'failed', message: response.error || 'Batch remove failed' } });
        addLog(dispatch, runId, `Batch remove failed: ${response.error}`, 'error', 'fa-circle-xmark');
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        dispatch({ type: 'SET_BATCH_GROUP_ID', runId, batchGroupId });
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      processBatchResults(dispatch, runId, response.data?.results || [], 'Removed successfully', 'remove');
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
