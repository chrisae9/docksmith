import { useEffect, useRef } from 'react';
import type { OperationAction, OperationPhase } from '../types';

interface UseSelfRestartRecoveryOptions {
  operationId: string | null;
  sawPendingRestart: boolean;
  wasDisconnected: boolean;
  phase: OperationPhase;
  runId: string;
  dispatch: React.Dispatch<OperationAction>;
  clearWasDisconnected: () => void;
}

export function useSelfRestartRecovery({
  operationId,
  sawPendingRestart,
  wasDisconnected,
  phase,
  runId,
  dispatch,
  clearWasDisconnected,
}: UseSelfRestartRecoveryOptions) {
  const abortRef = useRef<AbortController | null>(null);
  const pollingRef = useRef(false);

  // Trigger 1: pending_restart stage detected from SSE — start polling with backoff
  useEffect(() => {
    if (!sawPendingRestart || !operationId || phase !== 'running' || pollingRef.current) return;

    pollingRef.current = true;
    const controller = new AbortController();
    abortRef.current = controller;
    const { signal } = controller;

    const pollForRestart = async () => {
      const maxAttempts = 120;
      const baseInterval = 2000;

      for (let attempt = 0; attempt < maxAttempts; attempt++) {
        if (signal.aborted) return;

        // Backoff with jitter: 2s base, up to 5s max, with random jitter
        const delay = Math.min(baseInterval + attempt * 100, 5000) + Math.random() * 500;
        await new Promise(resolve => setTimeout(resolve, delay));
        if (signal.aborted) return;

        try {
          const response = await fetch(`/api/operations/${operationId}`, { signal });
          if (!response.ok) continue; // Transient error (502, etc.) — keep polling

          const data = await response.json();
          if (!data.success || !data.data) continue;

          const op = data.data;

          if (op.status === 'complete') {
            dispatch({ type: 'POLL_UPDATE', runId, containerName: op.container_name || 'Unknown', status: 'success', message: op.error_message || 'Completed successfully' });
            dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
            dispatch({ type: 'SET_STATUS', runId, status: 'success' });
            dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Self-restart completed successfully', type: 'success', icon: 'fa-circle-check' } });
            dispatch({ type: 'SET_SAW_PENDING_RESTART', runId, value: false });
            pollingRef.current = false;
            return;
          } else if (op.status === 'failed') {
            dispatch({ type: 'POLL_UPDATE', runId, containerName: op.container_name || 'Unknown', status: 'failed', message: op.error_message || 'Failed' });
            dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
            dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
            dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Self-restart failed', type: 'error', icon: 'fa-circle-xmark' } });
            dispatch({ type: 'SET_SAW_PENDING_RESTART', runId, value: false });
            pollingRef.current = false;
            return;
          }
          // Still pending_restart or in_progress — keep polling
        } catch (err) {
          if (signal.aborted) return;
          // ECONNREFUSED / network error — server still restarting, keep polling
        }
      }

      // Timeout
      if (!signal.aborted) {
        dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Timed out waiting for restart completion', type: 'error', icon: 'fa-clock' } });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
        pollingRef.current = false;
      }
    };

    pollForRestart();

    return () => {
      controller.abort();
      pollingRef.current = false;
    };
  }, [sawPendingRestart, operationId, phase, runId, dispatch]);

  // Trigger 2: SSE reconnection after disconnect — poll once to check status
  useEffect(() => {
    if (!wasDisconnected || !sawPendingRestart || !operationId || phase !== 'running') return;

    clearWasDisconnected();

    const checkStatus = async () => {
      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Checking operation status after reconnect...', type: 'info', icon: 'fa-wifi' } });

      // Small delay to ensure backend is fully ready
      await new Promise(resolve => setTimeout(resolve, 1500));

      try {
        const response = await fetch(`/api/operations/${operationId}`);
        if (!response.ok) {
          dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `API error: ${response.status}`, type: 'error', icon: 'fa-circle-xmark' } });
          return;
        }

        const data = await response.json();
        if (!data.success || !data.data) {
          dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'No operation data returned', type: 'error', icon: 'fa-circle-xmark' } });
          return;
        }

        const op = data.data;
        dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Operation status: ${op.status}`, type: 'info', icon: 'fa-info-circle' } });

        if (op.status === 'complete') {
          const containerName = op.container_name || 'Unknown';
          dispatch({ type: 'POLL_UPDATE', runId, containerName, status: 'success', message: op.error_message || 'Completed successfully' });
          dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
          dispatch({ type: 'SET_STATUS', runId, status: 'success' });
          dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Operation completed successfully', type: 'success', icon: 'fa-circle-check' } });
          dispatch({ type: 'SET_SAW_PENDING_RESTART', runId, value: false });
        } else if (op.status === 'failed') {
          const containerName = op.container_name || 'Unknown';
          dispatch({ type: 'POLL_UPDATE', runId, containerName, status: 'failed', message: op.error_message || 'Operation failed' });
          dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
          dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
          dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: op.error_message || 'Operation failed', type: 'error', icon: 'fa-circle-xmark' } });
          dispatch({ type: 'SET_SAW_PENDING_RESTART', runId, value: false });
        } else {
          // Still in progress — wait for SSE events or continued polling
          dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Waiting for completion (current: ${op.status})...`, type: 'info', icon: 'fa-spinner' } });
        }
      } catch (err) {
        const errMsg = err instanceof Error ? err.message : 'Unknown error';
        dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `Failed to check status: ${errMsg}`, type: 'error', icon: 'fa-circle-xmark' } });
      }
    };

    checkStatus();
  }, [wasDisconnected, sawPendingRestart, operationId, phase, runId, dispatch, clearWasDisconnected]);
}
