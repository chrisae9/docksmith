import { useState, useEffect } from 'react';
import { getOperations, checkContainers } from '../api/client';
import type { UpdateOperation } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';
import { formatTimeWithDate } from '../utils/time';
import { useElapsedTime } from '../hooks/useElapsedTime';
import { useAutoScrollLogs } from '../hooks/useAutoScrollLogs';
import { useProgressEventLogger } from '../hooks/useProgressEventLogger';
import { useAutoRefreshOnClose } from '../hooks/useAutoRefreshOnClose';
import { ProgressModal } from './ProgressModal';
import type { ProgressModalStatCard } from './ProgressModal';

interface HistoryProps {
  onBack: () => void;
}

interface RollbackConfirmation {
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
}

export function History({ onBack: _onBack }: HistoryProps) {
  const [operations, setOperations] = useState<UpdateOperation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<'all' | 'complete' | 'failed'>('all');
  const [searchQuery, setSearchQuery] = useState('');
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

  const { lastEvent: progressEvent, clearEvents } = useEventStream(true);

  useEffect(() => {
    fetchOperations();
  }, []);

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

  // Custom hooks to replace duplicate patterns
  const isRollingBack = !!(rollbackProgress && rollbackProgress.status === 'in_progress');
  const elapsedTime = useElapsedTime(rollbackProgress?.startTime ?? null, isRollingBack);
  const logEntriesRef = useAutoScrollLogs(rollbackProgress?.logs);
  useProgressEventLogger(progressEvent, rollbackProgress, setRollbackProgress);
  useAutoRefreshOnClose(!!rollbackProgress, () => {
    fetchOperations();
    checkContainers();
  });

  const getStageIcon = (stage: string): React.ReactNode => {
    switch (stage) {
      case 'validating':
        return <i className="fa-solid fa-magnifying-glass"></i>;
      case 'backup':
        return <i className="fa-solid fa-floppy-disk"></i>;
      case 'updating_compose':
        return <i className="fa-solid fa-file-pen"></i>;
      case 'pulling_image':
        return <i className="fa-solid fa-cloud-arrow-down"></i>;
      case 'recreating':
        return <i className="fa-solid fa-rotate"></i>;
      case 'health_check':
        return <i className="fa-solid fa-heart-pulse"></i>;
      case 'rolling_back':
        return <i className="fa-solid fa-rotate-left"></i>;
      case 'complete':
        return <i className="fa-solid fa-circle-check"></i>;
      case 'failed':
        return <i className="fa-solid fa-circle-xmark"></i>;
      default:
        return <i className="fa-solid fa-hourglass-half"></i>;
    }
  };

  // Alias for consistency - using shared utility from utils/time.ts
  const formatTime = formatTimeWithDate;

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
    if (rollback) return <i className="fa-solid fa-rotate-left"></i>;
    switch (status) {
      case 'complete': return <i className="fa-solid fa-check"></i>;
      case 'failed': return <i className="fa-solid fa-xmark"></i>;
      default: return <i className="fa-regular fa-circle"></i>;
    }
  };

  const getStatusClass = (status: string, rollback: boolean = false) => {
    if (rollback) return 'status-rollback';
    switch (status) {
      case 'complete': return 'status-success';
      case 'failed': return 'status-failed';
      default: return 'status-pending';
    }
  };

  const filteredOperations = operations.filter(op => {
    // Status filter
    if (filter === 'complete' && op.status !== 'complete') return false;
    if (filter === 'failed' && !(op.status === 'failed' || op.rollback_occurred)) return false;

    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      const matchesContainer = op.container_name?.toLowerCase().includes(query);
      const matchesStack = op.stack_name?.toLowerCase().includes(query);
      const matchesId = op.operation_id.toLowerCase().includes(query);
      const matchesOldVersion = op.old_version?.toLowerCase().includes(query);
      const matchesNewVersion = op.new_version?.toLowerCase().includes(query);

      if (!matchesContainer && !matchesStack && !matchesId && !matchesOldVersion && !matchesNewVersion) {
        return false;
      }
    }

    return true;
  });

  if (loading) {
    return (
      <div className="history-page">
        <header>
          <div className="header-top">
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
          <h1>History</h1>
        </div>
        <div className="search-bar">
          <div className="search-container">
            <i className="fa-solid fa-magnifying-glass search-icon"></i>
            <input
              type="text"
              className="search-input"
              placeholder="Search operations..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
            {searchQuery && (
              <button
                className="search-clear"
                onClick={() => setSearchQuery('')}
              >
                <i className="fa-solid fa-xmark"></i>
              </button>
            )}
          </div>
        </div>
        <div className="filter-toolbar">
          <div className="segmented-control">
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
        </div>
      </header>

      <main className="history-list">
        {filteredOperations.length === 0 ? (
          <div className="empty">No operations found</div>
        ) : (
          filteredOperations.map((op) => (
            <div
              key={op.operation_id}
              className={`operation-card ${getStatusClass(op.status, op.rollback_occurred)} ${expandedOp === op.operation_id ? 'expanded' : ''}`}
              onClick={() => setExpandedOp(expandedOp === op.operation_id ? null : op.operation_id)}
            >
              <div className="operation-summary">
                <div className="op-main">
                  <span className={`op-status-icon ${getStatusClass(op.status, op.rollback_occurred)}`}>
                    {getStatusIcon(op.status, op.rollback_occurred)}
                  </span>
                  <span className="op-container">
                    {op.operation_type === 'batch' ? (
                      <>{op.stack_name || 'Batch'} <span className="op-type-badge batch">BATCH</span></>
                    ) : (
                      op.container_name || op.stack_name || 'Unknown'
                    )}
                  </span>
                  {op.operation_type === 'rollback' && (
                    <span className="op-type-badge rollback">ROLLBACK</span>
                  )}
                  {op.rollback_occurred && (
                    <span className="op-type-badge rolled-back">ROLLED BACK</span>
                  )}
                </div>
                <div className="op-info">
                  {op.operation_type === 'batch' && op.error_message && (
                    <span className="op-batch-summary">{op.error_message.replace('Batch update completed: ', '')}</span>
                  )}
                  {op.operation_type !== 'batch' && op.new_version && (
                    <span className="op-version">
                      {op.old_version && op.old_version !== op.new_version ? (
                        <>{op.old_version} → {op.new_version}</>
                      ) : (
                        <>{op.new_version}</>
                      )}
                    </span>
                  )}
                </div>
                <div className="op-meta">
                  <button
                    className="op-copy-btn"
                    title={`Copy ID: ${op.operation_id}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      navigator.clipboard.writeText(op.operation_id);
                      const btn = e.currentTarget;
                      btn.classList.add('copied');
                      setTimeout(() => btn.classList.remove('copied'), 1500);
                    }}
                  >
                    <i className="fa-regular fa-copy"></i>
                    <span className="op-id-short">{op.operation_id.slice(0, 8)}</span>
                  </button>
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
                    {(op.status === 'complete' || op.status === 'failed') && !op.rollback_occurred && (
                      <button
                        className="rollback-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          showRollbackConfirm(op);
                        }}
                      >
                        <i className="fa-solid fa-rotate-left"></i> Rollback
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
      {rollbackProgress && (() => {
        const inProgress = rollbackProgress.status === 'in_progress';
        const allSuccess = rollbackProgress.status === 'success';
        const allFailed = rollbackProgress.status === 'failed';

        const stageIcon = inProgress ? (
          <div className="stage-icon-wrapper">
            <i className="fa-solid fa-rotate-left"></i>
            <div className="spinner-ring"></div>
          </div>
        ) : allSuccess ? (
          <i className="fa-solid fa-circle-check"></i>
        ) : (
          <i className="fa-solid fa-circle-xmark"></i>
        );

        const stageVariant = inProgress ? 'in-progress' : allSuccess ? 'complete' : 'complete-with-errors';

        const stageMessage = inProgress
          ? `Rolling back ${rollbackProgress.containerName}...`
          : allSuccess
          ? 'Rollback completed successfully!'
          : 'Rollback failed';

        const stats: ProgressModalStatCard[] = [
          {
            label: 'Container',
            value: rollbackProgress.containerName,
          },
          {
            label: 'Status',
            value: rollbackProgress.status.replace('_', ' ').charAt(0).toUpperCase() + rollbackProgress.status.replace('_', ' ').slice(1),
            variant: allSuccess ? 'success' : allFailed ? 'error' : 'default',
          },
          {
            label: 'Elapsed',
            value: `${elapsedTime}s`,
          },
        ];

        const currentProgress = progressEvent && inProgress ? {
          event: progressEvent,
          getStageIcon,
        } : undefined;

        return (
          <ProgressModal
            title="Rolling Back Container"
            stageIcon={stageIcon}
            stageVariant={stageVariant}
            stageMessage={stageMessage}
            stats={stats}
            currentProgress={currentProgress}
            logs={rollbackProgress.logs}
            logEntriesRef={logEntriesRef}
            buttonText={inProgress ? 'Rolling back...' : 'Close'}
            buttonDisabled={inProgress}
            onClose={() => setRollbackProgress(null)}
          />
        );
      })()}
    </div>
  );
}
