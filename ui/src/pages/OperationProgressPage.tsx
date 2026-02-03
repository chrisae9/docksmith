import { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { startRestart, setLabels as setLabelsAPI, triggerBatchUpdate, stopContainer, removeContainer } from '../api/client';
import { useEventStream } from '../hooks/useEventStream';
import type { UpdateProgressEvent } from '../hooks/useEventStream';
import { useElapsedTime } from '../hooks/useElapsedTime';
import { STAGE_INFO, RESTART_STAGES, type LogEntry } from '../constants/progress';
import { ContainerProgressRow, ProgressStats, ActivityLog, type ContainerProgress } from '../components/ProgressComponents';
import '../styles/progress-common.css';
import './OperationProgressPage.css';

// Operation types
type OperationType = 'restart' | 'update' | 'rollback' | 'stop' | 'remove' | 'stackRestart' | 'stackStop';

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

type OperationInfo = RestartOperation | UpdateOperation | RollbackOperation | StopOperation | RemoveOperation | StackRestartOperation | StackStopOperation;

export function OperationProgressPage() {
  const navigate = useNavigate();
  const location = useLocation();

  // Determine operation type from location state
  const getOperationInfo = (): OperationInfo | null => {
    const state = location.state as any;
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
    return null;
  };

  const operationInfo = getOperationInfo();
  const operationType: OperationType | null = operationInfo?.type || null;

  // Common state
  const [status, setStatus] = useState<'in_progress' | 'success' | 'failed'>('in_progress');
  const [containers, setContainers] = useState<ContainerProgress[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [startTime, setStartTime] = useState<number | null>(null);
  const [hasStarted, setHasStarted] = useState(false);
  const [currentStage, setCurrentStage] = useState<string | null>(null);
  const [currentPercent, setCurrentPercent] = useState<number>(0);
  const [operationId, setOperationId] = useState<string | null>(null);
  const [canForceRetry, setCanForceRetry] = useState(false);
  const [forceRetryMessage, setForceRetryMessage] = useState<string>('');

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
  const { lastEvent, clearEvents } = useEventStream(status === 'in_progress' || operationId !== null);

  // Calculate elapsed time
  const isRunning = startTime !== null && status === 'in_progress';
  const elapsedTime = useElapsedTime(startTime, isRunning);

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
      addLog('This page will reconnect automatically when docksmith restarts.', 'info', 'fa-wifi');
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
      // Save settings flow - the API saves labels AND restarts the container atomically
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

        // Call API - this saves labels AND restarts the container
        const saveResponse = await setLabelsAPI(containerName, {
          ...labelChanges,
          force,
        });

        if (!saveResponse.success) {
          throw new Error(saveResponse.error || 'Failed to save settings');
        }

        // Show progress stages after API completes (the restart already happened on backend)
        // This provides visual feedback of what occurred
        const showStage = (stage: string, message: string, delay: number) => {
          return new Promise<void>(resolve => {
            const timeout = window.setTimeout(() => {
              setCurrentStage(stage);
              setCurrentPercent(delay === 0 ? 25 : delay === 300 ? 50 : delay === 600 ? 75 : 100);
              setContainers(prev => prev.map(c => c.name === containerName ? { ...c, stage } : c));
              addLog(message, 'stage', RESTART_STAGES[stage]?.icon || 'fa-circle');
              resolve();
            }, delay);
            timeoutsRef.current.push(timeout);
          });
        };

        await showStage('stopping', 'Stopping container...', 0);
        await showStage('starting', 'Starting container...', 300);
        await showStage('health_check', 'Health check passed', 300);

        updateContainer(containerName, { status: 'success', stage: 'complete', message: 'Settings saved and container restarted' });
        addLog('Settings saved and container restarted', 'success', 'fa-check');
        setStatus('success');
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
              } else if (op.status === 'failed') {
                completed = true;
                updateContainersWhere(
                  c => affectedContainers.includes(c.name) && c.status !== 'failed',
                  { status: 'failed', message: 'Update failed', error: op.error_message }
                );
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
    }

    setStatus('success'); // Will show summary based on container statuses
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
        addLog(`Rollback operation started`, 'info', 'fa-play');

        if (newOpId) {
          // Poll for completion as fallback to SSE
          let completed = false;
          let pollCount = 0;
          const maxPolls = 60;

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 2000));
            pollCount++;

            if (status !== 'in_progress') {
              completed = true;
              break;
            }

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

    let hasAnyFailure = false;
    let successCount = 0;
    let failedCount = 0;

    // Restart each container sequentially using the orchestrator
    for (const containerName of containerNames) {
      updateContainer(containerName, { status: 'in_progress', message: 'Starting restart...' });
      addLog(`Restarting ${containerName}...`, 'stage', 'fa-rotate');

      try {
        const response = await startRestart(containerName, force);

        if (!response.success || !response.data?.operation_id) {
          const errorMsg = response.error || 'Failed to start restart operation';
          updateContainer(containerName, { status: 'failed', message: errorMsg, error: errorMsg });
          addLog(`${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
          hasAnyFailure = true;
          failedCount++;

          // Check if it's a precheck failure
          if (isPreCheckFailure(errorMsg)) {
            setCanForceRetry(true);
            setForceRetryMessage('You can force restart to bypass pre-update checks');
          }
          continue;
        }

        const opId = response.data.operation_id;
        containerToOpIdRef.current.set(containerName, opId);
        addLog(`${containerName}: Operation started`, 'info', 'fa-play');

        // Poll for completion of this individual container's restart
        let completed = false;
        let pollCount = 0;
        const maxPolls = 90; // 3 minutes max per container

        while (!completed && pollCount < maxPolls) {
          await new Promise(resolve => setTimeout(resolve, 2000));
          pollCount++;

          try {
            const opResponse = await fetch(`/api/operations/${opId}`);
            const opData = await opResponse.json();

            if (opData.success && opData.data) {
              const op = opData.data;

              // Update progress from operation status
              if (op.current_stage) {
                const stageInfo = RESTART_STAGES[op.current_stage];
                if (stageInfo) {
                  updateContainer(containerName, {
                    stage: op.current_stage,
                    message: stageInfo.description,
                  });
                }
              }

              if (op.status === 'complete') {
                completed = true;
                updateContainer(containerName, { status: 'success', message: 'Restarted successfully', percent: 100 });
                addLog(`${containerName}: Restarted successfully`, 'success', 'fa-circle-check');
                successCount++;
              } else if (op.status === 'failed') {
                completed = true;
                const errorMsg = op.error_message || 'Restart failed';
                const briefMsg = isPreCheckFailure(errorMsg) ? 'Pre-update check failed' : errorMsg;
                updateContainer(containerName, { status: 'failed', message: briefMsg, error: errorMsg });
                addLog(`${containerName}: ${errorMsg}`, 'error', 'fa-circle-xmark');
                hasAnyFailure = true;
                failedCount++;

                if (isPreCheckFailure(errorMsg)) {
                  setCanForceRetry(true);
                  setForceRetryMessage('You can force restart to bypass pre-update checks');
                }
              }
            }
          } catch {
            // Continue polling on error
          }
        }

        if (!completed) {
          updateContainer(containerName, { status: 'failed', message: 'Timed out waiting for completion' });
          addLog(`${containerName}: Timed out`, 'error', 'fa-clock');
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
        addLog(`Stack restart completed with issues: ${successCount} succeeded, ${failedCount} failed`, 'warning', 'fa-triangle-exclamation');
        setStatus('success'); // Partial success still shows as success with individual failures visible
      } else {
        addLog(`Stack restart failed: all ${failedCount} container(s) failed`, 'error', 'fa-circle-xmark');
        setStatus('failed');
      }
    } else {
      addLog(`Stack restart completed: ${successCount} container(s) restarted successfully`, 'success', 'fa-circle-check');
      setStatus('success');
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

  // Redirect if no operation info
  if (!operationInfo) {
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
              Return to Dashboard
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
    switch (operationType) {
      case 'restart': return 'Restarting Container';
      case 'update': return 'Updating Containers';
      case 'rollback': return 'Rolling Back';
      case 'stop': return 'Stopping Container';
      case 'remove': return 'Removing Container';
      case 'stackRestart': return 'Restarting Stack';
      case 'stackStop': return 'Stopping Stack';
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
      switch (operationType) {
        case 'restart': return 'Container restarted successfully!';
        case 'update': return 'All updates completed successfully!';
        case 'rollback': return 'Rollback completed successfully!';
        case 'stop': return 'Container stopped successfully!';
        case 'remove': return 'Container removed successfully!';
        case 'stackRestart': return 'Stack restarted successfully!';
        case 'stackStop': return 'Stack stopped successfully!';
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

    switch (operationType) {
      case 'restart': return `Restarting ${(operationInfo as RestartOperation).containerName}...`;
      case 'update': return `Updating ${containers.length} container(s)...`;
      case 'rollback': return `Rolling back ${(operationInfo as RollbackOperation).containerName}...`;
      case 'stop': return `Stopping ${(operationInfo as StopOperation).containerName}...`;
      case 'remove': return `Removing ${(operationInfo as RemoveOperation).containerName}...`;
      case 'stackRestart': return `Restarting ${(operationInfo as StackRestartOperation).containers.length} container(s) in "${(operationInfo as StackRestartOperation).stackName}"...`;
      case 'stackStop': return `Stopping ${(operationInfo as StackStopOperation).containers.length} container(s) in "${(operationInfo as StackStopOperation).stackName}"...`;
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
                className={`progress-bar-fill ${operationType === 'rollback' ? 'warning' : 'accent'}`}
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
