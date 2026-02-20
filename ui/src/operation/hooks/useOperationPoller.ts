import { useEffect, useRef } from 'react';
import type { OperationAction } from '../types';

interface UseOperationPollerOptions {
  enabled: boolean;
  operationIds: string[];
  batchGroupId: string | null;
  runId: string;
  dispatch: React.Dispatch<OperationAction>;
  onOperationComplete?: (opId: string, op: any) => void;
  pollInterval?: number;
  sseConnected?: boolean;
}

export function useOperationPoller({
  enabled,
  operationIds,
  batchGroupId,
  runId,
  dispatch,
  onOperationComplete,
  pollInterval = 5000,
  sseConnected,
}: UseOperationPollerOptions) {
  const abortRef = useRef<AbortController | null>(null);
  // Persist across effect re-runs so we don't duplicate logs
  const loggedOpsRef = useRef(new Set<string>());
  // Use a ref so changing sseConnected doesn't restart the poll loop
  const sseConnectedRef = useRef(sseConnected);
  sseConnectedRef.current = sseConnected;

  useEffect(() => {
    if (!enabled) {
      abortRef.current?.abort();
      abortRef.current = null;
      return;
    }

    // Deduplicate operationIds and filter out sentinel/pending IDs
    const uniqueOpIds = [...new Set(operationIds)].filter(id => !id.startsWith('pending-'));
    if (uniqueOpIds.length === 0 && !batchGroupId) return;

    const controller = new AbortController();
    abortRef.current = controller;
    const { signal } = controller;

    const loggedOps = loggedOpsRef.current;
    const maxAttempts = batchGroupId ? 300 : 120;

    const poll = async () => {
      let attempts = 0;

      while (attempts < maxAttempts && !signal.aborted) {
        // Read ref each iteration so interval adapts without restarting the loop
        const interval = sseConnectedRef.current === false ? 2000 : pollInterval;
        await new Promise(resolve => setTimeout(resolve, interval));
        if (signal.aborted) return;
        attempts++;

        try {
          if (batchGroupId) {
            // Batch group polling
            const response = await fetch(`/api/operations/group/${batchGroupId}`, { signal });
            if (!response.ok) continue;

            const data = await response.json();
            if (!data.success || !data.data) continue;

            const ops = data.data.operations;
            if (!ops || ops.length === 0) continue; // No operations yet, keep polling
            let allDone = true;

            for (const op of ops) {
              const isTerminal = op.status === 'complete' || op.status === 'failed';

              if (isTerminal && op.batch_details && op.batch_details.length > 0) {
                // Use per-container batch_details for accurate per-container status
                for (const d of op.batch_details) {
                  if (d.status === 'complete') {
                    dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'success', message: d.message || 'Completed' });
                  } else if (d.status === 'failed') {
                    dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'failed', message: d.message || 'Failed', error: d.message });
                  } else {
                    // Fallback: op is terminal but detail has no terminal status — use op-level
                    const fallbackStatus = op.status === 'complete' ? 'success' as const : 'failed' as const;
                    dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: fallbackStatus, message: d.message || op.error_message || op.status });
                  }
                }
                if (!loggedOps.has(op.operation_id)) {
                  loggedOps.add(op.operation_id);
                  const names = op.batch_details.map((d: any) => d.container_name);
                  const msg = op.status === 'complete' ? 'Update completed' : (op.error_message || 'Update failed');
                  const logType = op.status === 'complete' ? 'success' : 'error';
                  const logIcon = op.status === 'complete' ? 'fa-circle-check' : 'fa-circle-xmark';
                  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `${names.join(', ')}: ${msg}`, type: logType, icon: logIcon } });
                  onOperationComplete?.(op.operation_id, op);
                }
              } else if (isTerminal) {
                // No batch_details — single container op
                const name = op.container_name || 'Unknown';
                const status = op.status === 'complete' ? 'success' as const : 'failed' as const;
                dispatch({ type: 'POLL_UPDATE', runId, containerName: name, status, message: op.error_message || (op.status === 'complete' ? 'Completed' : 'Failed'), error: op.status === 'failed' ? op.error_message : undefined });
                if (!loggedOps.has(op.operation_id)) {
                  loggedOps.add(op.operation_id);
                  const msg = op.status === 'complete' ? 'Update completed' : (op.error_message || 'Update failed');
                  const logType = op.status === 'complete' ? 'success' : 'error';
                  const logIcon = op.status === 'complete' ? 'fa-circle-check' : 'fa-circle-xmark';
                  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `${name}: ${msg}`, type: logType, icon: logIcon } });
                  onOperationComplete?.(op.operation_id, op);
                }
              } else {
                // Operation still in progress — check per-container batch_details
                allDone = false;
                if (op.batch_details && op.batch_details.length > 0) {
                  for (const d of op.batch_details) {
                    if (d.status === 'complete') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'success', message: d.message || 'Completed' });
                    } else if (d.status === 'failed') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'failed', message: d.message || 'Failed', error: d.message });
                    } else if (d.status === 'in_progress') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'in_progress', message: d.message || 'In progress' });
                    }
                  }
                }
              }
            }

            if (allDone) {
              const allComplete = ops.every((op: any) => op.status === 'complete');
              dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
              dispatch({ type: 'SET_STATUS', runId, status: allComplete ? 'success' : 'failed' });
              dispatch({
                type: 'ADD_LOG', runId,
                entry: {
                  time: Date.now(),
                  message: allComplete ? 'All operations completed' : 'Some operations failed',
                  type: allComplete ? 'success' : 'error',
                  icon: allComplete ? 'fa-circle-check' : 'fa-circle-xmark',
                },
              });
              return;
            }
          } else {
            // Individual operation polling
            for (const opId of uniqueOpIds) {
              if (signal.aborted) return;

              try {
                const response = await fetch(`/api/operations/${opId}`, { signal });
                if (!response.ok) continue;

                const data = await response.json();
                if (!data.success || !data.data) continue;

                const op = data.data;
                const isTerminal = op.status === 'complete' || op.status === 'failed';

                if (op.batch_details && op.batch_details.length > 0) {
                  // Process per-container batch_details (both in-progress and terminal)
                  for (const d of op.batch_details) {
                    if (d.status === 'complete') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'success', message: d.message || 'Completed' });
                    } else if (d.status === 'failed') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'failed', message: d.message || 'Failed', error: d.message });
                    } else if (d.status === 'in_progress') {
                      dispatch({ type: 'POLL_UPDATE', runId, containerName: d.container_name, status: 'in_progress', message: d.message || 'In progress' });
                    }
                  }

                  if (isTerminal && !loggedOps.has(opId)) {
                    loggedOps.add(opId);
                    const names = op.batch_details.map((d: any) => d.container_name);
                    const msg = op.status === 'complete' ? 'Update completed' : (op.error_message || 'Update failed');
                    const logType = op.status === 'complete' ? 'success' : 'error';
                    const logIcon = op.status === 'complete' ? 'fa-circle-check' : 'fa-circle-xmark';
                    dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `${names.join(', ')}: ${msg}`, type: logType, icon: logIcon } });
                    onOperationComplete?.(opId, op);
                  }

                  if (isTerminal) {
                    dispatch({ type: 'SET_END_TIME', runId, endTime: Date.now() });
                    dispatch({ type: 'SET_STATUS', runId, status: op.status === 'complete' ? 'success' : 'failed' });
                    return;
                  }
                } else {
                  // Single container op (no batch_details)
                  const containerName = op.container_name || 'Unknown';

                  if (op.status === 'complete') {
                    dispatch({ type: 'POLL_UPDATE', runId, containerName, status: 'success', message: op.error_message || 'Completed successfully' });
                    if (!loggedOps.has(opId)) {
                      loggedOps.add(opId);
                      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `${containerName}: Completed`, type: 'success', icon: 'fa-circle-check' } });
                      onOperationComplete?.(opId, op);
                    }
                  } else if (op.status === 'failed') {
                    dispatch({ type: 'POLL_UPDATE', runId, containerName, status: 'failed', message: op.error_message || 'Failed', error: op.error_message });
                    if (!loggedOps.has(opId)) {
                      loggedOps.add(opId);
                      dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: `${containerName}: ${op.error_message || 'Failed'}`, type: 'error', icon: 'fa-circle-xmark' } });
                      onOperationComplete?.(opId, op);
                    }
                  }
                }
              } catch (err) {
                if (signal.aborted) return;
                // Network error, continue polling
              }
            }
          }
        } catch (err) {
          if (signal.aborted) return;
          // Network error, continue polling
        }
      }

      // Timeout
      if (!signal.aborted) {
        dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message: 'Timed out waiting for completion', type: 'error', icon: 'fa-clock' } });
        dispatch({ type: 'SET_STATUS', runId, status: 'failed' });
      }
    };

    poll();

    return () => {
      controller.abort();
    };
  }, [enabled, operationIds.join(','), batchGroupId, runId, pollInterval]); // eslint-disable-line react-hooks/exhaustive-deps
}
