import { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useEventStream } from '../hooks/useEventStream';
import type { UpdateProgressEvent } from '../hooks/useEventStream';
import { useElapsedTime } from '../hooks/useElapsedTime';
import './RollbackProgressPage.css';

interface RollbackInfo {
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
}

interface LogEntry {
  time: number;
  message: string;
  type: 'info' | 'success' | 'error' | 'stage';
  icon?: string;
}

// Stage display information - same structure as UpdateProgressPage
const STAGE_INFO: Record<string, { icon: string; label: string; description: string }> = {
  'validating': { icon: 'fa-magnifying-glass', label: 'Validating', description: 'Checking container configuration...' },
  'backup': { icon: 'fa-floppy-disk', label: 'Backup', description: 'Creating backup of current state...' },
  'updating_compose': { icon: 'fa-file-pen', label: 'Updating Compose', description: 'Reverting compose file to previous version...' },
  'pulling_image': { icon: 'fa-cloud-arrow-down', label: 'Pulling Image', description: 'Downloading previous container image...' },
  'recreating': { icon: 'fa-rotate', label: 'Recreating', description: 'Recreating container with previous image...' },
  'health_check': { icon: 'fa-heart-pulse', label: 'Health Check', description: 'Waiting for container to become healthy...' },
  'rolling_back': { icon: 'fa-rotate-left', label: 'Rolling Back', description: 'Reverting to previous version...' },
  'complete': { icon: 'fa-circle-check', label: 'Complete', description: 'Rollback completed successfully' },
  'failed': { icon: 'fa-circle-xmark', label: 'Failed', description: 'Rollback failed' },
};

