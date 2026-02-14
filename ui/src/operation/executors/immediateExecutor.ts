import type { OperationInfo, OperationAction, StartOperation, StopOperation, RemoveOperation } from '../types';
import type { LogEntry } from '../../constants/progress';
import type { ExecutorContext, OperationExecutor } from './types';
import { startContainer, stopContainer, removeContainer } from '../../api/client';

function addLog(dispatch: React.Dispatch<OperationAction>, runId: string, message: string, type: LogEntry['type'] = 'info', icon?: string) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message, type, icon } });
}

export class ImmediateExecutor implements OperationExecutor {
  async execute(info: OperationInfo, ctx: ExecutorContext): Promise<void> {
    const { dispatch, runId, setSearchParams } = ctx;

    switch (info.type) {
      case 'start':
        return this.runStart(info, dispatch, runId, setSearchParams);
      case 'stop':
        return this.runStop(info, dispatch, runId, setSearchParams);
      case 'remove':
        return this.runRemove(info, dispatch, runId, setSearchParams);
      default:
        throw new Error(`ImmediateExecutor does not handle operation type: ${info.type}`);
    }
  }

  private async runStart(info: StartOperation, dispatch: React.Dispatch<OperationAction>, runId: string, setSearchParams: ExecutorContext['setSearchParams']): Promise<void> {
    const { containerName } = info;

    addLog(dispatch, runId, `Starting ${containerName}...`, 'info', 'fa-play');

    try {
      const response = await startContainer(containerName);

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          dispatch({ type: 'SET_OPERATION_ID', runId, operationId: opId });
          setSearchParams({ id: opId }, { replace: true });
        }
        dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Container started successfully' });
        dispatch({ type: 'SET_STATUS', runId, status: 'success' });
        addLog(dispatch, runId, 'Container started successfully', 'success', 'fa-circle-check');
      } else {
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: response.error || 'Failed to start container', error: response.error });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        addLog(dispatch, runId, response.error || 'Failed to start container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to start container';
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }

  private async runStop(info: StopOperation, dispatch: React.Dispatch<OperationAction>, runId: string, setSearchParams: ExecutorContext['setSearchParams']): Promise<void> {
    const { containerName } = info;

    addLog(dispatch, runId, `Stopping ${containerName}...`, 'info', 'fa-stop');

    try {
      const response = await stopContainer(containerName);

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          dispatch({ type: 'SET_OPERATION_ID', runId, operationId: opId });
          setSearchParams({ id: opId }, { replace: true });
        }
        dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Container stopped successfully' });
        dispatch({ type: 'SET_STATUS', runId, status: 'success' });
        addLog(dispatch, runId, 'Container stopped successfully', 'success', 'fa-circle-check');
      } else {
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: response.error || 'Failed to stop container', error: response.error });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        addLog(dispatch, runId, response.error || 'Failed to stop container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to stop container';
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }

  private async runRemove(info: RemoveOperation, dispatch: React.Dispatch<OperationAction>, runId: string, setSearchParams: ExecutorContext['setSearchParams']): Promise<void> {
    const { containerName, force } = info;

    addLog(dispatch, runId, `Removing ${containerName}${force ? ' (force)' : ''}...`, 'info', 'fa-trash');

    try {
      const response = await removeContainer(containerName, { force });

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          dispatch({ type: 'SET_OPERATION_ID', runId, operationId: opId });
          setSearchParams({ id: opId }, { replace: true });
        }
        dispatch({ type: 'CONTAINER_COMPLETED', runId, containerName, message: 'Container removed successfully' });
        dispatch({ type: 'SET_STATUS', runId, status: 'success' });
        addLog(dispatch, runId, 'Container removed successfully', 'success', 'fa-circle-check');
      } else {
        dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: response.error || 'Failed to remove container', error: response.error });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        addLog(dispatch, runId, response.error || 'Failed to remove container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to remove container';
      dispatch({ type: 'CONTAINER_FAILED', runId, containerName, message: errorMsg, error: errorMsg });
      dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      addLog(dispatch, runId, errorMsg, 'error', 'fa-circle-xmark');
    }
  }
}
