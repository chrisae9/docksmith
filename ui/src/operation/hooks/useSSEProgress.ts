import { useEffect, useRef, type MutableRefObject } from 'react';
import type { UpdateProgressEvent } from '../../hooks/useEventStream';
import { STAGE_INFO, RESTART_STAGES } from '../../constants/progress';
import { isPreCheckFailure } from '../utils';
import type { OperationState, OperationAction, OperationType } from '../types';

const MULTI_CONTAINER_OPS: OperationType[] = [
  'update', 'batchStart', 'batchStop', 'batchRestart', 'batchRemove',
  'mixed', 'batchFixMismatch', 'batchLabel', 'labelRollback', 'stackRestart',
];

const SSE_COMPLETION_OPS: OperationType[] = [
  'restart', 'rollback', 'fixMismatch',
];

export function useSSEProgress(
  state: OperationState,
  dispatch: React.Dispatch<OperationAction>,
  eventQueue: MutableRefObject<UpdateProgressEvent[]>,
  eventSeq: number,
) {
  // Use refs to avoid stale closures in the effect while reading latest state
  const stateRef = useRef(state);
  stateRef.current = state;

  useEffect(() => {
    if (state.phase !== 'running') return;

    // Drain the queue — process ALL events that arrived since last render.
    // This is immune to React batching: even if 10 events arrive in one tick,
    // they all land in the ref queue and we process every one.
    const events = eventQueue.current;
    eventQueue.current = [];

    for (const event of events) {
      processEvent(stateRef.current, dispatch, event);
    }
  }, [eventSeq]); // eslint-disable-line react-hooks/exhaustive-deps
  // eventSeq triggers the effect; we read state from ref to avoid re-running on every state change
}