export function RollbackProgressPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { lastEvent, clearEvents } = useEventStream(true);

  // Get rollback info from navigation state
  const rollbackInfo: RollbackInfo | null = location.state?.rollback || null;

  const [status, setStatus] = useState<'in_progress' | 'success' | 'failed'>('in_progress');
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [startTime, setStartTime] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [hasStarted, setHasStarted] = useState(false);
  const [currentStage, setCurrentStage] = useState<string | null>(null);
  const [currentPercent, setCurrentPercent] = useState<number>(0);
  const [operationId, setOperationId] = useState<string | null>(null);
  const logEntriesRef = useRef<HTMLDivElement>(null);
  const processedEventsRef = useRef<Set<string>>(new Set());

  // Auto-scroll logs
  useEffect(() => {
    if (logEntriesRef.current) {
      logEntriesRef.current.scrollTop = logEntriesRef.current.scrollHeight;
    }
  }, [logs]);

  // Calculate elapsed time
  const isRollingBack = startTime !== null && status === 'in_progress';
  const elapsedTime = useElapsedTime(startTime, isRollingBack);

  // Add log entry helper
  const addLog = (message: string, type: LogEntry['type'] = 'info', icon?: string) => {
    setLogs(prev => [...prev, { time: Date.now(), message, type, icon }]);
  };

  // Handle SSE progress events
  useEffect(() => {
    if (!lastEvent || status !== 'in_progress' || !operationId) return;

    const event = lastEvent as UpdateProgressEvent;
    const eventKey = `${event.operation_id}-${event.stage}-${event.percent}`;

    // Skip duplicate events or events not for our operation
    if (processedEventsRef.current.has(eventKey)) return;
    if (event.operation_id !== operationId && event.container_name !== rollbackInfo?.containerName) return;

    processedEventsRef.current.add(eventKey);

    // Update current stage display
    setCurrentStage(event.stage);
    setCurrentPercent(event.percent || event.progress || 0);

    // Add stage transition log
    const stageInfo = STAGE_INFO[event.stage];
    if (stageInfo && event.stage !== 'complete' && event.stage !== 'failed') {
      addLog(`${stageInfo.label} (${event.percent || 0}%)`, 'stage', stageInfo.icon);
    }

    // Handle completion via SSE
    if (event.stage === 'complete') {
      setStatus('success');
      addLog('Rollback completed successfully', 'success', 'fa-circle-check');
    } else if (event.stage === 'failed') {
      setStatus('failed');
      setError(event.message || 'Rollback failed');
      addLog(event.message || 'Rollback failed', 'error', 'fa-circle-xmark');
    }
  }, [lastEvent, status, operationId, rollbackInfo?.containerName]);

  // Start rollback process when component mounts
  useEffect(() => {
    if (!rollbackInfo) {
      navigate('/');
      return;
    }

    if (hasStarted) return;
    setHasStarted(true);

    const runRollback = async () => {
      clearEvents();
      processedEventsRef.current.clear();
      const now = Date.now();
      setStartTime(now);

      addLog(`Starting rollback of ${rollbackInfo.containerName}...`, 'info', 'fa-rotate-left');
      if (rollbackInfo.newVersion && rollbackInfo.oldVersion) {
        addLog(`Rolling back from ${rollbackInfo.newVersion} to ${rollbackInfo.oldVersion}`, 'info', 'fa-code-compare');
      }

      try {
        const response = await fetch('/api/rollback', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ operation_id: rollbackInfo.operationId }),
        });
        const data = await response.json();

        if (data.success) {
          const rollbackOpId = data.data?.operation_id;
          setOperationId(rollbackOpId);
          addLog(`Rollback operation started`, 'info', 'fa-play');

          if (rollbackOpId) {
            // Poll for completion as fallback to SSE
            let completed = false;
            let pollCount = 0;
            const maxPolls = 60;

            while (!completed && pollCount < maxPolls) {
              await new Promise(resolve => setTimeout(resolve, 5000));
              pollCount++;

              // Check if SSE already completed us
              if (status !== 'in_progress') {
                completed = true;
                break;
              }

              try {
                const opResponse = await fetch(`/api/operations/${rollbackOpId}`);
                const opData = await opResponse.json();

                if (opData.success && opData.data) {
                  const op = opData.data;

                  if (op.status === 'complete') {
                    completed = true;
                    setStatus('success');
                    addLog('Rollback completed successfully', 'success', 'fa-circle-check');
                  } else if (op.status === 'failed') {
                    completed = true;
                    setStatus('failed');
                    setError(op.error_message);
                    addLog(op.error_message || 'Rollback failed', 'error', 'fa-circle-xmark');
                  }
                }
              } catch {
                // Continue polling on error
              }
            }

            if (!completed) {
              setStatus('failed');
              setError('Timed out waiting for completion');
              addLog('Timed out waiting for completion', 'error', 'fa-clock');
            }
          } else {
            // No operation ID, mark as complete without polling
            setStatus('success');
            addLog('Rollback initiated successfully', 'success', 'fa-circle-check');
          }
        } else {
          setStatus('failed');
          setError(data.error);
          addLog(data.error || 'Failed to trigger rollback', 'error', 'fa-circle-xmark');
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Unknown error';
        setStatus('failed');
        setError(errorMsg);
        addLog(errorMsg, 'error', 'fa-circle-xmark');
      }
    };

    runRollback();
  }, [rollbackInfo, hasStarted, navigate, clearEvents]);

  const isComplete = status === 'success' || status === 'failed';

  const getStageIcon = () => {
    if (status === 'failed') {
      return <i className="fa-solid fa-circle-xmark"></i>;
    }
    if (status === 'success') {
      return <i className="fa-solid fa-circle-check"></i>;
    }
    // Show current stage icon if available
    if (currentStage && STAGE_INFO[currentStage]) {
      return <i className={`fa-solid ${STAGE_INFO[currentStage].icon}`}></i>;
    }
    return <i className="fa-solid fa-spinner fa-spin"></i>;
  };

  const getStageMessage = () => {
    if (status === 'failed') {
      return 'Rollback failed';
    }
    if (status === 'success') {
      return 'Rollback completed successfully!';
    }
    if (currentStage && STAGE_INFO[currentStage]) {
      return STAGE_INFO[currentStage].label;
    }
    return `Rolling back ${rollbackInfo?.containerName}...`;
  };

  const getStageDescription = () => {
    if (currentStage && STAGE_INFO[currentStage]) {
      return STAGE_INFO[currentStage].description;
    }
    return 'Reverting container to previous version...';
  };

  return (
    <div className="rollback-progress-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate('/')} disabled={!isComplete}>
          ← Back
        </button>
        <h1>Rolling Back</h1>
        <div className="header-spacer" />
      </header>

      <main className="page-content">
        {/* Stage Display */}
        <div className="progress-stage-section">
          <div className={`stage-icon ${status === 'in_progress' ? 'in-progress' : status === 'success' ? 'success' : 'error'}`}>
            {getStageIcon()}
          </div>
          <div className="stage-message">{getStageMessage()}</div>
          {status === 'in_progress' && (
            <div className="stage-description">{getStageDescription()}</div>
          )}
        </div>

        {/* Progress Bar */}
        {status === 'in_progress' && currentPercent > 0 && (
          <div className="current-progress-section">
            <div className="progress-bar-container">
              <div className="progress-bar-fill" style={{ width: `${currentPercent}%` }} />
              <span className="progress-bar-text">{currentPercent}%</span>
            </div>
          </div>
        )}

        {/* Container Info */}
        {rollbackInfo && (
          <section className="info-section">
            <h2>Container Details</h2>
            <div className="info-card">
              <div className="info-row">
                <span className="info-label">Container</span>
                <span className="info-value">{rollbackInfo.containerName}</span>
              </div>
              {rollbackInfo.newVersion && rollbackInfo.oldVersion && (
                <>
                  <div className="info-row">
                    <span className="info-label">Current Version</span>
                    <span className="info-value code">{rollbackInfo.newVersion}</span>
                  </div>
                  <div className="info-row">
                    <span className="info-label">Rolling Back To</span>
                    <span className="info-value code">{rollbackInfo.oldVersion}</span>
                  </div>
                </>
              )}
              <div className="info-row">
                <span className="info-label">Status</span>
                <span className={`info-value status-badge ${status}`}>
                  {status === 'in_progress' ? 'In Progress' : status === 'success' ? 'Success' : 'Failed'}
                </span>
              </div>
              <div className="info-row">
                <span className="info-label">Elapsed</span>
                <span className="info-value">{elapsedTime}s</span>
              </div>
            </div>
          </section>
        )}

        {/* Error Message */}
        {error && (
          <div className="error-banner">
            <i className="fa-solid fa-exclamation-triangle"></i>
            <span>{error}</span>
          </div>
        )}

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
          disabled={!isComplete}
          style={{ width: '100%' }}
        >
          {isComplete ? 'Done' : 'Rolling Back...'}
        </button>
      </footer>
    </div>
  );
}
