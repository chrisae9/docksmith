import { useRef, useMemo, useCallback, useEffect, useState } from 'react';
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom';
import { useEventStream } from '../hooks/useEventStream';
import { useElapsedTime } from '../hooks/useElapsedTime';
import { useOperationState } from '../operation/hooks/useOperationState';
import { useSSEProgress } from '../operation/hooks/useSSEProgress';
import { useOperationPoller } from '../operation/hooks/useOperationPoller';
import { useRecovery } from '../operation/hooks/useRecovery';
import { useSelfRestartRecovery } from '../operation/hooks/useSelfRestartRecovery';
import { useOperationExecutor } from '../operation/hooks/useOperationExecutor';
import { NoOperationFallback } from '../operation/components/NoOperationFallback';
import { SummaryBar } from '../operation/components/SummaryBar';
import { OperationHeader } from '../operation/components/OperationHeader';
import { OperationFooter } from '../operation/components/OperationFooter';
import { GroupedContainerList } from '../operation/components/GroupedContainerList';
import { parseLocationState, getPageTitle, getForceButtonLabel } from '../operation/utils';
import { selectSuccessCount, selectIsTerminal, selectHasErrors } from '../operation/selectors';
import { ACTIVE_OPERATION_KEY } from '../utils/constants';
import '../styles/progress-common.css';
import './OperationProgressPage.css';

