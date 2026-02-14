import type { OperationState, OperationAction, OperationPhase } from './types';

export function generateRunId(): string {
  return crypto.randomUUID();
}

export function createInitialState(): OperationState {
  return {
    runId: generateRunId(),
    phase: 'idle',
    operationType: null,
    operationInfo: null,
    containers: [],
    logs: [],
    operationId: null,
    batchGroupId: null,
    startTime: null,
    endTime: null,
    currentStage: null,
    currentPercent: 0,
    canForceRetry: false,
    forceRetryMessage: '',
    canRetry: false,
    recoveredOperation: null,
    expectedDependents: [],
    dependentsRestarted: [],
    dependentsBlocked: [],
    containerToOpId: new Map(),
    processedEvents: new Set(),
    maxPercent: new Map(),
    lastLoggedStage: new Map(),
    sawPendingRestart: false,
  };
}

// Check if all containers are done, derive phase from container states
function checkAllDone(state: OperationState): OperationState {
  if (state.containers.length === 0) return state;
  const allDone = state.containers.every(c => c.status === 'success' || c.status === 'failed');
  if (!allDone) return state;
  const anyFailed = state.containers.some(c => c.status === 'failed');
  const allFailed = state.containers.every(c => c.status === 'failed');
  return {
    ...state,
    phase: allFailed ? 'failed' : anyFailed ? 'partial' : 'completed',
    endTime: state.endTime || Date.now(),
  };
}

