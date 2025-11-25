import { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { triggerBatchUpdate } from '../api/client';
import { useEventStream } from '../hooks/useEventStream';
import type { UpdateProgressEvent } from '../hooks/useEventStream';
import { useElapsedTime } from '../hooks/useElapsedTime';
import { STAGE_INFO, type LogEntry } from '../constants/progress';
import '../styles/progress-common.css';
import './UpdateProgressPage.css';

interface ContainerToUpdate {
  name: string;
  target_version: string;
  stack: string;
}

interface ContainerProgress {
  name: string;
  status: 'pending' | 'in_progress' | 'success' | 'failed';
  stage?: string;
  percent?: number;
  message?: string;
  error?: string;
  operationId?: string;
}

export function UpdateProgressPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { lastEvent, clearEvents } = useEventStream(true);

  // Get containers from navigation state
  const containersToUpdate: ContainerToUpdate[] = location.state?.containers || [];

  const [containers, setContainers] = useState<ContainerProgress[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [startTime, setStartTime] = useState<number | null>(null);
  const [isComplete, setIsComplete] = useState(false);
  const [hasStarted, setHasStarted] = useState(false);
  const [currentStage, setCurrentStage] = useState<string | null>(null);
  const [currentContainer, setCurrentContainer] = useState<string | null>(null);
  const [currentPercent, setCurrentPercent] = useState<number>(0);
  const logEntriesRef = useRef<HTMLDivElement>(null);
  const containerToOpIdRef = useRef<Map<string, string>>(new Map());
  const processedEventsRef = useRef<Set<string>>(new Set());

  // Auto-scroll logs
  useEffect(() => {
    if (logEntriesRef.current) {
      logEntriesRef.current.scrollTop = logEntriesRef.current.scrollHeight;
    }
  }, [logs]);

  // Calculate elapsed time
  const isUpdating = startTime !== null && !isComplete;
  const elapsedTime = useElapsedTime(startTime, isUpdating);

  // Add log entry helper
  const addLog = (message: string, type: LogEntry['type'] = 'info', icon?: string) => {
    setLogs(prev => [...prev, { time: Date.now(), message, type, icon }]);
  };

  // Handle SSE progress events
  useEffect(() => {
    if (!lastEvent || isComplete) return;

    const event = lastEvent as UpdateProgressEvent;
    const eventKey = `${event.operation_id}-${event.stage}-${event.percent}`;

    // Skip duplicate events
    if (processedEventsRef.current.has(eventKey)) return;
    processedEventsRef.current.add(eventKey);

    // Check if this event is for one of our containers
    const containerToOpId = containerToOpIdRef.current;
    let targetContainer: string | null = null;

    for (const [containerName, opId] of containerToOpId) {
      if (opId === event.operation_id || event.container_name === containerName) {
        targetContainer = containerName;
        break;
      }
    }

    if (!targetContainer && event.container_name) {
      // Check if container name matches any of our containers
      for (const c of containers) {
        if (c.name === event.container_name) {
          targetContainer = c.name;
          break;
        }
      }
    }

    if (!targetContainer) return;

    // Update current stage display
    setCurrentStage(event.stage);
    setCurrentContainer(targetContainer);
    setCurrentPercent(event.percent || event.progress || 0);

    // Update container progress
    setContainers(prev => prev.map(c => {
      if (c.name === targetContainer) {
        const stageInfo = STAGE_INFO[event.stage];
        return {
          ...c,
          status: event.stage === 'complete' ? 'success' : event.stage === 'failed' ? 'failed' : 'in_progress',
          stage: event.stage,
          percent: event.percent || event.progress || 0,
          message: stageInfo?.description || event.message,
        };
      }
      return c;
    }));

    // Add stage transition log
    const stageInfo = STAGE_INFO[event.stage];
    if (stageInfo && event.stage !== 'complete' && event.stage !== 'failed') {
      addLog(`${targetContainer}: ${stageInfo.label} (${event.percent || 0}%)`, 'stage', stageInfo.icon);
    }

    // Handle completion
    if (event.stage === 'complete') {
      setContainers(prev => prev.map(c => {
        if (c.name === targetContainer) {
          return { ...c, status: 'success', message: 'Updated successfully', percent: 100 };
        }
        return c;
      }));
      addLog(`${targetContainer}: Update completed successfully`, 'success', 'fa-circle-check');
    } else if (event.stage === 'failed') {
      setContainers(prev => prev.map(c => {
        if (c.name === targetContainer) {
          return { ...c, status: 'failed', message: event.message || 'Update failed', error: event.message };
        }
        return c;
      }));
      addLog(`${targetContainer}: ${event.message || 'Update failed'}`, 'error', 'fa-circle-xmark');
    }
  }, [lastEvent, isComplete, containers]);

  // Start update process when component mounts
  useEffect(() => {
    if (containersToUpdate.length === 0) {
      navigate('/');
      return;
    }

    if (hasStarted) return;
    setHasStarted(true);

    const runUpdate = async () => {
      clearEvents();
      processedEventsRef.current.clear();
      const now = Date.now();
      setStartTime(now);

      // Initialize container progress
      setContainers(containersToUpdate.map(c => ({
        name: c.name,
        status: 'pending',
        percent: 0,
      })));

      addLog(`Starting stack-aware update of ${containersToUpdate.length} container(s)`, 'info', 'fa-rocket');

      // Group by stack for logging
      const stackGroups = new Map<string, string[]>();
      for (const c of containersToUpdate) {
        const stack = c.stack || '__standalone__';
        if (!stackGroups.has(stack)) {
          stackGroups.set(stack, []);
        }
        stackGroups.get(stack)!.push(c.name);
      }

      // Log stack grouping
      for (const [stack, containerNames] of stackGroups) {
        const stackName = stack === '__standalone__' ? 'Standalone' : stack;
        addLog(`${stackName}: ${containerNames.join(', ')}`, 'info', 'fa-layer-group');
      }

      try {
        // Send batch update request
        const response = await triggerBatchUpdate(containersToUpdate);

        if (!response.success) {
          setContainers(prev => prev.map(c => ({
            ...c,
            status: 'failed',
            message: 'Batch update failed',
            error: response.error,
          })));
          addLog(`Batch update failed: ${response.error}`, 'error', 'fa-circle-xmark');
          setIsComplete(true);
          return;
        }

        // Track operation IDs
        const operations = response.data?.operations || [];

        for (const op of operations) {
          if (op.status === 'started' && op.operation_id) {
            for (const containerName of op.containers) {
              containerToOpIdRef.current.set(containerName, op.operation_id);
            }
            setContainers(prev => prev.map(c => {
              if (op.containers.includes(c.name)) {
                return { ...c, status: 'in_progress', message: 'Initializing update...', operationId: op.operation_id };
              }
              return c;
            }));
            addLog(`Operation started for ${op.containers.join(', ')}`, 'info', 'fa-play');
          } else if (op.status === 'failed') {
            setContainers(prev => prev.map(c => {
              if (op.containers.includes(c.name)) {
                return { ...c, status: 'failed', message: 'Failed to start', error: op.error };
              }
              return c;
            }));
            addLog(`Failed to start: ${op.error}`, 'error', 'fa-circle-xmark');
          }
        }

        // Poll for completion (as fallback to SSE)
        const uniqueOpIds = new Set<string>();
        for (const opId of containerToOpIdRef.current.values()) {
          uniqueOpIds.add(opId);
        }

        const pollOperation = async (operationId: string) => {
          let completed = false;
          let pollCount = 0;
          const maxPolls = 120; // 10 minutes with 5 second intervals

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 5000));
            pollCount++;

            try {
              const opResponse = await fetch(`/api/operations/${operationId}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;
                const affectedContainers: string[] = [];
                for (const [containerName, opId] of containerToOpIdRef.current) {
                  if (opId === operationId) {
                    affectedContainers.push(containerName);
                  }
                }

                if (op.status === 'complete') {
                  completed = true;
                  setContainers(prev => prev.map(c => {
                    if (affectedContainers.includes(c.name) && c.status !== 'success') {
                      return { ...c, status: 'success', message: 'Updated successfully', percent: 100 };
                    }
                    return c;
                  }));
                } else if (op.status === 'failed') {
                  completed = true;
                  setContainers(prev => prev.map(c => {
                    if (affectedContainers.includes(c.name) && c.status !== 'failed') {
                      return { ...c, status: 'failed', message: 'Update failed', error: op.error_message };
                    }
                    return c;
                  }));
                }
              }
            } catch {
              // Continue polling on error
            }
          }

          if (!completed) {
            const affectedContainers: string[] = [];
            for (const [containerName, opId] of containerToOpIdRef.current) {
              if (opId === operationId) {
                affectedContainers.push(containerName);
              }
            }
            setContainers(prev => prev.map(c => {
              if (affectedContainers.includes(c.name) && c.status === 'in_progress') {
                return { ...c, status: 'failed', message: 'Timed out waiting for completion' };
              }
              return c;
            }));
            addLog(`Operation timed out`, 'error', 'fa-clock');
          }
        };

        await Promise.all(Array.from(uniqueOpIds).map(opId => pollOperation(opId)));

      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        setContainers(prev => prev.map(c => ({
          ...c,
          status: 'failed',
          message: 'Error',
          error: errorMsg,
        })));
        addLog(errorMsg, 'error', 'fa-circle-xmark');
      }

      setIsComplete(true);
    };

    runUpdate();
  }, [containersToUpdate, hasStarted, navigate, clearEvents]);

  // Log final summary when complete
  useEffect(() => {
    if (isComplete && containers.length > 0 && logs.length > 0) {
      const lastLog = logs[logs.length - 1];
      // Only add summary if not already added
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
  }, [isComplete, containers.length]);

  // Calculate stats
  const successCount = containers.filter(c => c.status === 'success').length;
  const failedCount = containers.filter(c => c.status === 'failed').length;
  const inProgressCount = containers.filter(c => c.status === 'in_progress').length;
  const completedCount = successCount + failedCount;
  const allComplete = containers.length > 0 && containers.every(c => c.status === 'success' || c.status === 'failed');
  const hasErrors = failedCount > 0;

  // Get overall stage display
  const getOverallStageIcon = () => {
    if (isComplete) {
      if (hasErrors) {
        return <i className="fa-solid fa-exclamation-triangle"></i>;
      }
      return <i className="fa-solid fa-circle-check"></i>;
    }
    if (currentStage && STAGE_INFO[currentStage]) {
      return <i className={`fa-solid ${STAGE_INFO[currentStage].icon}`}></i>;
    }
    return <i className="fa-solid fa-spinner fa-spin"></i>;
  };

  const getOverallStageMessage = () => {
    if (isComplete) {
      if (hasErrors) {
        return 'Updates completed with some errors';
      }
      return 'All updates completed successfully!';
    }
    if (currentStage && currentContainer) {
      const stageInfo = STAGE_INFO[currentStage];
      if (stageInfo) {
        return `${currentContainer}: ${stageInfo.label}`;
      }
    }
    return `Updating ${containers.length} container(s)...`;
  };

  return (
    <div className="progress-page update-progress-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate('/')} disabled={!allComplete}>
          ← Back
        </button>
        <h1>Updating Containers</h1>
        <div className="header-spacer" />
      </header>

      <main className="page-content">
        {/* Stage Display */}
        <div className="progress-stage-section">
          <div className={`stage-icon ${isComplete ? (hasErrors ? 'warning' : 'success') : 'in-progress'}`}>
            {getOverallStageIcon()}
          </div>
          <div className="stage-message">{getOverallStageMessage()}</div>
          {!isComplete && currentStage && STAGE_INFO[currentStage] && (
            <div className="stage-description">{STAGE_INFO[currentStage].description}</div>
          )}
        </div>

        {/* Current Operation Progress Bar */}
        {!isComplete && inProgressCount > 0 && (
          <div className="current-progress-section">
            <div className="progress-bar-container">
              <div className="progress-bar-fill accent" style={{ width: `${currentPercent}%` }} />
              <span className="progress-bar-text">{currentPercent}%</span>
            </div>
          </div>
        )}

        {/* Stats Cards */}
        <div className="progress-stats">
          <div className="stat-card">
            <span className="stat-label">Progress</span>
            <span className="stat-value">{completedCount}/{containers.length}</span>
          </div>
          <div className="stat-card success">
            <span className="stat-label">Successful</span>
            <span className="stat-value">{successCount}</span>
          </div>
          <div className="stat-card error">
            <span className="stat-label">Failed</span>
            <span className="stat-value">{failedCount}</span>
          </div>
          <div className="stat-card">
            <span className="stat-label">Elapsed</span>
            <span className="stat-value">{elapsedTime}s</span>
          </div>
        </div>

        {/* Container List */}
        <section className="containers-section">
          <h2>Containers</h2>
          <div className="container-list">
            {containers.map((container, index) => (
              <div key={container.name} className={`container-item status-${container.status}`}>
                <div className="container-main-row">
                  <span className="status-icon">
                    {container.status === 'pending' && <i className="fa-regular fa-circle"></i>}
                    {container.status === 'in_progress' && (
                      container.stage && STAGE_INFO[container.stage]
                        ? <i className={`fa-solid ${STAGE_INFO[container.stage].icon}`}></i>
                        : <i className="fa-solid fa-spinner fa-spin"></i>
                    )}
                    {container.status === 'success' && <i className="fa-solid fa-check"></i>}
                    {container.status === 'failed' && <i className="fa-solid fa-xmark"></i>}
                  </span>
                  <span className="container-index">{index + 1}.</span>
                  <span className="container-name">{container.name}</span>
                  {container.status === 'in_progress' && container.percent !== undefined && container.percent > 0 && (
                    <span className="container-percent">{container.percent}%</span>
                  )}
                </div>
                {container.message && (
                  <div className="container-message">{container.message}</div>
                )}
                {container.status === 'in_progress' && container.percent !== undefined && container.percent > 0 && (
                  <div className="container-progress-bar">
                    <div className="container-progress-fill" style={{ width: `${container.percent}%` }} />
                  </div>
                )}
                {container.error && (
                  <div className="container-error">{container.error}</div>
                )}
              </div>
            ))}
          </div>
        </section>

        {/* Activity Log */}
        <section className="activity-section">
          <h2>Activity Log</h2>
          <div className="activity-log" ref={logEntriesRef}>
            {logs.map((log, i) => (
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
        </section>
      </main>

      <footer className="page-footer">
        <button
          className="button button-primary"
          onClick={() => navigate('/')}
          disabled={!allComplete}
          style={{ width: '100%' }}
        >
          {allComplete ? 'Done' : 'Updating...'}
        </button>
      </footer>
    </div>
  );
}
