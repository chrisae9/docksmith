import { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom';
import { startRestart, startStackRestart, setLabels as setLabelsAPI, triggerBatchUpdate, startContainer, stopContainer, removeContainer, fixComposeMismatch, getOperationsByGroup, batchSetLabels, batchStartContainers, batchStopContainers, batchRestartContainers, batchRemoveContainers, rollbackLabels } from '../api/client';
import { useEventStream } from '../hooks/useEventStream';
import type { UpdateProgressEvent } from '../hooks/useEventStream';
import { useElapsedTime } from '../hooks/useElapsedTime';
import { STAGE_INFO, RESTART_STAGES, type LogEntry } from '../constants/progress';
import { ContainerProgressRow, ProgressStats, ActivityLog, type ContainerProgress } from '../components/ProgressComponents';
import '../styles/progress-common.css';
import './OperationProgressPage.css';

// Operation types
type OperationType = 'restart' | 'update' | 'rollback' | 'start' | 'stop' | 'remove' | 'stackRestart' | 'stackStop' | 'fixMismatch' | 'batchFixMismatch' | 'mixed' | 'batchLabel' | 'batchStart' | 'batchStop' | 'batchRestart' | 'batchRemove' | 'labelRollback';

// Restart operation info (includes save settings)
interface RestartOperation {
  type: 'restart';
  containerName: string;
  force?: boolean;
  saveSettings?: boolean;
  labelChanges?: {
    ignore?: boolean;
    allow_latest?: boolean;
    version_pin_major?: boolean;
    version_pin_minor?: boolean;
    version_pin_patch?: boolean;
    tag_regex?: string;
    script?: string;
    restart_after?: string;
  };
}

// Update operation info
interface UpdateOperation {
  type: 'update';
  containers: Array<{
    name: string;
    target_version: string;
    stack: string;
    force?: boolean;
    change_type?: number;
    old_resolved_version?: string;
    new_resolved_version?: string;
  }>;
}

// Rollback operation info
interface RollbackOperation {
  type: 'rollback';
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
  force?: boolean;
}

// Start operation info
interface StartOperation {
  type: 'start';
  containerName: string;
}

// Stop operation info
interface StopOperation {
  type: 'stop';
  containerName: string;
}

// Remove operation info
interface RemoveOperation {
  type: 'remove';
  containerName: string;
  force?: boolean;
}

// Stack restart operation info (restart multiple containers in a stack)
interface StackRestartOperation {
  type: 'stackRestart';
  stackName: string;
  containers: string[];
  force?: boolean;
}

// Stack stop operation info (stop multiple containers in a stack)
interface StackStopOperation {
  type: 'stackStop';
  stackName: string;
  containers: string[];
}

// Fix mismatch operation info (sync container to compose file)
interface FixMismatchOperation {
  type: 'fixMismatch';
  containerName: string;
}

// Batch fix mismatch operation info (sync multiple containers to compose files)
interface BatchFixMismatchOperation {
  type: 'batchFixMismatch';
  containerNames: string[];
}

// Mixed operation info (both updates and mismatches selected together)
interface MixedOperation {
  type: 'mixed';
  updates: Array<{
    name: string;
    target_version: string;
    stack: string;
    force?: boolean;
  }>;
  mismatches: string[];
}

// Batch label operation info (apply label changes to multiple containers)
interface BatchLabelOperation {
  type: 'batchLabel';
  containers: string[];
  labelOp: {
    ignore?: boolean;
    allow_latest?: boolean;
    version_pin_major?: boolean;
    version_pin_minor?: boolean;
    version_pin_patch?: boolean;
    tag_regex?: string;
    script?: string;
  };
}

// Batch start operation info (start multiple containers via batch endpoint)
interface BatchStartOperation {
  type: 'batchStart';
  containers: string[];
}

// Batch stop operation info (stop multiple containers via batch endpoint)
interface BatchStopOperation {
  type: 'batchStop';
  containers: string[];
}

// Batch restart operation info (restart multiple containers via batch endpoint)
interface BatchRestartOperation {
  type: 'batchRestart';
  containers: string[];
}

// Batch remove operation info (remove multiple containers via batch endpoint)
interface BatchRemoveOperation {
  type: 'batchRemove';
  containers: string[];
}

// Label rollback operation info (reverse label changes from a previous batch)
interface LabelRollbackOperation {
  type: 'labelRollback';
  batchGroupId: string;
  containers: string[];
  containerNames?: string[]; // Optional: rollback specific containers only
}

type OperationInfo = RestartOperation | UpdateOperation | RollbackOperation | StartOperation | StopOperation | RemoveOperation | StackRestartOperation | StackStopOperation | FixMismatchOperation | BatchFixMismatchOperation | MixedOperation | BatchLabelOperation | BatchStartOperation | BatchStopOperation | BatchRestartOperation | BatchRemoveOperation | LabelRollbackOperation;

export function OperationProgressPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();

  // Check for operation ID or batch group ID in URL (for page refresh recovery)
  const urlOperationId = searchParams.get('id');
  const urlBatchGroupId = searchParams.get('group');

  // Determine operation type from location state - capture once on mount
  const getOperationInfoFromState = (state: any): OperationInfo | null => {
    if (!state) return null;

    if (state.restart) {
      return { type: 'restart', ...state.restart };
    }
    if (state.update) {
      return { type: 'update', ...state.update };
    }
    if (state.rollback) {
      return { type: 'rollback', ...state.rollback };
    }
    if (state.start) {
      return { type: 'start', ...state.start };
    }
    if (state.stop) {
      return { type: 'stop', ...state.stop };
    }
    if (state.remove) {
      return { type: 'remove', ...state.remove };
    }
    if (state.stackRestart) {
      return { type: 'stackRestart', ...state.stackRestart };
    }
    if (state.stackStop) {
      return { type: 'stackStop', ...state.stackStop };
    }
    if (state.fixMismatch) {
      return { type: 'fixMismatch', ...state.fixMismatch };
    }
    if (state.batchFixMismatch) {
      return { type: 'batchFixMismatch', ...state.batchFixMismatch };
    }
    if (state.mixed) {
      return { type: 'mixed', ...state.mixed };
    }
    if (state.batchLabel) {
      return { type: 'batchLabel', ...state.batchLabel };
    }
    if (state.batchStart) {
      return { type: 'batchStart', ...state.batchStart };
    }
    if (state.batchStop) {
      return { type: 'batchStop', ...state.batchStop };
    }
    if (state.batchRestart) {
      return { type: 'batchRestart', ...state.batchRestart };
    }
    if (state.batchRemove) {
      return { type: 'batchRemove', ...state.batchRemove };
    }
    if (state.labelRollback) {
      return { type: 'labelRollback', ...state.labelRollback };
    }
    return null;
  };

  // Store operation info in ref so it persists even after clearing location state
  const operationInfoRef = useRef<OperationInfo | null>(getOperationInfoFromState(location.state));
  const operationInfo = operationInfoRef.current;
  const operationType: OperationType | null = operationInfo?.type || null;

  // Recovery mode: we have an operation ID or batch group ID in URL but no location state
  const isRecoveryMode = !operationInfo && (!!urlOperationId || !!urlBatchGroupId);

  // Common state
  const [status, setStatus] = useState<'in_progress' | 'success' | 'failed'>('in_progress');
  const [containers, setContainers] = useState<ContainerProgress[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [startTime, setStartTime] = useState<number | null>(null);
  const [hasStarted, setHasStarted] = useState(false);
  const [currentStage, setCurrentStage] = useState<string | null>(null);
  const [currentPercent, setCurrentPercent] = useState<number>(0);
  const [operationId, setOperationId] = useState<string | null>(urlOperationId);
  const [canForceRetry, setCanForceRetry] = useState(false);
  const [forceRetryMessage, setForceRetryMessage] = useState<string>('');
  const [recoveredOperation, setRecoveredOperation] = useState<any>(null);

  // Dependent container tracking
  const [expectedDependents, setExpectedDependents] = useState<string[]>([]);
  const [dependentsRestarted, setDependentsRestarted] = useState<string[]>([]);
  const [dependentsBlocked, setDependentsBlocked] = useState<string[]>([]);

  const logEntriesRef = useRef<HTMLDivElement>(null);
  const processedEventsRef = useRef<Set<string>>(new Set());
  const containerToOpIdRef = useRef<Map<string, string>>(new Map());
  const timeoutsRef = useRef<number[]>([]);
  // Track max seen percent per container to prevent progress bar jumping backwards
  const maxPercentRef = useRef<Map<string, number>>(new Map());
  // Track last logged stage per container to reduce log spam
  const lastLoggedStageRef = useRef<Map<string, string>>(new Map());

  // SSE for progress
  const { lastEvent, clearEvents, wasDisconnected, clearWasDisconnected } = useEventStream(status === 'in_progress' || operationId !== null);

  // Track if we saw pending_restart stage (self-update/restart in progress)
  const sawPendingRestartRef = useRef(false);

  // Calculate elapsed time
  const [endTime, setEndTime] = useState<number | null>(null);
  const isRunning = startTime !== null && status === 'in_progress';
  const elapsedTime = useElapsedTime(startTime, isRunning, endTime);

  // Auto-scroll logs
  useEffect(() => {
    if (logEntriesRef.current) {
      logEntriesRef.current.scrollTop = logEntriesRef.current.scrollHeight;
    }
  }, [logs]);

  // Add log entry helper
  const addLog = (message: string, type: LogEntry['type'] = 'info', icon?: string) => {
    setLogs(prev => [...prev, { time: Date.now(), message, type, icon }]);
  };

  // Update a specific container's progress
  const updateContainer = (containerName: string, updates: Partial<ContainerProgress>) => {
    setContainers(prev => prev.map(c =>
      c.name === containerName ? { ...c, ...updates } : c
    ));
  };

  // Update containers that match a predicate
  const updateContainersWhere = (
    predicate: (c: ContainerProgress) => boolean,
    updates: Partial<ContainerProgress> | ((c: ContainerProgress) => Partial<ContainerProgress>)
  ) => {
    setContainers(prev => prev.map(c => {
      if (predicate(c)) {
        const newUpdates = typeof updates === 'function' ? updates(c) : updates;
        return { ...c, ...newUpdates };
      }
      return c;
    }));
  };

  // Clear all timeouts
  const clearTimeouts = () => {
    timeoutsRef.current.forEach(id => clearTimeout(id));
    timeoutsRef.current = [];
  };

  // Check if error is a pre-update check failure
  const isPreCheckFailure = (errorMessage: string): boolean => {
    return errorMessage.includes('pre-update check failed') ||
           errorMessage.includes('failed pre-update check') ||
           errorMessage.includes('script exited with code') ||
           errorMessage.includes('pre-check failed') ||
           errorMessage.includes('Pre-update check') ||
           errorMessage.includes('pre_update_check') ||
           errorMessage.includes('Dependent container');
  };

  // Recovery mode: fetch operation status when we have URL param but no location state
  useEffect(() => {
    if (!isRecoveryMode) return;

    // Batch group recovery: fetch all operations in the group
    if (urlBatchGroupId) {
      const fetchGroupOperations = async () => {
        try {
          addLog('Recovering batch group status...', 'info', 'fa-sync');
          const response = await getOperationsByGroup(urlBatchGroupId);

          if (!response.success || !response.data) {
            addLog('Batch group not found', 'error', 'fa-circle-xmark');
            return;
          }

          const ops = response.data.operations;
          if (ops.length === 0) {
            addLog('No operations found in batch group', 'error', 'fa-circle-xmark');
            return;
          }

          // Find earliest start time
          const startTimes = ops.map(op => op.started_at ? new Date(op.started_at).getTime() : Date.now());
          setStartTime(Math.min(...startTimes));
          setRecoveredOperation(ops[0]);

          // Build container list from all operations' batch_details
          const containerList: ContainerProgress[] = [];
          for (const op of ops) {
            if (op.batch_details && op.batch_details.length > 0) {
              for (const detail of op.batch_details) {
                containerList.push({
                  name: detail.container_name,
                  status: op.status === 'complete' ? 'success' : op.status === 'failed' ? 'failed' : 'in_progress',
                  message: op.status === 'complete' ? 'Completed' : op.status === 'failed' ? (op.error_message || 'Failed') : 'In progress',
                  percent: op.status === 'complete' ? 100 : 50,
                  versionFrom: detail.old_version,
                  versionTo: detail.new_version,
                  operationId: op.operation_id,
                });
                containerToOpIdRef.current.set(detail.container_name, op.operation_id);
              }
            } else {
              containerList.push({
                name: op.container_name || 'Unknown',
                status: op.status === 'complete' ? 'success' : op.status === 'failed' ? 'failed' : 'in_progress',
                message: op.status === 'complete' ? 'Completed' : op.status === 'failed' ? (op.error_message || 'Failed') : 'In progress',
                percent: op.status === 'complete' ? 100 : 50,
                operationId: op.operation_id,
              });
              containerToOpIdRef.current.set(op.container_name, op.operation_id);
            }
          }
          setContainers(containerList);

          // Check overall status
          const allComplete = ops.every(op => op.status === 'complete');
          const anyFailed = ops.some(op => op.status === 'failed');
          const allDone = ops.every(op => op.status === 'complete' || op.status === 'failed');

          if (allComplete) {
            // Use latest completed_at from all operations
            const completedTimes = ops.filter(op => op.completed_at).map(op => new Date(op.completed_at!).getTime());
            setEndTime(completedTimes.length > 0 ? Math.max(...completedTimes) : Date.now());
            setStatus('success');
            addLog('All operations completed successfully', 'success', 'fa-circle-check');
          } else if (allDone && anyFailed) {
            const completedTimes = ops.filter(op => op.completed_at).map(op => new Date(op.completed_at!).getTime());
            setEndTime(completedTimes.length > 0 ? Math.max(...completedTimes) : Date.now());
            setStatus('failed');
            addLog('Some operations failed', 'error', 'fa-circle-xmark');
          } else {
            // Still in progress — poll all operations
            addLog(`Batch update in progress (${ops.length} operations)`, 'info', 'fa-spinner');

            const pollGroupOperations = async () => {
              let attempts = 0;
              const maxAttempts = 120;
              const loggedOps = new Set<string>();

              while (attempts < maxAttempts) {
                await new Promise(resolve => setTimeout(resolve, 2000));
                attempts++;

                try {
                  const pollResponse = await getOperationsByGroup(urlBatchGroupId);
                  if (!pollResponse.success || !pollResponse.data) continue;

                  const pollOps = pollResponse.data.operations;
                  const pollAllDone = pollOps.every(op => op.status === 'complete' || op.status === 'failed');

                  // Update container statuses
                  for (const op of pollOps) {
                    const affectedNames: string[] = [];
                    if (op.batch_details && op.batch_details.length > 0) {
                      for (const d of op.batch_details) affectedNames.push(d.container_name);
                    } else {
                      affectedNames.push(op.container_name);
                    }

                    if (op.status === 'complete') {
                      setContainers(prev => prev.map(c =>
                        affectedNames.includes(c.name) ? { ...c, status: 'success', percent: 100, message: 'Completed' } : c
                      ));
                      if (!loggedOps.has(op.operation_id)) {
                        loggedOps.add(op.operation_id);
                        addLog(`${affectedNames.join(', ')}: Update completed`, 'success', 'fa-circle-check');
                      }
                    } else if (op.status === 'failed') {
                      setContainers(prev => prev.map(c =>
                        affectedNames.includes(c.name) ? { ...c, status: 'failed', message: op.error_message || 'Failed' } : c
                      ));
                      if (!loggedOps.has(op.operation_id)) {
                        loggedOps.add(op.operation_id);
                        addLog(`${affectedNames.join(', ')}: ${op.error_message || 'Update failed'}`, 'error', 'fa-circle-xmark');
                      }
                    }
                  }

                  if (pollAllDone) {
                    const pollAllComplete = pollOps.every(op => op.status === 'complete');
                    setEndTime(Date.now());
                    setStatus(pollAllComplete ? 'success' : 'failed');
                    addLog(pollAllComplete ? 'All operations completed' : 'Some operations failed', pollAllComplete ? 'success' : 'error', pollAllComplete ? 'fa-circle-check' : 'fa-circle-xmark');
                    return;
                  }
                } catch {
                  // Continue polling
                }
              }
              addLog('Timed out waiting for batch completion', 'error', 'fa-clock');
              setStatus('failed');
            };
            pollGroupOperations();
          }
        } catch (err) {
          const errMsg = err instanceof Error ? err.message : 'Unknown error';
          addLog(`Failed to recover batch group: ${errMsg}`, 'error', 'fa-circle-xmark');
        }
      };

      fetchGroupOperations();
      return;
    }

    // Single operation recovery
    if (!urlOperationId) return;

    const fetchOperation = async () => {
      try {
        addLog('Recovering operation status...', 'info', 'fa-sync');
        const response = await fetch(`/api/operations/${urlOperationId}`);
        if (!response.ok) {
          addLog(`Failed to fetch operation: ${response.status}`, 'error', 'fa-circle-xmark');
          return;
        }

        const data = await response.json();
        if (!data.success || !data.data) {
          addLog('Operation not found', 'error', 'fa-circle-xmark');
          return;
        }

        const op = data.data;
        setRecoveredOperation(op);
        setStartTime(op.started_at ? new Date(op.started_at).getTime() : Date.now());

        // Initialize container list from recovered operation
        const containerName = op.container_name || 'Unknown';
        setContainers([{
          name: containerName,
          status: op.status === 'complete' ? 'success' : op.status === 'failed' ? 'failed' : 'in_progress',
          message: op.error_message || op.status,
          percent: op.status === 'complete' ? 100 : 50,
        }]);

        // Update status based on operation status
        if (op.status === 'complete') {
          setEndTime(op.completed_at ? new Date(op.completed_at).getTime() : Date.now());
          setStatus('success');
          addLog(op.error_message || 'Operation completed successfully', 'success', 'fa-circle-check');
        } else if (op.status === 'failed') {
          setEndTime(op.completed_at ? new Date(op.completed_at).getTime() : Date.now());
          setStatus('failed');
          addLog(op.error_message || 'Operation failed', 'error', 'fa-circle-xmark');
        } else if (op.status === 'pending_restart') {
          // Self-restart in progress - start polling
          addLog('Self-restart in progress, waiting for completion...', 'info', 'fa-rotate');
          sawPendingRestartRef.current = true;

          // Start polling for completion
          const pollForCompletion = async () => {
            let attempts = 0;
            const maxAttempts = 60;
            const pollInterval = 2000;

            while (attempts < maxAttempts) {
              attempts++;
              await new Promise(resolve => setTimeout(resolve, pollInterval));

              try {
                const pollResponse = await fetch(`/api/operations/${urlOperationId}`);
                if (!pollResponse.ok) continue;

                const pollData = await pollResponse.json();
                if (!pollData.success || !pollData.data) continue;

                const pollOp = pollData.data;
                if (pollOp.status === 'complete') {
                  setContainers(prev => prev.map(c => ({
                    ...c,
                    status: 'success',
                    percent: 100,
                    message: pollOp.error_message || 'Completed successfully'
                  })));
                  setStatus('success');
                  addLog(pollOp.error_message || 'Self-restart completed successfully', 'success', 'fa-circle-check');
                  return;
                } else if (pollOp.status === 'failed') {
                  setContainers(prev => prev.map(c => ({
                    ...c,
                    status: 'failed',
                    message: pollOp.error_message || 'Failed'
                  })));
                  setStatus('failed');
                  addLog(pollOp.error_message || 'Operation failed', 'error', 'fa-circle-xmark');
                  return;
                }
              } catch {
                // Network error - keep polling
              }
            }
            addLog('Timed out waiting for completion', 'error', 'fa-clock');
            setStatus('failed');
          };
          pollForCompletion();
        } else {
          // in_progress - just show current state
          addLog(`Operation in progress: ${op.status}`, 'info', 'fa-spinner');
        }
      } catch (err) {
        const errMsg = err instanceof Error ? err.message : 'Unknown error';
        addLog(`Failed to recover operation: ${errMsg}`, 'error', 'fa-circle-xmark');
      }
    };

    fetchOperation();
  }, [isRecoveryMode, urlOperationId, urlBatchGroupId]);

  // Handle reconnection after self-restart: poll operation status to see if it completed
  useEffect(() => {
    if (!wasDisconnected || !sawPendingRestartRef.current || !operationId || status !== 'in_progress') {
      return;
    }

    // Clear the wasDisconnected flag so we don't keep polling
    clearWasDisconnected();

    // Poll the operation status
    const checkOperationStatus = async () => {
      addLog('Checking operation status after reconnect...', 'info', 'fa-wifi');

      try {
        const response = await fetch(`/api/operations/${operationId}`);
        if (!response.ok) {
          addLog(`API error: ${response.status}`, 'error', 'fa-circle-xmark');
          return;
        }

        const data = await response.json();

        if (!data.success) {
          addLog(`API returned error: ${data.error || 'unknown'}`, 'error', 'fa-circle-xmark');
          return;
        }

        if (!data.data) {
          addLog('No operation data returned', 'error', 'fa-circle-xmark');
          return;
        }

        const op = data.data;
        addLog(`Operation status: ${op.status}`, 'info', 'fa-info-circle');

        if (op.status === 'complete') {
          // Operation completed! Update UI
          const containerName = operationType === 'restart' && operationInfo?.type === 'restart'
            ? operationInfo.containerName
            : op.container_name;

          if (containerName) {
            setContainers(prev => prev.map(c =>
              c.name === containerName
                ? { ...c, status: 'success', percent: 100, message: op.error_message || 'Completed successfully' }
                : c
            ));
          }

          setStatus('success');
          addLog(op.error_message || 'Operation completed successfully', 'success', 'fa-circle-check');
          sawPendingRestartRef.current = false;
        } else if (op.status === 'failed') {
          const containerName = operationType === 'restart' && operationInfo?.type === 'restart'
            ? operationInfo.containerName
            : op.container_name;

          if (containerName) {
            setContainers(prev => prev.map(c =>
              c.name === containerName
                ? { ...c, status: 'failed', message: op.error_message || 'Operation failed' }
                : c
            ));
          }

          setStatus('failed');
          addLog(op.error_message || 'Operation failed', 'error', 'fa-circle-xmark');
          sawPendingRestartRef.current = false;
        } else {
          // Still in progress - wait for SSE events
          addLog(`Waiting for completion (current: ${op.status})...`, 'info', 'fa-spinner');
        }
      } catch (err) {
        const errMsg = err instanceof Error ? err.message : 'Unknown error';
        addLog(`Failed to check status: ${errMsg}`, 'error', 'fa-circle-xmark');
      }
    };

    // Small delay to ensure backend is fully ready
    const timeout = setTimeout(checkOperationStatus, 1500);
    return () => clearTimeout(timeout);
  }, [wasDisconnected, operationId, status, operationType, operationInfo, clearWasDisconnected]);

  // Handle SSE progress events
  useEffect(() => {
    if (!lastEvent || status !== 'in_progress') return;

    const event = lastEvent as UpdateProgressEvent;
    // Backend sends 'progress', not 'percent' - use the correct field for deduplication
    // Include container_name to handle batch updates where multiple containers have same progress
    const eventKey = `${event.operation_id}-${event.container_name || ''}-${event.stage}-${event.progress || event.percent}`;

    // Skip duplicate events
    if (processedEventsRef.current.has(eventKey)) return;
    processedEventsRef.current.add(eventKey);

    // Check if this event is for our operation
    let isOurOperation = false;
    let targetContainer: string | null = null;

    if (operationId && event.operation_id === operationId) {
      isOurOperation = true;
      if (operationType === 'restart' && operationInfo?.type === 'restart') {
        targetContainer = operationInfo.containerName;
      } else if (operationType === 'rollback' && operationInfo?.type === 'rollback') {
        targetContainer = operationInfo.containerName;
      }
    }

    // For update mode, check container mapping
    if (operationType === 'update') {
      for (const [containerName, opId] of containerToOpIdRef.current) {
        if (opId === event.operation_id || event.container_name === containerName) {
          targetContainer = containerName;
          isOurOperation = true;
          break;
        }
      }
      if (!targetContainer && event.container_name) {
        for (const c of containers) {
          if (c.name === event.container_name) {
            targetContainer = c.name;
            isOurOperation = true;
            break;
          }
        }
      }
    }

    if (!isOurOperation) return;

    const eventPercent = event.percent || event.progress || 0;
    const containerKey = targetContainer || '__global__';

    // Get the max percent seen so far for this container
    const currentMaxPercent = maxPercentRef.current.get(containerKey) || 0;

    // Only update progress if it's higher (prevents jumping backwards during layer-by-layer pulls)
    // Exception: reset is allowed when stage changes to a new phase (e.g., pulling -> recreating)
    const lastStage = lastLoggedStageRef.current.get(containerKey);
    const isNewStage = lastStage !== event.stage;
    const effectivePercent = isNewStage ? eventPercent : Math.max(currentMaxPercent, eventPercent);

    // Update max percent tracker
    if (effectivePercent > currentMaxPercent || isNewStage) {
      maxPercentRef.current.set(containerKey, effectivePercent);
    }

    // Update current stage display (only increase percent, never decrease)
    setCurrentStage(event.stage);
    setCurrentPercent(prev => Math.max(prev, effectivePercent));

    // Handle pending_restart stage (self-update)
    if (event.stage === 'pending_restart') {
      // Track that we saw pending_restart - used to poll after reconnection
      sawPendingRestartRef.current = true;

      // Docksmith is about to restart - show special UI
      if (targetContainer) {
        updateContainer(targetContainer, {
          status: 'in_progress',
          stage: 'pending_restart',
          percent: 90,
          message: 'Restarting to apply update...',
        });
      }
      addLog('Docksmith is restarting to apply the update...', 'info', 'fa-rotate');
      addLog('Will check for completion automatically...', 'info', 'fa-wifi');

      // Start polling immediately - don't rely on SSE reconnection
      const pollForCompletion = async () => {
        const opId = operationId || event.operation_id;
        if (!opId) return;

        let attempts = 0;
        const maxAttempts = 60; // 2 minutes max
        const pollInterval = 2000; // 2 seconds

        while (attempts < maxAttempts) {
          attempts++;
          await new Promise(resolve => setTimeout(resolve, pollInterval));

          try {
            const response = await fetch(`/api/operations/${opId}`);
            if (!response.ok) continue;

            const data = await response.json();
            if (!data.success || !data.data) continue;

            const op = data.data;

            if (op.status === 'complete') {
              // Operation completed!
              const cName = targetContainer || op.container_name;
              if (cName) {
                setContainers(prev => prev.map(c =>
                  c.name === cName
                    ? { ...c, status: 'success', percent: 100, message: op.error_message || 'Completed successfully' }
                    : c
                ));
              }
              setStatus('success');
              addLog(op.error_message || 'Self-restart completed successfully', 'success', 'fa-circle-check');
              sawPendingRestartRef.current = false;
              return;
            } else if (op.status === 'failed') {
              const cName = targetContainer || op.container_name;
              if (cName) {
                setContainers(prev => prev.map(c =>
                  c.name === cName
                    ? { ...c, status: 'failed', message: op.error_message || 'Failed' }
                    : c
                ));
              }
              setStatus('failed');
              addLog(op.error_message || 'Self-restart failed', 'error', 'fa-circle-xmark');
              sawPendingRestartRef.current = false;
              return;
            }
            // Still pending_restart or in_progress, keep polling
          } catch {
            // Network error - server probably still restarting, keep polling
          }
        }

        // Timeout
        addLog('Timed out waiting for restart completion', 'error', 'fa-clock');
        setStatus('failed');
      };

      pollForCompletion();
      return;
    }

    // Update container progress
    if (targetContainer) {
      const stageInfo = STAGE_INFO[event.stage] || RESTART_STAGES[event.stage];
      // For failed stage, use brief message for precheck failures, full error for others
      // For other stages, use stage description as fallback
      let message: string | undefined;
      if (event.stage === 'failed') {
        message = isPreCheckFailure(event.message || '') ? 'Pre-update check failed' : event.message;
      } else if (event.stage === 'complete') {
        message = event.message;
      } else if (event.stage === 'pending_restart') {
        message = 'Restarting to apply update...';
      } else {
        message = stageInfo?.description || event.message;
      }
      // Only update percent if it's higher than current
      updateContainer(targetContainer, {
        status: event.stage === 'complete' ? 'success' : event.stage === 'failed' ? 'failed' : 'in_progress',
        stage: event.stage,
        percent: effectivePercent,
        message,
      });
    }

    // Handle restarting_dependents stage
    if (event.stage === 'restarting_dependents') {
      // Parse dependent info from message if available
      const message = event.message || '';

      // Check for blocked dependents (message format: "Blocked dependents: name1, name2")
      if (message.includes('Blocked dependents:')) {
        const blockedPart = message.split('Blocked dependents:')[1];
        if (blockedPart) {
          const names = blockedPart.split(',').map(n => n.trim()).filter(n => n);
          for (const depName of names) {
            if (!dependentsBlocked.includes(depName)) {
              setDependentsBlocked(prev => [...prev, depName]);
            }
          }
          // Log the blocked dependents
          addLog(`Blocked: ${names.join(', ')} (pre-update check failed)`, 'warning', 'fa-ban');
        }
      }
      // Check for warning/summary messages about blocked dependents
      else if (message.includes('Warning:') || message.toLowerCase().includes('blocked')) {
        // Log the warning summary message
        addLog(message, 'warning', 'fa-triangle-exclamation');
      }
      // Check for restarted dependents (message format: "Restarted dependents: name1, name2")
      else if (message.includes('Restarted dependents:') || message.includes('Dependents restarted:')) {
        const restartedPart = message.includes('Restarted dependents:')
          ? message.split('Restarted dependents:')[1]
          : message.split('Dependents restarted:')[1];
        if (restartedPart) {
          const names = restartedPart.split(',').map(n => n.trim()).filter(n => n);
          for (const depName of names) {
            if (!dependentsRestarted.includes(depName)) {
              setDependentsRestarted(prev => [...prev, depName]);
            }
          }
          // Log the restarted dependents
          addLog(`Restarted dependents: ${names.join(', ')}`, 'success', 'fa-rotate');
        }
      }
    }

    // Add stage transition log - only log when stage changes to reduce spam
    const stageInfo = STAGE_INFO[event.stage] || RESTART_STAGES[event.stage];
    if (stageInfo && event.stage !== 'complete' && event.stage !== 'failed' && isNewStage) {
      const prefix = targetContainer && operationType === 'update' ? `${targetContainer}: ` : '';
      addLog(`${prefix}${stageInfo.label}`, 'stage', stageInfo.icon);
      // Track that we've logged this stage
      lastLoggedStageRef.current.set(containerKey, event.stage);
    }

    // Handle completion
    if (event.stage === 'complete') {
      if (targetContainer) {
        updateContainersWhere(
          c => c.name === targetContainer && c.status !== 'success',
          { status: 'success', message: 'Completed successfully', percent: 100 }
        );
      }

      // For single container ops (restart/rollback), complete immediately
      if (operationType !== 'update') {
        setEndTime(Date.now());
        setStatus('success');
        const successMsg = operationType === 'rollback' ? 'Rollback completed successfully' : 'Container restarted successfully';
        addLog(successMsg, 'success', 'fa-circle-check');
      } else {
        addLog(`${targetContainer}: Completed successfully`, 'success', 'fa-circle-check');
      }
    } else if (event.stage === 'failed') {
      const errorMessage = event.message || 'Operation failed';
      // For precheck failures, show brief message in container list, full error only in logs
      const briefMessage = isPreCheckFailure(errorMessage) ? 'Pre-update check failed' : errorMessage;

      if (targetContainer) {
        updateContainersWhere(
          c => c.name === targetContainer && c.status !== 'failed',
          { status: 'failed', message: briefMessage }
        );
      }

      // For single container ops, fail immediately
      if (operationType !== 'update') {
        setEndTime(Date.now());
        setStatus('failed');
        addLog(`Error: ${errorMessage}`, 'error', 'fa-circle-xmark');

        if (isPreCheckFailure(errorMessage)) {
          setCanForceRetry(true);
          setForceRetryMessage('You can force the operation to bypass the pre-update check');
          addLog('You can force the operation to bypass the pre-update check', 'info', 'fa-info-circle');
        }
      } else {
        addLog(`${targetContainer}: ${errorMessage}`, 'error', 'fa-circle-xmark');
      }
    }
  }, [lastEvent, status, operationId, operationType, operationInfo, containers, dependentsRestarted, dependentsBlocked]);

  // Start the operation
  useEffect(() => {
    if (hasStarted || !operationInfo) return;

    setHasStarted(true);
    setStartTime(Date.now());
    clearEvents();
    processedEventsRef.current.clear();
    maxPercentRef.current.clear();
    lastLoggedStageRef.current.clear();

    // Clear location state to prevent re-triggering on page refresh
    // Replace current history entry with empty state
    navigate(location.pathname, { replace: true, state: null });

    switch (operationInfo.type) {
      case 'restart':
        runRestart(operationInfo);
        break;
      case 'update':
        runUpdate(operationInfo);
        break;
      case 'rollback':
        runRollback(operationInfo);
        break;
      case 'start':
        runStart(operationInfo);
        break;
      case 'stop':
        runStop(operationInfo);
        break;
      case 'remove':
        runRemove(operationInfo);
        break;
      case 'stackRestart':
        runStackRestart(operationInfo);
        break;
      case 'stackStop':
        runStackStop(operationInfo);
        break;
      case 'fixMismatch':
        runFixMismatch(operationInfo);
        break;
      case 'batchFixMismatch':
        runBatchFixMismatch(operationInfo as BatchFixMismatchOperation);
        break;
      case 'mixed':
        runMixed(operationInfo as MixedOperation);
        break;
      case 'batchLabel':
        runBatchLabel(operationInfo as BatchLabelOperation);
        break;
      case 'batchStart':
        runBatchStart(operationInfo as BatchStartOperation);
        break;
      case 'batchStop':
        runBatchStop(operationInfo as BatchStopOperation);
        break;
      case 'batchRestart':
        runBatchRestart(operationInfo as BatchRestartOperation);
        break;
      case 'batchRemove':
        runBatchRemove(operationInfo as BatchRemoveOperation);
        break;
      case 'labelRollback':
        runLabelRollback(operationInfo as LabelRollbackOperation);
        break;
    }

    return () => {
      clearTimeouts();
    };
  }, [operationInfo, hasStarted]);

  // Helper to describe label changes
  const describeChanges = (changes: RestartOperation['labelChanges']): string[] => {
    if (!changes) return [];
    const descriptions: string[] = [];

    if (changes.ignore !== undefined) {
      descriptions.push(changes.ignore ? 'Enable ignore' : 'Disable ignore');
    }
    if (changes.allow_latest !== undefined) {
      descriptions.push(changes.allow_latest ? 'Allow :latest tag' : 'Disallow :latest tag');
    }
    if (changes.version_pin_major !== undefined || changes.version_pin_minor !== undefined || changes.version_pin_patch !== undefined) {
      if (changes.version_pin_major) {
        descriptions.push('Pin to major version');
      } else if (changes.version_pin_minor) {
        descriptions.push('Pin to minor version');
      } else if (changes.version_pin_patch) {
        descriptions.push('Pin to patch version');
      } else {
        descriptions.push('Remove version pin');
      }
    }
    if (changes.tag_regex !== undefined) {
      descriptions.push(changes.tag_regex ? `Set tag filter: ${changes.tag_regex}` : 'Remove tag filter');
    }
    if (changes.script !== undefined) {
      descriptions.push(changes.script ? `Set script: ${changes.script.split('/').pop()}` : 'Remove pre-update script');
    }
    if (changes.restart_after !== undefined) {
      descriptions.push(changes.restart_after ? `Set restart deps: ${changes.restart_after}` : 'Remove restart dependencies');
    }

    return descriptions;
  };

  // Run restart operation
  const runRestart = async (info: RestartOperation) => {
    const { containerName, force, saveSettings, labelChanges } = info;

    // Initialize container list
    setContainers([{
      name: containerName,
      status: 'in_progress',
      badge: force ? 'Force' : undefined,
    }]);

    try {
      // Save settings flow - API starts background operation and returns operation ID
      // We track progress via SSE (same pattern as regular updates)
      if (saveSettings && labelChanges) {
        setCurrentStage('saving');
        const changes = describeChanges(labelChanges);
        if (changes.length > 0) {
          addLog(`Applying ${changes.length} change(s) to ${containerName}:`, 'stage', 'fa-floppy-disk');
          changes.forEach(change => {
            addLog(`  • ${change}`, 'info');
          });
        } else {
          addLog(`Saving settings for ${containerName}...`, 'stage', 'fa-floppy-disk');
        }

        // Call API - returns immediately with operation ID, work runs in background
        const saveResponse = await setLabelsAPI(containerName, {
          ...labelChanges,
          force,
        });

        if (!saveResponse.success) {
          throw new Error(saveResponse.error || 'Failed to save settings');
        }

        // Get operation ID and track via SSE (same as pure restart flow)
        const opId = saveResponse.data?.operation_id;
        if (opId) {
          setOperationId(opId);
          setSearchParams({ id: opId }, { replace: true });
          addLog(`Operation started: ${opId.substring(0, 8)}...`, 'info', 'fa-play');
        }

        // SSE will handle progress updates - no fake delays needed
        return;
      }

      // Pure restart flow - use SSE-based restart
      const action = force ? 'Force restarting' : 'Restarting';
      addLog(`${action} ${containerName}...`, 'stage', 'fa-rotate');
      setCurrentStage('stopping');

      const response = await startRestart(containerName, force);

      if (!response.success || !response.data?.operation_id) {
        throw new Error(response.error || 'Failed to start restart operation');
      }

      setOperationId(response.data.operation_id);
      // Add operation ID to URL for recovery after page refresh
      setSearchParams({ id: response.data.operation_id }, { replace: true });
      addLog(`Operation started: ${response.data.operation_id.substring(0, 8)}...`, 'info', 'fa-play');

      // Fetch operation details to get expected dependents
      try {
        const opResponse = await fetch(`/api/operations/${response.data.operation_id}`);
        const opData = await opResponse.json();
        if (opData.success && opData.data?.dependents_affected) {
          setExpectedDependents(opData.data.dependents_affected);
          if (opData.data.dependents_affected.length > 0) {
            addLog(`Found ${opData.data.dependents_affected.length} dependent container(s)`, 'info', 'fa-link');
          }
        }
      } catch {
        // Ignore errors fetching operation details
      }

    } catch (err) {
      clearTimeouts();
      const errorMessage = err instanceof Error ? err.message : 'Operation failed';
      // For precheck failures, show brief message in container list, full error only in logs
      const briefMessage = isPreCheckFailure(errorMessage) ? 'Pre-update check failed' : errorMessage;
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: briefMessage });
      addLog(`Error: ${errorMessage}`, 'error', 'fa-circle-xmark');

      if (isPreCheckFailure(errorMessage)) {
        setCanForceRetry(true);
        setForceRetryMessage('You can force restart to bypass the pre-update check');
        addLog('You can force the operation to bypass the pre-update check', 'info', 'fa-info-circle');
      }
    }
  };

  // Run update operation
  const runUpdate = async (info: UpdateOperation) => {
    // Initialize container progress
    setContainers(info.containers.map(c => ({
      name: c.name,
      status: 'pending' as const,
      percent: 0,
      badge: c.force ? 'Force' : undefined,
      versionTo: c.target_version,
    })));

    addLog(`Starting update of ${info.containers.length} container(s)`, 'info', 'fa-rocket');

    // Group by stack for logging
    const stackGroups = new Map<string, string[]>();
    for (const c of info.containers) {
      const stack = c.stack || '__standalone__';
      if (!stackGroups.has(stack)) {
        stackGroups.set(stack, []);
      }
      stackGroups.get(stack)!.push(c.name);
    }

    for (const [stack, containerNames] of stackGroups) {
      const stackName = stack === '__standalone__' ? 'Standalone' : stack;
      addLog(`${stackName}: ${containerNames.join(', ')}`, 'info', 'fa-layer-group');
    }

    try {
      const response = await triggerBatchUpdate(info.containers);

      if (!response.success) {
        updateContainersWhere(
          () => true,
          { status: 'failed', message: 'Batch update failed', error: response.error }
        );
        addLog(`Batch update failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      // Save batch group ID to URL for refresh recovery
      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      // Track operation IDs
      const operations = response.data?.operations || [];

      for (const op of operations) {
        if (op.status === 'started' && op.operation_id) {
          for (const containerName of op.containers) {
            containerToOpIdRef.current.set(containerName, op.operation_id);
          }
          updateContainersWhere(
            c => op.containers.includes(c.name),
            { status: 'in_progress', message: 'Initializing update...', operationId: op.operation_id }
          );
          addLog(`Operation started for ${op.containers.join(', ')}`, 'info', 'fa-play');
        } else if (op.status === 'failed') {
          updateContainersWhere(
            c => op.containers.includes(c.name),
            { status: 'failed', message: 'Failed to start', error: op.error }
          );
          addLog(`Failed to start: ${op.error}`, 'error', 'fa-circle-xmark');
        }
      }

      // Poll for completion
      const uniqueOpIds = new Set<string>();
      for (const opId of containerToOpIdRef.current.values()) {
        uniqueOpIds.add(opId);
      }

      const pollOperation = async (opId: string) => {
        let completed = false;
        let pollCount = 0;
        const maxPolls = 120;

        while (!completed && pollCount < maxPolls) {
          await new Promise(resolve => setTimeout(resolve, 2000));
          pollCount++;

          try {
            const opResponse = await fetch(`/api/operations/${opId}`);
            const opData = await opResponse.json();

            if (opData.success && opData.data) {
              const op = opData.data;
              const affectedContainers: string[] = [];
              for (const [containerName, operationId] of containerToOpIdRef.current) {
                if (operationId === opId) {
                  affectedContainers.push(containerName);
                }
              }

              if (op.status === 'complete') {
                completed = true;
                updateContainersWhere(
                  c => affectedContainers.includes(c.name) && c.status !== 'success',
                  { status: 'success', message: 'Updated successfully', percent: 100 }
                );
                addLog(`${affectedContainers.join(', ')}: Update completed`, 'success', 'fa-circle-check');
              } else if (op.status === 'failed') {
                completed = true;
                updateContainersWhere(
                  c => affectedContainers.includes(c.name) && c.status !== 'failed',
                  { status: 'failed', message: 'Update failed', error: op.error_message }
                );
                addLog(`${affectedContainers.join(', ')}: ${op.error_message || 'Update failed'}`, 'error', 'fa-circle-xmark');
              }
            }
          } catch {
            // Continue polling on error
          }
        }

        if (!completed) {
          const affectedContainers: string[] = [];
          for (const [containerName, operationId] of containerToOpIdRef.current) {
            if (operationId === opId) {
              affectedContainers.push(containerName);
            }
          }
          updateContainersWhere(
            c => affectedContainers.includes(c.name) && c.status === 'in_progress',
            { status: 'failed', message: 'Timed out waiting for completion' }
          );
          addLog(`Operation timed out`, 'error', 'fa-clock');
        }
      };

      await Promise.all(Array.from(uniqueOpIds).map(opId => pollOperation(opId)));

    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(
        () => true,
        { status: 'failed', message: 'Error', error: errorMsg }
      );
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
      return;
    }

    // Determine final status based on individual container results
    setEndTime(Date.now());
    setContainers(prev => {
      const failedCount = prev.filter(c => c.status === 'failed').length;
      const successCount = prev.filter(c => c.status === 'success').length;

      if (failedCount > 0 && successCount === 0) {
        addLog(`Update failed: all ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Update completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success'); // Partial success — individual failures visible in UI
      } else {
        addLog(`Update completed: ${successCount} container(s) updated successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }

      return prev;
    });
  };

  // Run rollback operation
  const runRollback = async (info: RollbackOperation) => {
    const { operationId: rollbackOpId, containerName, oldVersion, newVersion, force } = info;

    // Initialize container list
    setContainers([{
      name: containerName,
      status: 'in_progress',
      badge: 'Rollback',
      versionFrom: newVersion,
      versionTo: oldVersion,
    }]);

    addLog(`Starting rollback of ${containerName}...`, 'info', 'fa-rotate-left');
    if (newVersion && oldVersion) {
      addLog(`Rolling back from ${newVersion} to ${oldVersion}`, 'info', 'fa-code-compare');
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
        setOperationId(newOpId);
        if (newOpId) {
          // Add operation ID to URL for recovery after page refresh
          setSearchParams({ id: newOpId }, { replace: true });
        }
        addLog(`Rollback operation started`, 'info', 'fa-play');

        if (newOpId) {
          // Poll for completion as fallback to SSE
          let completed = false;
          let pollCount = 0;
          const maxPolls = 60;

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 2000));
            pollCount++;

            try {
              const opResponse = await fetch(`/api/operations/${newOpId}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;

                if (op.status === 'complete') {
                  completed = true;
                  setStatus('success');
                  updateContainer(containerName, { status: 'success', message: 'Rolled back successfully' });
                  addLog('Rollback completed successfully', 'success', 'fa-circle-check');
                } else if (op.status === 'failed') {
                  completed = true;
                  setStatus('failed');
                  updateContainer(containerName, { status: 'failed', message: op.error_message, error: op.error_message });
                  addLog(op.error_message || 'Rollback failed', 'error', 'fa-circle-xmark');
                }
              }
            } catch {
              // Continue polling on error
            }
          }

          if (!completed) {
            setStatus('failed');
            updateContainer(containerName, { status: 'failed', message: 'Timed out waiting for completion' });
            addLog('Timed out waiting for completion', 'error', 'fa-clock');
          }
        } else {
          setStatus('success');
          updateContainer(containerName, { status: 'success', message: 'Rollback initiated successfully' });
          addLog('Rollback initiated successfully', 'success', 'fa-circle-check');
        }
      } else {
        setStatus('failed');
        updateContainer(containerName, { status: 'failed', message: data.error, error: data.error });
        addLog(data.error || 'Failed to trigger rollback', 'error', 'fa-circle-xmark');

        if (data.error && isPreCheckFailure(data.error)) {
          setCanForceRetry(true);
          setForceRetryMessage('You can force rollback to bypass the pre-update check');
          addLog('You can force rollback to bypass the pre-update check', 'info', 'fa-info-circle');
        }
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run start operation
  const runStart = async (info: StartOperation) => {
    const { containerName } = info;

    setContainers([{
      name: containerName,
      status: 'in_progress',
      message: 'Starting container...',
    }]);

    addLog(`Starting ${containerName}...`, 'info', 'fa-play');

    try {
      const response = await startContainer(containerName);

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          setOperationId(opId);
          setSearchParams({ id: opId }, { replace: true });
        }
        setStatus('success');
        updateContainer(containerName, { status: 'success', message: 'Container started successfully' });
        addLog('Container started successfully', 'success', 'fa-circle-check');
      } else {
        setStatus('failed');
        updateContainer(containerName, { status: 'failed', message: response.error, error: response.error });
        addLog(response.error || 'Failed to start container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to start container';
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run stop operation
  const runStop = async (info: StopOperation) => {
    const { containerName } = info;

    setContainers([{
      name: containerName,
      status: 'in_progress',
      message: 'Stopping container...',
    }]);

    addLog(`Stopping ${containerName}...`, 'info', 'fa-stop');

    try {
      const response = await stopContainer(containerName);

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          setOperationId(opId);
          setSearchParams({ id: opId }, { replace: true });
        }
        setStatus('success');
        updateContainer(containerName, { status: 'success', message: 'Container stopped successfully' });
        addLog('Container stopped successfully', 'success', 'fa-circle-check');
      } else {
        setStatus('failed');
        updateContainer(containerName, { status: 'failed', message: response.error, error: response.error });
        addLog(response.error || 'Failed to stop container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to stop container';
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run remove operation
  const runRemove = async (info: RemoveOperation) => {
    const { containerName, force } = info;

    setContainers([{
      name: containerName,
      status: 'in_progress',
      message: 'Removing container...',
    }]);

    addLog(`Removing ${containerName}${force ? ' (force)' : ''}...`, 'info', 'fa-trash');

    try {
      const response = await removeContainer(containerName, { force });

      if (response.success) {
        const opId = response.data?.operation_id;
        if (opId) {
          setOperationId(opId);
          setSearchParams({ id: opId }, { replace: true });
        }
        setStatus('success');
        updateContainer(containerName, { status: 'success', message: 'Container removed successfully' });
        addLog('Container removed successfully', 'success', 'fa-circle-check');
      } else {
        setStatus('failed');
        updateContainer(containerName, { status: 'failed', message: response.error, error: response.error });
        addLog(response.error || 'Failed to remove container', 'error', 'fa-circle-xmark');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to remove container';
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run stack restart operation (restart multiple containers in a stack)
  const runStackRestart = async (info: StackRestartOperation) => {
    const { stackName, containers: containerNames, force } = info;

    // Initialize container list
    setContainers(containerNames.map(name => ({
      name,
      status: 'pending' as const,
      percent: 0,
      badge: force ? 'Force' : undefined,
    })));

    addLog(`Restarting ${containerNames.length} container(s) in stack "${stackName}"`, 'info', 'fa-layer-group');
    if (force) {
      addLog('Force mode enabled - pre-update checks will be skipped', 'info', 'fa-forward');
    }

    // Call single backend endpoint for the entire stack restart
    try {
      const response = await startStackRestart(stackName, containerNames, force);

      if (!response.success || !response.data?.operation_id) {
        const errorMsg = response.error || 'Failed to start stack restart';
        setStatus('failed');
        addLog(errorMsg, 'error', 'fa-circle-xmark');

        if (isPreCheckFailure(errorMsg)) {
          setCanForceRetry(true);
          setForceRetryMessage('You can force restart to bypass pre-update checks');
        }
        return;
      }

      const opId = response.data.operation_id;
      setOperationId(opId);
      setSearchParams({ id: opId }, { replace: true });
      addLog('Stack restart operation started', 'info', 'fa-play');

      // Poll the single operation for progress
      let completed = false;
      let pollCount = 0;
      const maxPolls = 180; // 6 minutes for entire stack

      while (!completed && pollCount < maxPolls) {
        await new Promise(resolve => setTimeout(resolve, 2000));
        pollCount++;

        try {
          const opResponse = await fetch(`/api/operations/${opId}`);
          const opData = await opResponse.json();
          if (!opData.success || !opData.data) continue;

          const op = opData.data;

          // Update per-container status from batch_details
          if (op.batch_details) {
            for (const detail of op.batch_details) {
              updateContainer(detail.container_name, {
                status: detail.status === 'complete' ? 'success'
                      : detail.status === 'failed' ? 'failed'
                      : detail.status === 'restarting' ? 'in_progress'
                      : 'pending',
                message: detail.message || '',
              });
            }
          }

          if (op.status === 'complete') {
            completed = true;
            const failedContainers = op.batch_details?.filter((d: any) => d.status === 'failed') || [];
            const successContainers = op.batch_details?.filter((d: any) => d.status === 'complete') || [];

            if (failedContainers.length > 0 && successContainers.length > 0) {
              addLog(`Stack restart completed with issues: ${successContainers.length} succeeded, ${failedContainers.length} failed`, 'warning', 'fa-triangle-exclamation');
              setStatus('success');
            } else if (failedContainers.length > 0) {
              addLog(`Stack restart failed: all containers failed`, 'error', 'fa-circle-xmark');
              setStatus('failed');
            } else {
              addLog(`Stack restart completed: ${successContainers.length} container(s) restarted successfully`, 'success', 'fa-circle-check');
              setStatus('success');
            }
          } else if (op.status === 'failed') {
            completed = true;
            const errorMsg = op.error_message || 'Stack restart failed';
            setStatus('failed');
            addLog(errorMsg, 'error', 'fa-circle-xmark');

            if (isPreCheckFailure(errorMsg)) {
              setCanForceRetry(true);
              setForceRetryMessage('You can force restart to bypass pre-update checks');
            }
          }
        } catch {
          // Continue polling on error
        }
      }

      if (!completed) {
        setStatus('failed');
        addLog('Timed out waiting for stack restart', 'error', 'fa-clock');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      setStatus('failed');
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run stack stop operation (stop multiple containers in a stack)
  const runStackStop = async (info: StackStopOperation) => {
    const { stackName, containers: containerNames } = info;

    // Initialize container list
    setContainers(containerNames.map(name => ({
      name,
      status: 'pending' as const,
      percent: 0,
    })));

    addLog(`Stopping ${containerNames.length} container(s) in stack "${stackName}"`, 'info', 'fa-layer-group');

    let hasAnyFailure = false;
    let successCount = 0;
    let failedCount = 0;

    // Stop each container sequentially
    for (const containerName of containerNames) {
      updateContainer(containerName, { status: 'in_progress', message: 'Stopping...' });
      addLog(`Stopping ${containerName}...`, 'stage', 'fa-stop');

      try {
        const response = await stopContainer(containerName);

        if (response.success) {
          updateContainer(containerName, { status: 'success', message: 'Stopped successfully', percent: 100 });
          addLog(`${containerName}: Stopped successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          const errorMsg = response.error || 'Failed to stop container';
          updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
          addLog(`${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
          hasAnyFailure = true;
          failedCount++;
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
        addLog(`${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
        hasAnyFailure = true;
        failedCount++;
      }
    }

    // Final summary
    if (hasAnyFailure) {
      if (successCount > 0) {
        addLog(`Stack stop completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success'); // Partial success
      } else {
        addLog(`Stack stop failed: all ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      }
    } else {
      addLog(`Stack stop completed: ${successCount} container(s) stopped successfully`, 'success', 'fa-circle-check');
      setStatus('success');
    }
  };

  // Describe label changes for display
  const describeLabelOp = (labelOp: BatchLabelOperation['labelOp']): string => {
    if (labelOp.ignore === true) return 'Ignore';
    if (labelOp.ignore === false) return 'Unignore';
    if (labelOp.allow_latest === true) return 'Allow :latest';
    if (labelOp.allow_latest === false) return 'Disallow :latest';
    if (labelOp.version_pin_major) return 'Pin Major';
    if (labelOp.version_pin_minor) return 'Pin Minor';
    if (labelOp.version_pin_patch) return 'Pin Patch';
    if (labelOp.version_pin_major === false && labelOp.version_pin_minor === false && labelOp.version_pin_patch === false) return 'Unpin';
    if (labelOp.tag_regex === '') return 'Clear Tag Filter';
    if (labelOp.tag_regex) return `Set Tag Filter: ${labelOp.tag_regex}`;
    if (labelOp.script === '') return 'Clear Script';
    if (labelOp.script) return `Set Script: ${labelOp.script}`;
    return 'Apply Labels';
  };

  // Run batch label operation (apply label changes to multiple containers)
  const runBatchLabel = async (info: BatchLabelOperation) => {
    const { containers: containerNames, labelOp } = info;
    const opDescription = describeLabelOp(labelOp);

    // Initialize container list
    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Applying label changes...',
    })));

    addLog(`Applying "${opDescription}" to ${containerNames.length} container(s)`, 'info', 'fa-tags');

    try {
      const operations = containerNames.map(name => ({ container: name, ...labelOp }));
      const response = await batchSetLabels(operations);

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Batch label operation failed' });
        addLog(`Batch label operation failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      // Save batch group ID to URL for recovery
      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      // Process per-container results
      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: opDescription, percent: 100 });
          if (result.operation_id) {
            containerToOpIdRef.current.set(result.container, result.operation_id);
          }
          addLog(`${result.container}: ${opDescription} applied`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      // Final summary
      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`Labels applied to ${successCount} container(s) successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run batch start operation (start multiple containers via batch endpoint)
  const runBatchStart = async (info: BatchStartOperation) => {
    const { containers: containerNames } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Starting...',
    })));

    addLog(`Starting ${containerNames.length} container(s)`, 'info', 'fa-play');

    try {
      const response = await batchStartContainers(containerNames);

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Batch start failed' });
        addLog(`Batch start failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: 'Started successfully', percent: 100 });
          if (result.operation_id) containerToOpIdRef.current.set(result.container, result.operation_id);
          addLog(`${result.container}: Started successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed to start`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Start completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`${successCount} container(s) started successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run batch stop operation (stop multiple containers via batch endpoint)
  const runBatchStop = async (info: BatchStopOperation) => {
    const { containers: containerNames } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Stopping...',
    })));

    addLog(`Stopping ${containerNames.length} container(s)`, 'info', 'fa-stop');

    try {
      const response = await batchStopContainers(containerNames);

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Batch stop failed' });
        addLog(`Batch stop failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: 'Stopped successfully', percent: 100 });
          if (result.operation_id) containerToOpIdRef.current.set(result.container, result.operation_id);
          addLog(`${result.container}: Stopped successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed to stop`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Stop completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`${successCount} container(s) stopped successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run batch restart operation (restart multiple containers via batch endpoint)
  const runBatchRestart = async (info: BatchRestartOperation) => {
    const { containers: containerNames } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Restarting...',
    })));

    addLog(`Restarting ${containerNames.length} container(s)`, 'info', 'fa-rotate');

    try {
      const response = await batchRestartContainers(containerNames);

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Batch restart failed' });
        addLog(`Batch restart failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: 'Restarted successfully', percent: 100 });
          if (result.operation_id) containerToOpIdRef.current.set(result.container, result.operation_id);
          addLog(`${result.container}: Restarted successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed to restart`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Restart completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`${successCount} container(s) restarted successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run batch remove operation (remove multiple containers via batch endpoint)
  const runBatchRemove = async (info: BatchRemoveOperation) => {
    const { containers: containerNames } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Removing...',
    })));

    addLog(`Removing ${containerNames.length} container(s)`, 'info', 'fa-trash');

    try {
      const response = await batchRemoveContainers(containerNames, true);

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Batch remove failed' });
        addLog(`Batch remove failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      const batchGroupId = response.data?.batch_group_id;
      if (batchGroupId) {
        setSearchParams({ group: batchGroupId }, { replace: true });
      }

      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: 'Removed successfully', percent: 100 });
          if (result.operation_id) containerToOpIdRef.current.set(result.container, result.operation_id);
          addLog(`${result.container}: Removed successfully`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed to remove`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Remove completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`${successCount} container(s) removed successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run label rollback operation (reverse label changes from a previous batch)
  const runLabelRollback = async (info: LabelRollbackOperation) => {
    const { batchGroupId, containers: containerNames, containerNames: specificContainers } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'in_progress' as const,
      percent: 0,
      message: 'Rolling back label changes...',
    })));

    const targetDesc = specificContainers?.length
      ? `${specificContainers.length} container(s)`
      : `${containerNames.length} container(s)`;
    addLog(`Rolling back label changes for ${targetDesc}`, 'info', 'fa-rotate-left');

    try {
      const response = await rollbackLabels({
        batch_group_id: batchGroupId,
        container_names: specificContainers,
      });

      if (!response.success) {
        updateContainersWhere(() => true, { status: 'failed', message: response.error || 'Label rollback failed' });
        addLog(`Label rollback failed: ${response.error}`, 'error', 'fa-circle-xmark');
        setStatus('failed');
        return;
      }

      const rollbackGroupId = response.data?.batch_group_id;
      if (rollbackGroupId) {
        setSearchParams({ group: rollbackGroupId }, { replace: true });
      }

      const results = response.data?.results || [];
      let successCount = 0;
      let failedCount = 0;

      for (const result of results) {
        if (result.success) {
          updateContainer(result.container, { status: 'success', message: 'Labels restored', percent: 100 });
          if (result.operation_id) containerToOpIdRef.current.set(result.container, result.operation_id);
          addLog(`${result.container}: Labels restored`, 'success', 'fa-circle-check');
          successCount++;
        } else {
          updateContainer(result.container, { status: 'failed', message: result.error || 'Failed', error: result.error });
          addLog(`${result.container}: ${result.error || 'Failed'}`, 'error', 'fa-circle-xmark');
          failedCount++;
        }
      }

      if (failedCount > 0 && successCount === 0) {
        addLog(`All ${failedCount} container(s) failed to rollback`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      } else if (failedCount > 0) {
        addLog(`Rollback completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success');
      } else {
        addLog(`Labels restored for ${successCount} container(s) successfully`, 'success', 'fa-circle-check');
        setStatus('success');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      updateContainersWhere(() => true, { status: 'failed', message: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
      setStatus('failed');
    }
  };

  // Run fix mismatch operation
  const runFixMismatch = async (info: FixMismatchOperation) => {
    const { containerName } = info;

    setContainers([{
      name: containerName,
      status: 'in_progress',
      message: 'Syncing container to compose file...',
    }]);

    addLog(`Fixing compose mismatch for ${containerName}...`, 'info', 'fa-rotate');

    try {
      const response = await fixComposeMismatch(containerName);

      if (!response.success || !response.data?.operation_id) {
        throw new Error(response.error || 'Failed to start fix mismatch operation');
      }

      setOperationId(response.data.operation_id);
      // Add operation ID to URL for recovery after page refresh
      setSearchParams({ id: response.data.operation_id }, { replace: true });
      addLog(`Operation started: ${response.data.operation_id.substring(0, 8)}...`, 'info', 'fa-play');

      // Poll for completion
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
              setStatus('success');
              updateContainer(containerName, { status: 'success', message: 'Fixed successfully', percent: 100 });
              addLog('Compose mismatch fixed successfully', 'success', 'fa-circle-check');
            } else if (op.status === 'failed') {
              completed = true;
              setStatus('failed');
              updateContainer(containerName, { status: 'failed', message: op.error_message, error: op.error_message });
              addLog(op.error_message || 'Fix mismatch failed', 'error', 'fa-circle-xmark');
            }
          }
        } catch {
          // Continue polling on error
        }
      }

      if (!completed) {
        setStatus('failed');
        updateContainer(containerName, { status: 'failed', message: 'Timed out waiting for completion' });
        addLog('Timed out waiting for completion', 'error', 'fa-clock');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to fix compose mismatch';
      setStatus('failed');
      updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
      addLog(errorMsg, 'error', 'fa-circle-xmark');
    }
  };

  // Run batch fix mismatch operation (sequential)
  const runBatchFixMismatch = async (info: BatchFixMismatchOperation) => {
    const { containerNames } = info;

    setContainers(containerNames.map(name => ({
      name,
      status: 'pending' as const,
      message: 'Waiting...',
    })));

    addLog(`Fixing ${containerNames.length} compose mismatch(es)...`, 'info', 'fa-rotate');

    let successCount = 0;
    let failedCount = 0;

    for (const containerName of containerNames) {
      updateContainer(containerName, { status: 'in_progress', message: 'Syncing container to compose file...' });
      addLog(`Fixing ${containerName}...`, 'info', 'fa-rotate');

      try {
        const response = await fixComposeMismatch(containerName);

        if (!response.success || !response.data?.operation_id) {
          throw new Error(response.error || 'Failed to start fix mismatch operation');
        }

        // Poll for completion
        let completed = false;
        let pollCount = 0;
        const maxPolls = 60;

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
                updateContainer(containerName, { status: 'success', message: 'Fixed successfully', percent: 100 });
                addLog(`${containerName} fixed successfully`, 'success', 'fa-circle-check');
              } else if (op.status === 'failed') {
                completed = true;
                failedCount++;
                updateContainer(containerName, { status: 'failed', message: op.error_message, error: op.error_message });
                addLog(`${containerName} failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
              }
            }
          } catch {
            // Continue polling on error
          }
        }

        if (!completed) {
          failedCount++;
          updateContainer(containerName, { status: 'failed', message: 'Timed out waiting for completion' });
          addLog(`${containerName} timed out`, 'error', 'fa-clock');
        }
      } catch (err) {
        failedCount++;
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
        addLog(`${containerName} failed: ${errorMsg}`, 'error', 'fa-circle-xmark');
      }
    }

    // Final status
    if (failedCount > 0) {
      if (successCount > 0) {
        addLog(`Completed with errors: ${successCount} fixed, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('failed');
      } else {
        addLog(`All ${failedCount} fix(es) failed`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      }
    } else {
      addLog(`All ${successCount} compose mismatch(es) fixed successfully`, 'success', 'fa-circle-check');
      setStatus('success');
    }
  };

  // Run mixed operation (updates + mismatches)
  const runMixed = async (info: MixedOperation) => {
    const { updates, mismatches } = info;
    const totalCount = updates.length + mismatches.length;

    addLog(`Processing ${totalCount} item(s): ${updates.length} update(s) and ${mismatches.length} mismatch fix(es)...`, 'info', 'fa-layer-group');

    // Initialize containers for both types
    const allContainers = [
      ...updates.map(u => ({ name: u.name, status: 'pending' as const, message: 'Update pending' })),
      ...mismatches.map(name => ({ name, status: 'pending' as const, message: 'Fix pending' })),
    ];
    setContainers(allContainers);

    let successCount = 0;
    let failedCount = 0;

    // First, run updates via batch update API
    if (updates.length > 0) {
      addLog(`Starting ${updates.length} update(s)...`, 'info', 'fa-download');

      try {
        const response = await triggerBatchUpdate(updates);

        if (!response.success) {
          throw new Error(response.error || 'Failed to start batch update');
        }

        // Track operations (batch update returns stack-level operations)
        if (response.data?.operations) {
          for (const stackOp of response.data.operations) {
            if (stackOp.operation_id) {
              // Mark all containers in this stack as in progress
              for (const containerName of stackOp.containers) {
                updateContainer(containerName, { status: 'in_progress', message: 'Updating...' });
                containerToOpIdRef.current.set(containerName, stackOp.operation_id);
              }
            } else if (stackOp.error) {
              // Mark all containers in this stack as failed
              for (const containerName of stackOp.containers) {
                failedCount++;
                updateContainer(containerName, { status: 'failed', message: stackOp.error || 'Failed to start', error: stackOp.error });
                addLog(`${containerName} failed to start: ${stackOp.error || 'Unknown error'}`, 'error', 'fa-circle-xmark');
              }
            }
          }
        }

        // Poll for update completions
        const pendingUpdates = new Set(updates.map(u => u.name).filter(name =>
          containerToOpIdRef.current.has(name)
        ));

        let pollCount = 0;
        const maxPolls = 120;

        while (pendingUpdates.size > 0 && pollCount < maxPolls) {
          await new Promise(resolve => setTimeout(resolve, 2000));
          pollCount++;

          for (const name of pendingUpdates) {
            const opId = containerToOpIdRef.current.get(name);
            if (!opId) continue;

            try {
              const opResponse = await fetch(`/api/operations/${opId}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;
                if (op.status === 'complete') {
                  pendingUpdates.delete(name);
                  successCount++;
                  updateContainer(name, { status: 'success', message: 'Updated', percent: 100 });
                  addLog(`${name} updated successfully`, 'success', 'fa-circle-check');
                } else if (op.status === 'failed') {
                  pendingUpdates.delete(name);
                  failedCount++;
                  updateContainer(name, { status: 'failed', message: op.error_message, error: op.error_message });
                  addLog(`${name} update failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
                }
              }
            } catch {
              // Continue polling
            }
          }
        }

        // Handle timeouts
        for (const name of pendingUpdates) {
          failedCount++;
          updateContainer(name, { status: 'failed', message: 'Timed out' });
          addLog(`${name} timed out`, 'error', 'fa-clock');
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Batch update failed';
        addLog(errorMsg, 'error', 'fa-circle-xmark');
        // Mark all pending updates as failed
        for (const u of updates) {
          const container = allContainers.find(c => c.name === u.name);
          if (container?.status === 'pending') {
            failedCount++;
            updateContainer(u.name, { status: 'failed', message: errorMsg, error: errorMsg });
          }
        }
      }
    }

    // Then, run mismatch fixes sequentially
    if (mismatches.length > 0) {
      addLog(`Starting ${mismatches.length} mismatch fix(es)...`, 'info', 'fa-rotate');

      for (const containerName of mismatches) {
        updateContainer(containerName, { status: 'in_progress', message: 'Syncing to compose file...' });

        try {
          const response = await fixComposeMismatch(containerName);

          if (!response.success || !response.data?.operation_id) {
            throw new Error(response.error || 'Failed to start');
          }

          // Poll for completion
          let completed = false;
          let pollCount = 0;
          const maxPolls = 60;

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
                  updateContainer(containerName, { status: 'success', message: 'Fixed', percent: 100 });
                  addLog(`${containerName} fixed successfully`, 'success', 'fa-circle-check');
                } else if (op.status === 'failed') {
                  completed = true;
                  failedCount++;
                  updateContainer(containerName, { status: 'failed', message: op.error_message, error: op.error_message });
                  addLog(`${containerName} fix failed: ${op.error_message || 'Unknown error'}`, 'error', 'fa-circle-xmark');
                }
              }
            } catch {
              // Continue polling
            }
          }

          if (!completed) {
            failedCount++;
            updateContainer(containerName, { status: 'failed', message: 'Timed out' });
            addLog(`${containerName} timed out`, 'error', 'fa-clock');
          }
        } catch (err) {
          failedCount++;
          const errorMsg = err instanceof Error ? err.message : 'Unknown error';
          updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
          addLog(`${containerName} fix failed: ${errorMsg}`, 'error', 'fa-circle-xmark');
        }
      }
    }

    // Final status
    if (failedCount > 0) {
      if (successCount > 0) {
        addLog(`Completed with errors: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
      } else {
        addLog(`All ${failedCount} operation(s) failed`, 'error', 'fa-circle-xmark');
      }
      setStatus('failed');
    } else {
      addLog(`All ${successCount} operation(s) completed successfully`, 'success', 'fa-circle-check');
      setStatus('success');
    }
  };

  // Handle force retry
  const handleForceRetry = () => {
    if (!operationInfo) return;

    // Reset state
    setStatus('in_progress');
    setContainers([]);
    setLogs([]);
    setCurrentStage(null);
    setCurrentPercent(0);
    setOperationId(null);
    setCanForceRetry(false);
    setForceRetryMessage('');
    setHasStarted(false);
    processedEventsRef.current.clear();
    containerToOpIdRef.current.clear();
    maxPercentRef.current.clear();
    lastLoggedStageRef.current.clear();

    // Navigate with force=true
    navigate('/operation', {
      state: {
        [operationInfo.type]: {
          ...operationInfo,
          force: true,
        }
      },
      replace: true,
    });

    // Force reload to reset component state
    window.location.reload();
  };

  // Detect completion from SSE events for batch updates
  // This avoids waiting for the polling interval when all containers are done
  useEffect(() => {
    if (operationType !== 'update' || status !== 'in_progress' || containers.length === 0) return;

    // Check if all containers have completed (success or failed)
    const allDone = containers.every(c => c.status === 'success' || c.status === 'failed');
    if (allDone) {
      setStatus('success'); // This will trigger the summary log
    }
  }, [operationType, status, containers]);

  // Log final summary when update completes
  useEffect(() => {
    if (operationType === 'update' && status === 'success' && containers.length > 0 && logs.length > 0) {
      const lastLog = logs[logs.length - 1];
      if (!lastLog.message.startsWith('Update complete:')) {
        const successful = containers.filter(c => c.status === 'success').length;
        const failed = containers.filter(c => c.status === 'failed').length;
        addLog(
          `Update complete: ${successful} succeeded, ${failed} failed`,
          successful === containers.length ? 'success' : 'info',
          successful === containers.length ? 'fa-trophy' : 'fa-flag-checkered'
        );
      }
    }
  }, [status, containers.length, operationType, logs.length]);

  // Show error only if no operation info AND not in recovery mode
  if (!operationInfo && !isRecoveryMode) {
    return (
      <div className="progress-page operation-progress-page">
        <header className="page-header">
          <button className="back-button" onClick={() => navigate('/')}>
            ← Back
          </button>
          <h1>Progress</h1>
          <div className="header-spacer" />
        </header>
        <main className="page-content">
          <div className="error-state">
            <i className="fa-solid fa-exclamation-circle"></i>
            <p>No operation information provided</p>
            <button className="button button-primary" onClick={() => navigate('/')}>
              Return to Containers
            </button>
          </div>
        </main>
      </div>
    );
  }

  const isComplete = status === 'success' || status === 'failed';
  const successCount = containers.filter(c => c.status === 'success').length;
  const failedCount = containers.filter(c => c.status === 'failed').length;
  const hasErrors = failedCount > 0 || status === 'failed';

  const getPageTitle = () => {
    // Handle recovery mode
    if (isRecoveryMode && recoveredOperation) {
      const opType = recoveredOperation.operation_type;
      switch (opType) {
        case 'restart': return 'Restarting Container';
        case 'single': return 'Updating Container';
        case 'batch': return 'Updating Containers';
        case 'rollback': return 'Rolling Back';
        case 'start': return 'Starting Container';
        case 'stop': return 'Stopping Container';
        case 'remove': return 'Removing Container';
        case 'label_change': return 'Applying Labels';
        default: return 'Operation Progress';
      }
    }
    switch (operationType) {
      case 'restart': return 'Restarting Container';
      case 'update': return 'Updating Containers';
      case 'rollback': return 'Rolling Back';
      case 'start': return 'Starting Container';
      case 'stop': return 'Stopping Container';
      case 'remove': return 'Removing Container';
      case 'stackRestart': return 'Restarting Stack';
      case 'stackStop': return 'Stopping Stack';
      case 'fixMismatch': return 'Fixing Compose Mismatch';
      case 'batchFixMismatch': return 'Fixing Compose Mismatches';
      case 'mixed': return 'Processing Containers';
      case 'batchLabel': return 'Applying Labels';
      case 'batchStart': return 'Starting Containers';
      case 'batchStop': return 'Stopping Containers';
      case 'batchRestart': return 'Restarting Containers';
      case 'batchRemove': return 'Removing Containers';
      case 'labelRollback': return 'Rolling Back Labels';
      default: return 'Progress';
    }
  };

  const getStageIcon = () => {
    if (hasErrors) {
      return <i className="fa-solid fa-circle-xmark"></i>;
    }
    if (isComplete) {
      return <i className="fa-solid fa-circle-check"></i>;
    }
    if (currentStage) {
      const stageInfo = STAGE_INFO[currentStage] || RESTART_STAGES[currentStage];
      if (stageInfo) {
        return <i className={`fa-solid ${stageInfo.icon}`}></i>;
      }
    }
    return <i className="fa-solid fa-spinner fa-spin"></i>;
  };

  const getStageMessage = () => {
    if (isComplete) {
      if (hasErrors) {
        return 'Operation failed';
      }
      // Handle recovery mode completion messages
      if (isRecoveryMode && recoveredOperation) {
        return recoveredOperation.error_message || 'Completed successfully!';
      }
      switch (operationType) {
        case 'restart': return 'Container restarted successfully!';
        case 'update': return 'All updates completed successfully!';
        case 'rollback': return 'Rollback completed successfully!';
        case 'start': return 'Container started successfully!';
        case 'stop': return 'Container stopped successfully!';
        case 'remove': return 'Container removed successfully!';
        case 'stackRestart': return 'Stack restarted successfully!';
        case 'stackStop': return 'Stack stopped successfully!';
        case 'fixMismatch': return 'Compose mismatch fixed successfully!';
        case 'batchFixMismatch': return 'All compose mismatches fixed successfully!';
        case 'mixed': return 'All operations completed successfully!';
        case 'batchLabel': return 'Labels applied successfully!';
        case 'batchStart': return 'All containers started successfully!';
        case 'batchStop': return 'All containers stopped successfully!';
        case 'batchRestart': return 'All containers restarted successfully!';
        case 'batchRemove': return 'All containers removed successfully!';
        case 'labelRollback': return 'Labels restored successfully!';
        default: return 'Completed successfully!';
      }
    }

    if (currentStage) {
      const stageInfo = STAGE_INFO[currentStage] || RESTART_STAGES[currentStage];
      if (stageInfo) {
        const containerName = operationType === 'update' && containers.find(c => c.status === 'in_progress')?.name;
        return containerName ? `${containerName}: ${stageInfo.label}` : stageInfo.label;
      }
    }

    // Handle recovery mode in-progress messages
    if (isRecoveryMode) {
      const containerName = recoveredOperation?.container_name || containers[0]?.name || 'Container';
      return `Recovering status for ${containerName}...`;
    }

    switch (operationType) {
      case 'restart': return `Restarting ${(operationInfo as RestartOperation).containerName}...`;
      case 'update': return `Updating ${containers.length} container(s)...`;
      case 'rollback': return `Rolling back ${(operationInfo as RollbackOperation).containerName}...`;
      case 'start': return `Starting ${(operationInfo as StartOperation).containerName}...`;
      case 'stop': return `Stopping ${(operationInfo as StopOperation).containerName}...`;
      case 'remove': return `Removing ${(operationInfo as RemoveOperation).containerName}...`;
      case 'stackRestart': return `Restarting ${(operationInfo as StackRestartOperation).containers.length} container(s) in "${(operationInfo as StackRestartOperation).stackName}"...`;
      case 'stackStop': return `Stopping ${(operationInfo as StackStopOperation).containers.length} container(s) in "${(operationInfo as StackStopOperation).stackName}"...`;
      case 'fixMismatch': return `Fixing ${(operationInfo as FixMismatchOperation).containerName}...`;
      case 'batchFixMismatch': return `Fixing ${(operationInfo as BatchFixMismatchOperation).containerNames.length} compose mismatch(es)...`;
      case 'mixed': return `Processing ${(operationInfo as MixedOperation).updates.length + (operationInfo as MixedOperation).mismatches.length} container(s)...`;
      case 'batchLabel': return `Applying labels to ${(operationInfo as BatchLabelOperation).containers.length} container(s)...`;
      case 'batchStart': return `Starting ${(operationInfo as BatchStartOperation).containers.length} container(s)...`;
      case 'batchStop': return `Stopping ${(operationInfo as BatchStopOperation).containers.length} container(s)...`;
      case 'batchRestart': return `Restarting ${(operationInfo as BatchRestartOperation).containers.length} container(s)...`;
      case 'batchRemove': return `Removing ${(operationInfo as BatchRemoveOperation).containers.length} container(s)...`;
      case 'labelRollback': return `Rolling back labels for ${(operationInfo as LabelRollbackOperation).containers.length} container(s)...`;
      default: return 'Processing...';
    }
  };

  const getStageDescription = () => {
    if (currentStage) {
      const stageInfo = STAGE_INFO[currentStage] || RESTART_STAGES[currentStage];
      if (stageInfo) {
        return stageInfo.description;
      }
    }
    return '';
  };

  const getForceButtonLabel = () => {
    switch (operationType) {
      case 'restart': return 'Force Restart';
      case 'rollback': return 'Force Rollback';
      case 'stackRestart': return 'Force Restart Stack';
      default: return 'Force Retry';
    }
  };

  return (
    <div className="progress-page operation-progress-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate(-1)} disabled={!isComplete}>
          ← Back
        </button>
        <h1>{getPageTitle()}</h1>
        <div className="header-spacer" />
      </header>

      <main className="page-content">
        {/* Stage Display */}
        <div className="progress-stage-section">
          <div className={`stage-icon ${isComplete ? (hasErrors ? 'error' : 'success') : 'in-progress'}`}>
            {!isComplete ? (
              <span className="spinning">{getStageIcon()}</span>
            ) : (
              getStageIcon()
            )}
          </div>
          <div className="stage-message">{getStageMessage()}</div>
          {!isComplete && getStageDescription() && (
            <div className="stage-description">{getStageDescription()}</div>
          )}
        </div>

        {/* Progress Bar */}
        {!isComplete && currentPercent > 0 && (
          <div className="current-progress-section">
            <div className="progress-bar-container">
              <div
                className={`progress-bar-fill ${operationType === 'rollback' || operationType === 'labelRollback' ? 'warning' : 'accent'}`}
                style={{ width: `${currentPercent}%` }}
              />
              <span className="progress-bar-text">{currentPercent}%</span>
            </div>
          </div>
        )}

        {/* Stats Cards */}
        <ProgressStats
          total={containers.length}
          successCount={successCount}
          failedCount={failedCount}
          elapsedTime={elapsedTime}
          isComplete={isComplete}
        />

        {/* Container List */}
        <section className="containers-section">
          <h2><i className="fa-solid fa-cube"></i> Containers</h2>
          <div className="container-list">
            {containers.map((container) => (
              <ContainerProgressRow key={container.name} container={container} />
            ))}

            {/* Dependent Containers Section */}
            {(dependentsRestarted.length > 0 || dependentsBlocked.length > 0 || (currentStage === 'restarting_dependents' && expectedDependents.length > 0)) && (
              <div className="dependents-section">
                <h3 className="dependents-header">
                  <i className="fa-solid fa-link"></i>
                  Dependent Containers
                </h3>
                {dependentsRestarted.map((depName) => (
                  <div key={depName} className="container-item status-success dependent">
                    <div className="container-main-row">
                      <span className="status-icon">
                        <i className="fa-solid fa-check"></i>
                      </span>
                      <span className="container-name">{depName}</span>
                      <span className="container-badge dependent">Dependent</span>
                    </div>
                    <div className="container-message">Restarted successfully</div>
                  </div>
                ))}
                {dependentsBlocked.map((depName) => (
                  <div key={depName} className="container-item status-failed dependent">
                    <div className="container-main-row">
                      <span className="status-icon">
                        <i className="fa-solid fa-xmark"></i>
                      </span>
                      <span className="container-name">{depName}</span>
                      <span className="container-badge dependent warning">Blocked</span>
                    </div>
                    <div className="container-message">Pre-update check failed</div>
                  </div>
                ))}
                {expectedDependents
                  .filter(d => !dependentsRestarted.includes(d) && !dependentsBlocked.includes(d))
                  .map((depName) => (
                  <div key={depName} className="container-item status-in_progress dependent">
                    <div className="container-main-row">
                      <span className="status-icon">
                        <i className="fa-solid fa-spinner fa-spin"></i>
                      </span>
                      <span className="container-name">{depName}</span>
                      <span className="container-badge dependent">Dependent</span>
                    </div>
                    <div className="container-message">Restarting...</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </section>

        {/* Activity Log */}
        <ActivityLog logs={logs} logRef={logEntriesRef} />
      </main>

      <footer className="page-footer">
        {canForceRetry ? (
          <div className="footer-buttons">
            <button
              className="button button-secondary"
              onClick={() => navigate(-1)}
            >
              Cancel
            </button>
            <button
              className="button button-danger"
              onClick={handleForceRetry}
              title={forceRetryMessage}
            >
              <i className="fa-solid fa-triangle-exclamation"></i>
              {getForceButtonLabel()}
            </button>
          </div>
        ) : isComplete && operationType === 'batchLabel' && successCount > 0 ? (
          <div className="footer-buttons">
            <button
              className="button button-secondary"
              onClick={() => {
                const groupId = searchParams.get('group');
                if (!groupId) return;
                const successfulContainers = containers.filter(c => c.status === 'success').map(c => c.name);
                navigate('/operation', {
                  state: {
                    labelRollback: {
                      batchGroupId: groupId,
                      containers: successfulContainers,
                    }
                  },
                  replace: true,
                });
                window.location.reload();
              }}
            >
              <i className="fa-solid fa-rotate-left"></i>
              Rollback
            </button>
            <button
              className="button button-primary"
              onClick={() => navigate(-1)}
            >
              Done
            </button>
          </div>
        ) : (
          <button
            className="button button-primary"
            onClick={() => navigate(-1)}
            disabled={!isComplete}
            style={{ width: '100%' }}
          >
            {isComplete ? 'Done' : 'Processing...'}
          </button>
        )}
      </footer>
    </div>
  );
}