export function operationReducer(state: OperationState, action: OperationAction): OperationState {
  // runId guard — ignore stale-run actions (except RETRY which provides newRunId)
  if (action.type !== 'RETRY' && 'runId' in action && action.runId !== state.runId) {
    return state;
  }

  switch (action.type) {
    case 'INIT': {
      return {
        ...state,
        runId: action.runId,
        phase: 'running',
        operationType: action.operationType,
        operationInfo: action.operationInfo,
        containers: action.containers,
        logs: [],
        startTime: Date.now(),
        endTime: null,
        currentStage: null,
        currentPercent: 0,
        canForceRetry: false,
        forceRetryMessage: '',
        canRetry: false,
        recoveredOperation: null,
        processedEvents: new Set(),
        maxPercent: new Map(),
        lastLoggedStage: new Map(),
        sawPendingRestart: false,
      };
    }

    case 'CONTAINER_UPDATE': {
      const containers = state.containers.map(c => {
        if (c.name !== action.containerName) return c;
        // Terminal state guard — don't overwrite success/failed
        if (c.status === 'success' || c.status === 'failed') return c;
        return { ...c, ...action.updates };
      });
      return { ...state, containers };
    }

    case 'CONTAINER_COMPLETED': {
      const containers = state.containers.map(c => {
        if (c.name !== action.containerName) return c;
        if (c.status === 'success' || c.status === 'failed') return c; // terminal guard
        return { ...c, status: 'success' as const, percent: 100, message: action.message || 'Completed' };
      });
      return checkAllDone({ ...state, containers });
    }

    case 'CONTAINER_FAILED': {
      const containers = state.containers.map(c => {
        if (c.name !== action.containerName) return c;
        if (c.status === 'success' || c.status === 'failed') return c; // terminal guard
        return { ...c, status: 'failed' as const, message: action.message, error: action.error };
      });
      return checkAllDone({ ...state, containers });
    }

    case 'CONTAINERS_WHERE_UPDATE': {
      const containers = state.containers.map(c => {
        if (!action.predicate(c)) return c;
        const updates = typeof action.updates === 'function' ? action.updates(c) : action.updates;
        return { ...c, ...updates };
      });
      return { ...state, containers };
    }

    case 'SET_OPERATION_ID': {
      return { ...state, operationId: action.operationId };
    }

    case 'SET_BATCH_GROUP_ID': {
      return { ...state, batchGroupId: action.batchGroupId };
    }

    case 'SET_STATUS': {
      // Don't override terminal phases derived by checkAllDone — it's the authority
      // on container-driven phase transitions. Only allow escalation (partial → failed).
      if (state.phase === 'completed' || state.phase === 'failed') return state;
      if (state.phase === 'partial' && action.status === 'success') return state; // Don't downgrade partial to completed
      const phase: OperationPhase = action.status === 'success' ? 'completed' : action.status === 'failed' ? 'failed' : 'running';
      return { ...state, phase };
    }

    case 'SET_END_TIME': {
      return { ...state, endTime: action.endTime };
    }

    case 'ADD_LOG': {
      return { ...state, logs: [...state.logs, action.entry] };
    }

    case 'SET_FORCE_RETRY': {
      return { ...state, canForceRetry: action.canForceRetry, forceRetryMessage: action.message };
    }

    case 'SET_CAN_RETRY': {
      return { ...state, canRetry: action.canRetry };
    }

    case 'SET_STAGE': {
      // Monotonic progress — only increase, never decrease
      const newPercent = Math.max(state.currentPercent, action.percent);
      return { ...state, currentStage: action.stage, currentPercent: newPercent };
    }

    case 'SET_DEPENDENTS': {
      return {
        ...state,
        expectedDependents: action.expected ?? state.expectedDependents,
        dependentsRestarted: action.restarted ?? state.dependentsRestarted,
        dependentsBlocked: action.blocked ?? state.dependentsBlocked,
      };
    }

    case 'SET_CONTAINER_OP_ID': {
      const newMap = new Map(state.containerToOpId);
      newMap.set(action.containerName, action.operationId);
      return { ...state, containerToOpId: newMap };
    }

    case 'SET_SAW_PENDING_RESTART': {
      return { ...state, sawPendingRestart: action.value };
    }

    case 'RECOVERY_LOADED': {
      const merged = {
        ...state,
        ...action.state,
        // Preserve runId so subsequent actions aren't rejected
        runId: state.runId,
      };
      // Ensure Map/Set fields are never undefined after spread
      // (recovery state may omit these, causing .forEach() to crash)
      merged.containerToOpId = merged.containerToOpId ?? new Map();
      merged.processedEvents = merged.processedEvents ?? new Set();
      merged.maxPercent = merged.maxPercent ?? new Map();
      merged.lastLoggedStage = merged.lastLoggedStage ?? new Map();
      return merged;
    }

    case 'SSE_EVENT': {
      // Deduplication: build event key and check processedEvents
      const event = action.event;
      const eventKey = `${event.operation_id}-${event.container_name || ''}-${event.stage}-${event.progress || event.percent}`;
      if (state.processedEvents.has(eventKey)) return state;

      const newProcessed = new Set(state.processedEvents);
      newProcessed.add(eventKey);

      const eventPercent = event.percent || event.progress || 0;
      const containerKey = action.containerName || '__global__';

      // Get the max percent seen so far for this container
      const currentMaxPercent = state.maxPercent.get(containerKey) || 0;
      const lastStage = state.lastLoggedStage.get(containerKey);
      const isNewStage = lastStage !== event.stage;
      const effectivePercent = isNewStage ? eventPercent : Math.max(currentMaxPercent, eventPercent);

      // Update max percent tracker
      const newMaxPercent = new Map(state.maxPercent);
      if (effectivePercent > currentMaxPercent || isNewStage) {
        newMaxPercent.set(containerKey, effectivePercent);
      }

      // Track new stage
      const newLastLoggedStage = new Map(state.lastLoggedStage);
      if (isNewStage) {
        newLastLoggedStage.set(containerKey, event.stage);
      }

      // Update current stage display (monotonic)
      const newCurrentPercent = Math.max(state.currentPercent, effectivePercent);

      // Update container if we have a target
      let newContainers = state.containers;
      if (action.containerName) {
        newContainers = state.containers.map(c => {
          if (c.name !== action.containerName) return c;
          if (c.status === 'success' || c.status === 'failed') return c; // terminal guard
          const newStatus = event.stage === 'complete' ? 'success' as const : event.stage === 'failed' ? 'failed' as const : 'in_progress' as const;
          return { ...c, status: newStatus, stage: event.stage, percent: effectivePercent, message: event.message };
        });
      }

      // Handle pending_restart
      const newSawPendingRestart = event.stage === 'pending_restart' ? true : state.sawPendingRestart;

      let result: OperationState = {
        ...state,
        containers: newContainers,
        processedEvents: newProcessed,
        maxPercent: newMaxPercent,
        lastLoggedStage: newLastLoggedStage,
        currentStage: event.stage,
        currentPercent: newCurrentPercent,
        sawPendingRestart: newSawPendingRestart,
      };

      // Check if all containers are done after update
      if (action.containerName) {
        result = checkAllDone(result);
      }

      return result;
    }

    case 'POLL_UPDATE': {
      const containers = state.containers.map(c => {
        if (c.name !== action.containerName) return c;
        if (c.status === 'success' || c.status === 'failed') return c; // terminal guard
        return {
          ...c,
          status: action.status,
          percent: action.status === 'success' ? 100 : c.percent,
          message: action.message,
          error: action.error,
        };
      });
      return checkAllDone({ ...state, containers });
    }

    case 'RETRY': {
      const fresh = createInitialState();
      return {
        ...fresh,
        runId: action.newRunId,
        operationType: state.operationType,
        operationInfo: state.operationInfo,
      };
    }

    default:
      return state;
  }
}
