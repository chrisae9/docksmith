import { useState, useEffect, useRef } from 'react';
import { getOperations } from '../api/client';
import type { UpdateOperation } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';

interface HistoryProps {
  onBack: () => void;
}

interface RollbackConfirmation {
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
}

export function History({ onBack }: HistoryProps) {
  const [operations, setOperations] = useState<UpdateOperation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<'all' | 'complete' | 'failed'>('all');
  const [expandedOp, setExpandedOp] = useState<string | null>(null);
  const [rollbackConfirm, setRollbackConfirm] = useState<RollbackConfirmation | null>(null);
  const [rollbackProgress, setRollbackProgress] = useState<{
    containerName: string;
    status: 'pending' | 'in_progress' | 'success' | 'failed';
    operationId?: string;
    message?: string;
    error?: string;
    startTime: number;
    logs: Array<{ time: number; message: string }>;
  } | null>(null);
  const [elapsedTime, setElapsedTime] = useState(0);
  const elapsedIntervalRef = useRef<number | null>(null);

  const { lastEvent: progressEvent, clearEvents } = useEventStream(true);

  useEffect(() => {
    fetchOperations();
  }, []);

  // Update elapsed time during rollback
  useEffect(() => {
    if (rollbackProgress && rollbackProgress.status === 'in_progress') {
      elapsedIntervalRef.current = window.setInterval(() => {
        setElapsedTime(Math.floor((Date.now() - rollbackProgress.startTime) / 1000));
      }, 1000);
    } else if (elapsedIntervalRef.current) {
      clearInterval(elapsedIntervalRef.current);
      elapsedIntervalRef.current = null;
    }
    return () => {
      if (elapsedIntervalRef.current) {
        clearInterval(elapsedIntervalRef.current);
      }
    };
  }, [rollbackProgress]);

  // Add SSE progress events to rollback log
  useEffect(() => {
    if (progressEvent && rollbackProgress && rollbackProgress.status === 'in_progress') {
      setRollbackProgress(prev => {
        if (!prev) return prev;
        const newLog = {
          time: progressEvent.timestamp ? progressEvent.timestamp * 1000 : Date.now(),
          message: `${progressEvent.container_name}: ${progressEvent.message}`,
        };
        const lastLog = prev.logs[prev.logs.length - 1];
        if (lastLog && lastLog.message === newLog.message) {
          return prev;
        }
        return {
          ...prev,
          logs: [...prev.logs.slice(-19), newLog],
        };
      });
    }
  }, [progressEvent, rollbackProgress]);

  const fetchOperations = async () => {
    setLoading(true);
    try {
      const response = await getOperations({ limit: 100 });
      if (response.success && response.data) {
        // Sort by completed_at or created_at DESC (most recent first)
        const sorted = response.data.operations.sort((a, b) => {
          const timeA = new Date(a.completed_at || a.created_at).getTime();
          const timeB = new Date(b.completed_at || b.created_at).getTime();
          return timeB - timeA;
        });
        setOperations(sorted);
      } else {
        setError(response.error || 'Failed to fetch operations');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const showRollbackConfirm = (op: UpdateOperation) => {
    setRollbackConfirm({
      operationId: op.operation_id,
      containerName: op.container_name,
      oldVersion: op.old_version,
      newVersion: op.new_version,
    });
  };

  const cancelRollback = () => {
    setRollbackConfirm(null);
  };

  const executeRollback = async () => {
    if (!rollbackConfirm) return;

    const { operationId, containerName } = rollbackConfirm;
    setRollbackConfirm(null);
    clearEvents();

    // Initialize rollback progress
    setRollbackProgress({
      containerName,
      status: 'in_progress',
      startTime: Date.now(),
      logs: [{ time: Date.now(), message: `Starting rollback of ${containerName}...` }],
    });
    setElapsedTime(0);

    try {
      const response = await fetch('/api/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ operation_id: operationId }),
      });
      const data = await response.json();

      if (data.success) {
        setRollbackProgress(prev => {
          if (!prev) return prev;
          return {
            ...prev,
            operationId: data.data?.operation_id,
            message: 'Rollback triggered, waiting for completion...',
            logs: [...prev.logs, { time: Date.now(), message: `Rollback operation started (${data.data?.operation_id?.slice(0, 8)}...)` }],
          };
        });

        // Poll for completion
        const rollbackOpId = data.data?.operation_id;
        if (rollbackOpId) {
          let completed = false;
          let pollCount = 0;
          const maxPolls = 60;

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 5000));
            pollCount++;

            try {
              const opResponse = await fetch(`/api/operations/${rollbackOpId}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const op = opData.data;
                setRollbackProgress(prev => {
                  if (!prev) return prev;
                  return { ...prev, message: `Status: ${op.status}` };
                });

                if (op.status === 'complete') {
                  completed = true;
                  setRollbackProgress(prev => {
                    if (!prev) return prev;
                    return {
                      ...prev,
                      status: 'success',
                      message: 'Rollback completed successfully',
                      logs: [...prev.logs, { time: Date.now(), message: `✓ Rollback complete` }],
                    };
                  });
                } else if (op.status === 'failed') {
                  completed = true;
                  setRollbackProgress(prev => {
                    if (!prev) return prev;
                    return {
                      ...prev,
                      status: 'failed',
                      message: 'Rollback failed',
                      error: op.error_message,
                      logs: [...prev.logs, { time: Date.now(), message: `✗ ${op.error_message}` }],
                    };
                  });
                }
              }
            } catch {
              // Continue polling
            }
          }

          if (!completed) {
            setRollbackProgress(prev => {
              if (!prev) return prev;
              return {
                ...prev,
                status: 'failed',
                message: 'Timed out waiting for completion',
                logs: [...prev.logs, { time: Date.now(), message: `✗ Timed out` }],
              };
            });
          }
        } else {
          // No operation ID, mark as complete without polling
          setRollbackProgress(prev => {
            if (!prev) return prev;
            return {
              ...prev,
              status: 'success',
              message: 'Rollback initiated',
              logs: [...prev.logs, { time: Date.now(), message: `✓ Rollback complete` }],
            };
          });
        }
      } else {
        setRollbackProgress(prev => {
          if (!prev) return prev;
          return {
            ...prev,
            status: 'failed',
            message: 'Failed to trigger rollback',
            error: data.error,
            logs: [...prev.logs, { time: Date.now(), message: `✗ ${data.error}` }],
          };
        });
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      setRollbackProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          status: 'failed',
          message: 'Error',
          error: errorMsg,
          logs: [...prev.logs, { time: Date.now(), message: `✗ ${errorMsg}` }],
        };
      });
    }
  };

  const getStageIcon = (stage: string): string => {
    switch (stage) {
      case 'validating': return '🔍';
      case 'backup': return '💾';
      case 'updating_compose': return '📝';
      case 'pulling_image': return '⬇️';
      case 'recreating': return '🔄';
      case 'health_check': return '❤️';
      case 'rolling_back': return '⏪';
      case 'complete': return '✅';
      case 'failed': return '❌';
      default: return '⏳';
    }
  };

  const formatTime = (timeStr?: string) => {
    if (!timeStr) return '-';
    const date = new Date(timeStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    // Relative time for recent operations
    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;

    return date.toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  };

  const formatDuration = (startedAt?: string, completedAt?: string, createdAt?: string) => {
    const start = startedAt ? new Date(startedAt).getTime() : (createdAt ? new Date(createdAt).getTime() : 0);
    const end = completedAt ? new Date(completedAt).getTime() : 0;
    if (!start || !end) return '-';

    const durationMs = end - start;
    if (durationMs < 1000) return `${durationMs}ms`;
    if (durationMs < 60000) return `${(durationMs / 1000).toFixed(0)}s`;
    return `${(durationMs / 60000).toFixed(1)}m`;
  };

  const getStatusIcon = (status: string, rollback: boolean) => {
    if (rollback) return '⏪';
    switch (status) {
      case 'complete': return '✓';
      case 'failed': return '✗';
      default: return '○';
    }
  };

  const getStatusClass = (status: string) => {
    switch (status) {
      case 'complete': return 'status-success';
      case 'failed': return 'status-failed';
      default: return 'status-pending';
    }
  };

  const filteredOperations = operations.filter(op => {
    if (filter === 'all') return true;
    if (filter === 'complete') return op.status === 'complete';
    if (filter === 'failed') return op.status === 'failed' || op.rollback_occurred;
    return true;
  });

  if (loading) {
    return (
      <div className="history-page">
        <header>
          <div className="header-top">
            <button onClick={onBack} className="back-btn">&larr;</button>
            <h1>History</h1>
          </div>
        </header>
        <div className="loading">
          <div className="spinner"></div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="history-page">
        <header>
          <div className="header-top">
            <button onClick={onBack} className="back-btn">&larr;</button>
            <h1>History</h1>
          </div>
        </header>
        <div className="error">
          <p>{error}</p>
          <button onClick={fetchOperations}>Retry</button>
        </div>
      </div>
    );
  }

  return (
    <div className="history-page">
      <header>
        <div className="header-top">
          <button onClick={onBack} className="back-btn">&larr;</button>
          <h1>History</h1>
          <button onClick={fetchOperations} className="refresh-btn">
            Refresh
          </button>
        </div>
        <div className="history-stats">
          <span className="stat">{operations.length} <small>total</small></span>
          <span className="stat">{operations.filter(o => o.status === 'complete').length} <small>ok</small></span>
          <span className="stat">{operations.filter(o => o.status === 'failed').length} <small>fail</small></span>
        </div>
        <div className="filter-tabs">
          <button
            className={filter === 'all' ? 'active' : ''}
            onClick={() => setFilter('all')}
          >
            All
          </button>
          <button
            className={filter === 'complete' ? 'active' : ''}
            onClick={() => setFilter('complete')}
          >
            Success
          </button>
          <button
            className={filter === 'failed' ? 'active' : ''}
            onClick={() => setFilter('failed')}
          >
            Failed
          </button>
        </div>
      </header>

      <main className="history-list">
        {filteredOperations.length === 0 ? (
          <div className="empty">No operations found</div>
        ) : (
          filteredOperations.map((op) => (
            <div
              key={op.operation_id}
              className={`operation-card ${getStatusClass(op.status)} ${expandedOp === op.operation_id ? 'expanded' : ''}`}
              onClick={() => setExpandedOp(expandedOp === op.operation_id ? null : op.operation_id)}
            >
              <div className="operation-summary">
                <div className="op-main">
                  <span className={`op-status-icon ${getStatusClass(op.status)}`}>
                    {getStatusIcon(op.status, op.rollback_occurred)}
                  </span>
                  <span className="op-container">{op.container_name}</span>
                  {op.operation_type === 'rollback' && (
                    <span className="op-type-badge rollback">ROLLBACK</span>
                  )}
                  {op.rollback_occurred && (
                    <span className="op-type-badge rolled-back">ROLLED BACK</span>
                  )}
                  {op.new_version && (
                    <span className="op-version">
                      {op.old_version ? (
                        op.old_version !== op.new_version ? (
                          <>{op.old_version} → {op.new_version}</>
                        ) : (
                          <>{op.new_version}</>
                        )
                      ) : (
                        <>→ {op.new_version}</>
                      )}
                    </span>
                  )}
                </div>
                <div className="op-meta">
                  <span className="op-id-mini" title="Click to copy" onClick={(e) => {
                    e.stopPropagation();
                    navigator.clipboard.writeText(op.operation_id);
                  }}>{op.operation_id.slice(0, 8)}</span>
                  <span className="op-time">{formatTime(op.completed_at || op.created_at)}</span>
                  <span className="op-duration">{formatDuration(op.started_at, op.completed_at, op.created_at)}</span>
                </div>
              </div>

              {expandedOp === op.operation_id && (
                <div className="operation-expanded">
                  <div className="op-detail-grid">
                    <div className="op-detail">
                      <span className="label">Stack</span>
                      <span className="value">{op.stack_name || 'standalone'}</span>
                    </div>
                    <div className="op-detail">
                      <span className="label">Type</span>
                      <span className="value">{op.operation_type}</span>
                    </div>
                    <div className="op-detail">
                      <span className="label">Status</span>
                      <span className={`value ${getStatusClass(op.status)}`}>{op.status}</span>
                    </div>
                    {op.completed_at && (
                      <div className="op-detail">
                        <span className="label">Completed</span>
                        <span className="value">{new Date(op.completed_at).toLocaleString()}</span>
                      </div>
                    )}
                  </div>

                  {op.error_message && (
                    <div className="op-error">
                      <span className="error-label">Error:</span>
                      <span className="error-msg">{op.error_message}</span>
                    </div>
                  )}

                  <div className="op-actions">
                    {op.status === 'complete' && !op.rollback_occurred && (
                      <button
                        className="rollback-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          showRollbackConfirm(op);
                        }}
                      >
                        ⏪ Rollback
                      </button>
                    )}
                    <span className="op-id">ID: {op.operation_id.slice(0, 12)}</span>
                  </div>
                </div>
              )}
            </div>
          ))
        )}
      </main>

      {/* Rollback Confirmation Dialog */}
      {rollbackConfirm && (
        <div className="confirm-dialog-overlay">
          <div className="confirm-dialog">
            <div className="confirm-dialog-header">
              <h3>Confirm Rollback</h3>
            </div>
            <div className="confirm-dialog-body">
              <p>Roll back <strong>{rollbackConfirm.containerName}</strong> to its previous version?</p>
              {rollbackConfirm.newVersion && rollbackConfirm.oldVersion && (
                <div className="confirm-version-change">
                  <span className="version-current">{rollbackConfirm.newVersion}</span>
                  <span className="version-arrow">→</span>
                  <span className="version-target">{rollbackConfirm.oldVersion}</span>
                </div>
              )}
              <p className="confirm-warning">This will recreate the container with the previous image.</p>
            </div>
            <div className="confirm-dialog-actions">
              <button className="confirm-cancel" onClick={cancelRollback}>Cancel</button>
              <button className="confirm-proceed" onClick={executeRollback}>Rollback</button>
            </div>
          </div>
        </div>
      )}

      {/* Rollback Progress Modal */}
      {rollbackProgress && (
        <div className="update-progress-overlay">
          <div className="update-progress-modal tui-style">
            <div className="update-progress-header">
              <h3>Rolling Back Container</h3>
            </div>

            <div className="update-overall-stats">
              <span>Container: {rollbackProgress.containerName}</span>
              <span>Status: {rollbackProgress.status}</span>
              <span>Elapsed: {elapsedTime}s</span>
            </div>

            <div className="update-container-list">
              <div className={`update-container-item status-${rollbackProgress.status}`}>
                <span className="status-icon">
                  {rollbackProgress.status === 'pending' && '○'}
                  {rollbackProgress.status === 'in_progress' && '◐'}
                  {rollbackProgress.status === 'success' && '✓'}
                  {rollbackProgress.status === 'failed' && '✗'}
                </span>
                <span className="container-name">{rollbackProgress.containerName}</span>
                {rollbackProgress.message && (
                  <span className="container-message">- {rollbackProgress.message}</span>
                )}
                {rollbackProgress.error && (
                  <div className="container-error">Error: {rollbackProgress.error}</div>
                )}
              </div>
            </div>

            {/* Real-time SSE progress */}
            {progressEvent && rollbackProgress.status === 'in_progress' && (
              <div className="current-operation-progress">
                <div className="update-progress-bar">
                  <div
                    className="update-progress-bar-fill"
                    style={{ width: `${progressEvent.progress ?? progressEvent.percent ?? 0}%` }}
                  />
                  <span className="update-progress-bar-text">{progressEvent.progress ?? progressEvent.percent ?? 0}%</span>
                </div>
                <div className="update-progress-stage">
                  {getStageIcon(progressEvent.stage)} {progressEvent.message}
                </div>
              </div>
            )}

            {/* Activity log */}
            <div className="update-activity-log">
              <div className="log-header">Activity:</div>
              <div className="log-entries">
                {rollbackProgress.logs.slice(-10).map((log, i) => (
                  <div key={i} className="log-entry">
                    <span className="log-time">
                      [{new Date(log.time).toLocaleTimeString('en-US', { hour12: false })}]
                    </span>
                    <span className="log-message">{log.message}</span>
                  </div>
                ))}
              </div>
            </div>

            {/* Completion message */}
            {(rollbackProgress.status === 'success' || rollbackProgress.status === 'failed') && (
              <div className="update-completion">
                {rollbackProgress.status === 'success' ? (
                  <div className="completion-success">✓ Rollback completed successfully!</div>
                ) : (
                  <div className="completion-error">✗ Rollback failed</div>
                )}
                <button className="close-btn" onClick={() => {
                  setRollbackProgress(null);
                  fetchOperations();
                }}>
                  Close
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
