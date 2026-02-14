import type {
  OperationInfo,
  OperationAction,
  RestartOperation,
  RollbackOperation,
  FixMismatchOperation,
  StackRestartOperation,
} from '../types';
import type { LogEntry } from '../../constants/progress';
import type { ExecutorContext, OperationExecutor } from './types';
import { describeChanges, isPreCheckFailure } from '../utils';
import {
  startRestart,
  startStackRestart,
  setLabels as setLabelsAPI,
  fixComposeMismatch,
} from '../../api/client';

function addLog(dispatch: React.Dispatch<OperationAction>, runId: string, message: string, type: LogEntry['type'] = 'info', icon?: string) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message, type, icon } });
}

export class SSETrackedExecutor implements OperationExecutor {
  async execute(info: OperationInfo, ctx: ExecutorContext): Promise<void> {
    switch (info.type) {
      case 'restart':
        return this.runRestart(info, ctx);
      case 'rollback':
        return this.runRollback(info, ctx);
      case 'fixMismatch':
        return this.runFixMismatch(info, ctx);
      case 'stackRestart':
        return this.runStackRestart(info, ctx);
      default:
        throw new Error(`SSETrackedExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runRestart(info: RestartOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containerName, force, saveSettings, labelChanges } = info;

    try {
      // Save settings flow: API starts background operation and returns operation ID
      if (saveSettings && labelChanges) {
        dispatch({ type: 'SET_STAGE', runId, stage: 'saving', percent: 0 });
        const changes = describeChanges(labelChanges);
        if (changes.length > 0) {
          addLog(dispatch, runId, `Applying ${changes.length} change(s) to ${containerName}:`, 'stage', 'fa-floppy-disk');
          changes.forEach(change => {
            addLog(dispatch, runId, `  \u2022 ${change}`, 'info');
          });
        } else {
          addLog(dispatch, runId, `Saving settings for ${containerName}...`, 'stage', 'fa-floppy-disk');
        }

        const saveResponse = await setLabelsAPI(containerName, {
          ...labelChanges,
          force,
        });

        if (!saveResponse.success) {
          throw new Error(saveResponse.error || 'Failed to save settings');
        }

        const opId = saveResponse.data?.operation_id;
        if (opId) {
          dispatch({ type: 'SET_OPERATION_ID', runId, operationId: opId });
          setSearchParams({ id: opId }, { replace: true });
          addLog(dispatch, runId, `Operation started: ${opId.substring(0, 8)}...`, 'info', 'fa-play');
        }

        // SSE will handle progress updates from here
        return;
      }

      // Pure restart flow
      const action = force ? 'Force restarting' : 'Restarting';
      addLog(dispatch, runId, `${action} ${containerName}...`, 'stage', 'fa-rotate');
      dispatch({ type: 'SET_STAGE', runId, stage: 'stopping', percent: 0 });

      const response = await startRestart(containerName, force);

      if (!response.success || !response.data?.operation_id) {
        throw new Error(response.error || 'Failed to start restart operation');
      }

      dispatch({ type: 'SET_OPERATION_ID', runId, operationId: response.data.operation_id });
      setSearchParams({ id: response.data.operation_id }, { replace: true });
      addLog(dispatch, runId, `Operation started: ${response.data.operation_id.substring(0, 8)}...`, 'info', 'fa-play');

      // Fetch operation details to get expected dependents
      try {
        const opResponse = await fetch(`/api/operations/${response.data.operation_id}`);
        const opData = await opResponse.json();
        if (opData.success && opData.data?.dependents_affected) {
          dispatch({ type: 'SET_DEPENDENTS', runId, expected: opData.data.dependents_affected });
          if (opData.data.dependents_affected.length > 0) {
            addLog(dispatch, runId, `Found ${opData.data.dependents_affected.length} dependent container(s)`, 'info', 'fa-link');
          }
        }
      } catch {
        // Ignore errors fetching operation details
      }

      // SSE + polling hooks handle the rest
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Operation failed';
      const briefMessage = isPreCheckFailure(errorMessage) ? 'Pre-update check failed' : errorMessage;

      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: briefMessage, error: errorMessage });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, `Error: ${errorMessage}`, 'error', 'fa-circle-xmark');

      if (isPreCheckFailure(errorMessage)) {
        dispatch({ type: 'SET_FORCE_RETRY', runId, canForceRetry: true, message: 'You can force restart to bypass the pre-update check' });
        addLog(dispatch, runId, 'You can force the operation to bypass the pre-update check', 'info', 'fa-info-circle');
      }
    }
  }

  private async runRollback(info: RollbackOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { operationId: rollbackOpId, containerName, oldVersion, newVersion, force } = info;

    addLog(dispatch, runId, `Starting rollback of ${containerName}...`, 'info', 'fa-rotate-left');
    if (newVersion && oldVersion) {
      addLog(dispatch, runId, `Rolling back from ${newVersion} to ${oldVersion}`, 'info', 'fa-code-compare');
    }

    try {
      const response = await fetch('/api/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          operation_id: rollbackOpId,
          force: force || false,
        }),
      });
      const data = await response.json();

      if (data.success) {
        const newOpId = data.data?.operation_id;
        if (newOpId) {
          dispatch({ type: 'SET_OPERATION_ID', runId, operationId: newOpId });
          setSearchParams({ id: newOpId }, { replace: true });
        }
        addLog(dispatch, runId, 'Rollback operation started', 'info', 'fa-play');

        // SSE + polling hooks handle the rest
      } else {
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: data.error || 'Failed to trigger rollback', error: data.error });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        addLog(dispatch, runId, data.error || 'Failed to trigger rollback', 'error', 'fa-circle-xmark');

        if (data.error && isPreCheckFailure(data.error)) {
          dispatch({ type: 'SET_FORCE_RETRY', runId, canForceRetry: true, message: 'You can force rollback to bypass the pre-update check' });
          addLog(dispatch, runId, 'You can force rollback to bypass the pre-update check', 'info', 'fa-info-circle');
        }
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }

  private async runFixMismatch(info: FixMismatchOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { containerName } = info;

    addLog(dispatch, runId, `Fixing compose mismatch for ${containerName}...`, 'info', 'fa-rotate');

    try {
      const response = await fixComposeMismatch(containerName);

      if (!response.success || !response.data?.operation_id) {
        throw new Error(response.error || 'Failed to start fix mismatch operation');
      }

      dispatch({ type: 'SET_OPERATION_ID', runId, operationId: response.data.operation_id });
      setSearchParams({ id: response.data.operation_id }, { replace: true });
      addLog(dispatch, runId, `Operation started: ${response.data.operation_id.substring(0, 8)}...`, 'info', 'fa-play');

      // SSE + polling hooks handle the rest
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to fix compose mismatch';
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }

  private async runStackRestart(info: StackRestartOperation, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;
    const { stackName, containers: containerNames, force } = info;

    addLog(dispatch, runId, `Restarting ${containerNames.length} container(s) in stack "${stackName}"`, 'info', 'fa-layer-group');
    if (force) {
      addLog(dispatch, runId, 'Force mode enabled - pre-update checks will be skipped', 'info', 'fa-forward');
    }

    try {
      const response = await startStackRestart(stackName, containerNames, force);

      if (!response.success || !response.data?.operation_id) {
        const errorMsg = response.error || 'Failed to start stack restart';
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');

        if (isPreCheckFailure(errorMsg)) {
          dispatch({ type: 'SET_FORCE_RETRY', runId, canForceRetry: true, message: 'You can force restart to bypass pre-update checks' });
        }
        return;
      }

      const opId = response.data.operation_id;
      dispatch({ type: 'SET_OPERATION_ID', runId, operationId: opId });
      setSearchParams({ id: opId }, { replace: true });
      addLog(dispatch, runId, 'Stack restart operation started', 'info', 'fa-play');

      // SSE + polling hooks handle the rest
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }
}