export function OperationProgressPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const logRef = useRef<HTMLDivElement>(null);

  // Parse operation info from navigation state
  const operationInfo = useMemo(() => parseLocationState(location.state), [location.state]);

  // URL params for recovery mode (page refresh mid-operation)
  const urlOperationId = searchParams.get('id');
  const urlBatchGroupId = searchParams.get('group');
  const isRecoveryMode = !operationInfo && (!!urlOperationId || !!urlBatchGroupId);

  // Central state machine (single source of truth)
  const [state, dispatch] = useOperationState();

  // SSE event stream
  const { eventQueue, eventSeq, wasDisconnected, clearWasDisconnected, connected } = useEventStream(state.phase === 'running');

  // Hook: execute operation on mount (fresh operations only)
  useOperationExecutor({ operationInfo, dispatch, runId: state.runId });

  // Hook: recover state from URL params (page refresh recovery)
  useRecovery({ urlOperationId, urlBatchGroupId, operationInfo, phase: state.phase, runId: state.runId, dispatch });

  // Hook: process SSE events into state (uses ref-based queue to avoid React batching event loss)
  useSSEProgress(state, dispatch, eventQueue, eventSeq);

  // Hook: poll for authoritative status (fallback for SSE)
  const pollerOperationIds = useMemo(() => {
    const ids: string[] = [];
    if (state.operationId) ids.push(state.operationId);
    state.containerToOpId.forEach(id => ids.push(id));
    return [...new Set(ids)];
  }, [state.operationId, state.containerToOpId]);

  useOperationPoller({
    enabled: state.phase === 'running' && !state.sawPendingRestart && (pollerOperationIds.length > 0 || !!state.batchGroupId),
    operationIds: pollerOperationIds,
    batchGroupId: state.batchGroupId,
    runId: state.runId,
    dispatch,
    sseConnected: connected,
  });

  // Catch up on missed events when SSE reconnects after a disconnect
  useEffect(() => {
    if (!wasDisconnected || state.phase !== 'running') return;
    clearWasDisconnected();

    if (state.batchGroupId) {
      fetch(`/api/operations/group/${state.batchGroupId}`)
        .then(r => r.json())
        .then(data => {
          if (!data.success || !data.data) return;
          for (const op of data.data.operations) {
            const isTerminal = op.status === 'complete' || op.status === 'failed';
            if (op.batch_details) {
              for (const d of op.batch_details) {
                if (d.status === 'complete') {
                  dispatch({ type: 'POLL_UPDATE', runId: state.runId, containerName: d.container_name, status: 'success', message: d.message || 'Completed' });
                } else if (d.status === 'failed') {
                  dispatch({ type: 'POLL_UPDATE', runId: state.runId, containerName: d.container_name, status: 'failed', message: d.message || 'Failed', error: d.message });
                } else if (d.status === 'in_progress') {
                  dispatch({ type: 'POLL_UPDATE', runId: state.runId, containerName: d.container_name, status: 'in_progress', message: d.message || 'In progress' });
                } else if (isTerminal) {
                  // Fallback: op is terminal but detail has no per-container status — use op-level
                  const fallbackStatus = op.status === 'complete' ? 'success' as const : 'failed' as const;
                  dispatch({ type: 'POLL_UPDATE', runId: state.runId, containerName: d.container_name, status: fallbackStatus, message: d.message || op.error_message || op.status });
                }
              }
            }
          }
        })
        .catch(() => {});
    }
  }, [wasDisconnected]); // eslint-disable-line react-hooks/exhaustive-deps

  // Persist active operation to sessionStorage so global banner can show it
  useEffect(() => {
    if (state.phase === 'running') {
      const url = state.batchGroupId
        ? `/operation?group=${state.batchGroupId}`
        : state.operationId
        ? `/operation?id=${state.operationId}`
        : null;
      if (url) {
        sessionStorage.setItem(ACTIVE_OPERATION_KEY, JSON.stringify({
          url,
          type: state.operationType,
          containerCount: state.containers.length,
          startTime: state.startTime,
        }));
      }
    } else if (state.phase === 'completed' || state.phase === 'failed' || state.phase === 'partial') {
      sessionStorage.removeItem(ACTIVE_OPERATION_KEY);
    }
  }, [state.phase, state.batchGroupId, state.operationId, state.operationType, state.containers.length, state.startTime]);

  // Hook: self-restart recovery (when docksmith updates itself)
  useSelfRestartRecovery({
    operationId: state.operationId,
    sawPendingRestart: state.sawPendingRestart,
    wasDisconnected,
    phase: state.phase,
    runId: state.runId,
    dispatch,
    clearWasDisconnected,
  });

  // Derived values
  const isComplete = selectIsTerminal(state);
  const hasErrors = selectHasErrors(state);
  const successCount = selectSuccessCount(state);
  const elapsedTime = useElapsedTime(state.startTime, state.phase === 'running', state.endTime);

  // Activity log: collapsible, auto-expand on errors
  const [logCollapsed, setLogCollapsed] = useState(true);

  useEffect(() => {
    if (hasErrors) setLogCollapsed(false);
  }, [hasErrors]);

  // Auto-scroll activity log
  useEffect(() => {
    if (logRef.current && !logCollapsed) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [state.logs.length, logCollapsed]);

  // Action handlers
  const handleBack = useCallback(() => navigate(-1), [navigate]);

  const handleForceRetry = useCallback(() => {
    if (!state.operationInfo) return;
    navigate('/operation', {
      state: { [state.operationInfo.type]: { ...state.operationInfo, force: true } },
      replace: true,
    });
    window.location.reload();
  }, [state.operationInfo, navigate]);

  const handleRetry = useCallback(() => {
    if (!state.operationInfo) return;
    navigate('/operation', {
      state: { [state.operationInfo.type]: state.operationInfo },
      replace: true,
    });
    window.location.reload();
  }, [state.operationInfo, navigate]);

  const handleRollback = useCallback(() => {
    const groupId = searchParams.get('group');
    if (!groupId) return;
    const successfulContainers = state.containers.filter(c => c.status === 'success').map(c => c.name);
    navigate('/operation', {
      state: { labelRollback: { batchGroupId: groupId, containers: successfulContainers } },
      replace: true,
    });
    window.location.reload();
  }, [searchParams, state.containers, navigate]);

  // No operation context — show recent operations fallback
  if (!operationInfo && !isRecoveryMode && state.phase === 'idle') {
    return <NoOperationFallback />;
  }

  // Computed display values
  const title = getPageTitle(state.operationType, state.recoveredOperation);
  const showRollback = isComplete && state.operationType === 'batchLabel' && successCount > 0;

  return (
    <div className="progress-page operation-progress-page">
      <OperationHeader title={title} isComplete={isComplete} onBack={handleBack} />

      <main className="page-content">
        <SummaryBar
          state={state}
          isComplete={isComplete}
          hasErrors={hasErrors}
          elapsedTime={elapsedTime}
        />

        <GroupedContainerList
          containers={state.containers}
          expectedDependents={state.expectedDependents}
          dependentsRestarted={state.dependentsRestarted}
          dependentsBlocked={state.dependentsBlocked}
          currentStage={state.currentStage}
        />

        {/* Collapsible Activity Log */}
        <section className={`activity-section ${logCollapsed ? 'collapsed' : ''}`}>
          <button className="activity-toggle" onClick={() => setLogCollapsed(!logCollapsed)}>
            <i className={`fa-solid fa-chevron-${logCollapsed ? 'right' : 'down'}`}></i>
            <i className="fa-solid fa-list-check"></i>
            <span>Activity Log</span>
            <span className="activity-count">({state.logs.length})</span>
          </button>
          {!logCollapsed && (
            <div className="activity-log" ref={logRef}>
              {state.logs.map((log, i) => (
                <div key={i} className={`log-entry log-${log.type}`}>
                  <span className="log-time">
                    [{new Date(log.time).toLocaleTimeString('en-US', { hour12: false })}]
                  </span>
                  {log.icon && (
                    <span className="log-icon">
                      <i className={`fa-solid ${log.icon}`}></i>
                    </span>
                  )}
                  <span className="log-message">{log.message}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      </main>

      <OperationFooter
        isComplete={isComplete}
        canForceRetry={state.canForceRetry}
        canRetry={state.canRetry}
        forceRetryMessage={state.forceRetryMessage}
        forceButtonLabel={getForceButtonLabel(state.operationType)}
        showRollback={showRollback}
        onForceRetry={handleForceRetry}
        onRetry={handleRetry}
        onBack={handleBack}
        onRollback={handleRollback}
      />
    </div>
  );
}