function processEvent(
  s: OperationState,
  dispatch: React.Dispatch<OperationAction>,
  event: UpdateProgressEvent,
) {
  // --- Dedup ---
  const eventKey = `${event.operation_id}-${event.container_name || ''}-${event.stage}-${event.progress || event.percent}`;
  if (s.processedEvents.has(eventKey)) return;

  // --- Ownership check ---
  let isOurOperation = false;
  let targetContainer: string | null = null;

  // Match by operationId
  if (s.operationId && event.operation_id === s.operationId) {
    isOurOperation = true;
    // For single-container ops, derive the target container from operationInfo
    if (s.operationInfo?.type === 'restart') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'rollback') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'fixMismatch') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'start') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'stop') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'remove') {
      targetContainer = s.operationInfo.containerName;
    } else if (s.operationInfo?.type === 'stackRestart') {
      // Stack restart: use event's container_name, or first container in the stack
      targetContainer = event.container_name || s.operationInfo.containers[0] || null;
    } else if (s.operationInfo?.type === 'stackStop') {
      targetContainer = event.container_name || s.operationInfo.containers[0] || null;
    }

    // Recovery mode fallback: operationInfo is null but we matched by operationId.
    // Use event.container_name if it exists in our container list.
    if (!targetContainer && event.container_name) {
      if (s.containers.some(c => c.name === event.container_name)) {
        targetContainer = event.container_name;
      }
    }
  }

  // Match by operation_id in container-to-operation map (strict: operation_id only)
  if (!isOurOperation && s.containerToOpId.size > 0) {
    // When event has a container_name, prefer matching that specific container
    if (event.container_name) {
      const opId = s.containerToOpId.get(event.container_name);
      if (opId === event.operation_id) {
        targetContainer = event.container_name;
        isOurOperation = true;
      }
    }
    // Fallback: find any container with matching operation_id (for events without container_name)
    if (!isOurOperation) {
      for (const [containerName, opId] of s.containerToOpId) {
        if (opId === event.operation_id) {
          targetContainer = containerName;
          isOurOperation = true;
          break;
        }
      }
    }
  }

  // Fallback: match by container name ONLY if we have no operation IDs tracked yet
  // (early SSE events before executor has set containerToOpId)
  if (!isOurOperation && event.container_name && s.containerToOpId.size === 0 && !s.operationId) {
    for (const c of s.containers) {
      if (c.name === event.container_name && c.status === 'in_progress') {
        targetContainer = c.name;
        isOurOperation = true;
        break;
      }
    }
  }

  if (!isOurOperation) return;

  // --- Dispatch SSE_EVENT to reducer (handles dedup, percent, container state, pending_restart) ---
  dispatch({
    type: 'SSE_EVENT',
    runId: s.runId,
    event,
    containerName: targetContainer,
  });

  // --- Side-effects that the reducer doesn't handle: logging & terminal actions ---
  const containerKey = targetContainer || '__global__';
  const lastStage = s.lastLoggedStage.get(containerKey);
  const isNewStage = lastStage !== event.stage;

  // Handle pending_restart stage — log + trigger polling via sawPendingRestart
  if (event.stage === 'pending_restart') {
    dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: 'Docksmith is restarting to apply the update...', type: 'info', icon: 'fa-rotate' } });
    dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: 'Will check for completion automatically...', type: 'info', icon: 'fa-wifi' } });
    return; // Don't process further stage logs for pending_restart
  }

  // Handle restarting_dependents stage — parse blocked/restarted from message
  if (event.stage === 'restarting_dependents') {
    const message = event.message || '';

    if (message.includes('Blocked dependents:')) {
      const blockedPart = message.split('Blocked dependents:')[1];
      if (blockedPart) {
        const names = blockedPart.split(',').map(n => n.trim()).filter(n => n);
        dispatch({ type: 'SET_DEPENDENTS', runId: s.runId, blocked: [...s.dependentsBlocked, ...names.filter(n => !s.dependentsBlocked.includes(n))] });
        dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `Blocked: ${names.join(', ')} (pre-update check failed)`, type: 'warning', icon: 'fa-ban' } });
      }
    } else if (message.includes('Warning:') || message.toLowerCase().includes('blocked')) {
      dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message, type: 'warning', icon: 'fa-triangle-exclamation' } });
    } else if (message.includes('Restarted dependents:') || message.includes('Dependents restarted:')) {
      const restartedPart = message.includes('Restarted dependents:')
        ? message.split('Restarted dependents:')[1]
        : message.split('Dependents restarted:')[1];
      if (restartedPart) {
        const names = restartedPart.split(',').map(n => n.trim()).filter(n => n);
        dispatch({ type: 'SET_DEPENDENTS', runId: s.runId, restarted: [...s.dependentsRestarted, ...names.filter(n => !s.dependentsRestarted.includes(n))] });
        dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `Restarted dependents: ${names.join(', ')}`, type: 'success', icon: 'fa-rotate' } });
      }
    }
  }

  // Determine if this is a single-container or multi-container operation.
  // SSE_COMPLETION_OPS types (rollback, restart, fixMismatch) can be single or batch;
  // check the actual container count to decide.
  const isSingleContainerOp = SSE_COMPLETION_OPS.includes(s.operationType as OperationType) && s.containers.length <= 1;
  const isMultiContainerOp = MULTI_CONTAINER_OPS.includes(s.operationType as OperationType) || s.containers.length > 1;

  // Add stage transition log — only log when stage actually changes
  const stageInfo = STAGE_INFO[event.stage] || RESTART_STAGES[event.stage];
  if (stageInfo && event.stage !== 'complete' && event.stage !== 'failed' && isNewStage) {
    const prefix = targetContainer && isMultiContainerOp ? `${targetContainer}: ` : '';
    dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `${prefix}${stageInfo.label}`, type: 'stage', icon: stageInfo.icon } });
  }

  // Handle completion
  if (event.stage === 'complete') {
    if (isSingleContainerOp) {
      dispatch({ type: 'SET_END_TIME', runId: s.runId, endTime: Date.now() });
      dispatch({ type: 'SET_STATUS', runId: s.runId, status: 'success' });
      const successMsg = s.operationType === 'rollback' ? 'Rollback completed successfully' : 'Operation completed successfully';
      dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: successMsg, type: 'success', icon: 'fa-circle-check' } });
    } else if (isMultiContainerOp && targetContainer) {
      dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `${targetContainer}: Completed successfully`, type: 'success', icon: 'fa-circle-check' } });
    }
    // Skip global completion events (no targetContainer) — the summary bar handles overall status
  } else if (event.stage === 'failed') {
    const errorMessage = event.message || 'Operation failed';

    if (isSingleContainerOp) {
      dispatch({ type: 'SET_END_TIME', runId: s.runId, endTime: Date.now() });
      dispatch({ type: 'SET_STATUS', runId: s.runId, status: 'failed' });
      dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `Error: ${errorMessage}`, type: 'error', icon: 'fa-circle-xmark' } });

      if (isPreCheckFailure(errorMessage)) {
        dispatch({ type: 'SET_FORCE_RETRY', runId: s.runId, canForceRetry: true, message: 'You can force the operation to bypass the pre-update check' });
        dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: 'You can force the operation to bypass the pre-update check', type: 'info', icon: 'fa-info-circle' } });
      }
    } else if (isMultiContainerOp) {
      const prefix = targetContainer ? `${targetContainer}: ` : '';
      dispatch({ type: 'ADD_LOG', runId: s.runId, entry: { time: Date.now(), message: `${prefix}${errorMessage}`, type: 'error', icon: 'fa-circle-xmark' } });
    }
  }
}
