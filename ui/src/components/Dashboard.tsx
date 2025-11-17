import { useState, useEffect, useRef } from 'react';
import { checkContainers, triggerBatchUpdate } from '../api/client';
import type { DiscoveryResult, ContainerInfo, Stack } from '../types/api';
import { ChangeType } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';

type FilterType = 'all' | 'updates' | 'current' | 'local';
type SortType = 'stack' | 'name' | 'status';

interface DashboardProps {
  onNavigateToHistory?: () => void;
}

export function Dashboard({ onNavigateToHistory }: DashboardProps) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const [collapsedStacks, setCollapsedStacks] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState<FilterType>('all');
  const [sort, setSort] = useState<SortType>('stack');
  const [showLocalImages, setShowLocalImages] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [updateStatus, setUpdateStatus] = useState<string | null>(null);
  const [elapsedTime, setElapsedTime] = useState(0);
  const [updateProgress, setUpdateProgress] = useState<{
    containers: Array<{
      name: string;
      status: 'pending' | 'in_progress' | 'success' | 'failed';
      message?: string;
      error?: string;
      operationId?: string;
    }>;
    currentIndex: number;
    startTime: number;
    logs: Array<{ time: number; message: string }>;
  } | null>(null);
  const elapsedIntervalRef = useRef<number | null>(null);

  // Connect to SSE for real-time progress (always connected for check progress)
  const { lastEvent: progressEvent, checkProgress, clearEvents } = useEventStream(true);

  // Update elapsed time every second when update is in progress
  useEffect(() => {
    if (updateProgress && !updateProgress.containers.every(c => c.status === 'success' || c.status === 'failed')) {
      elapsedIntervalRef.current = window.setInterval(() => {
        setElapsedTime(Math.floor((Date.now() - updateProgress.startTime) / 1000));
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
  }, [updateProgress]);

  // Add SSE progress events to activity log
  useEffect(() => {
    if (progressEvent && updateProgress) {
      setUpdateProgress(prev => {
        if (!prev) return prev;
        const newLog = {
          time: progressEvent.timestamp ? progressEvent.timestamp * 1000 : Date.now(),
          message: `${progressEvent.container_name}: ${progressEvent.message}`,
        };
        // Avoid duplicate logs
        const lastLog = prev.logs[prev.logs.length - 1];
        if (lastLog && lastLog.message === newLog.message) {
          return prev;
        }
        return {
          ...prev,
          logs: [...prev.logs.slice(-19), newLog], // Keep last 20 logs
        };
      });
    }
  }, [progressEvent, updateProgress]);

  const fetchData = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await checkContainers();
      if (response.success && response.data) {
        setResult(response.data);
      } else {
        setError(response.error || 'Failed to fetch data');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const handleUpdate = async () => {
    if (selectedContainers.size === 0 || !result) return;

    setUpdating(true);
    setUpdateStatus(null);
    clearEvents(); // Clear previous events

    const containerNames = Array.from(selectedContainers);

    // Initialize progress tracking for all containers
    const initialProgress = {
      containers: containerNames.map(name => ({
        name,
        status: 'pending' as const,
      })),
      currentIndex: 0,
      startTime: Date.now(),
      logs: [{ time: Date.now(), message: `Starting stack-aware update of ${containerNames.length} container(s)` }],
    };
    setUpdateProgress(initialProgress);

    // Build container info with stack grouping
    const containersToUpdate = containerNames.map(name => {
      const container = result.containers.find(c => c.container_name === name);
      return {
        name,
        target_version: container?.latest_version || '',
        stack: container?.stack || '',
      };
    });

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
    setUpdateProgress(prev => {
      if (!prev) return prev;
      const logs = [...prev.logs];
      for (const [stack, containers] of stackGroups) {
        const stackName = stack === '__standalone__' ? 'standalone' : stack;
        logs.push({ time: Date.now(), message: `Stack "${stackName}": ${containers.join(', ')}` });
      }
      return { ...prev, logs };
    });

    try {
      // Send batch update request
      const response = await triggerBatchUpdate(containersToUpdate);

      if (!response.success) {
        setUpdateProgress(prev => {
          if (!prev) return prev;
          const newContainers = prev.containers.map(c => ({
            ...c,
            status: 'failed' as const,
            message: 'Batch update failed',
            error: response.error,
          }));
          return {
            ...prev,
            containers: newContainers,
            logs: [...prev.logs, { time: Date.now(), message: `✗ Batch update failed: ${response.error}` }],
          };
        });
        setUpdating(false);
        return;
      }

      // Mark containers as in progress based on their stack operations
      const operations = response.data?.operations || [];
      const containerToOpId = new Map<string, string>();

      for (const op of operations) {
        if (op.status === 'started' && op.operation_id) {
          for (const containerName of op.containers) {
            containerToOpId.set(containerName, op.operation_id);
          }
          setUpdateProgress(prev => {
            if (!prev) return prev;
            const newContainers = prev.containers.map(c => {
              if (op.containers.includes(c.name)) {
                return { ...c, status: 'in_progress' as const, message: 'Update in progress...', operationId: op.operation_id };
              }
              return c;
            });
            return {
              ...prev,
              containers: newContainers,
              logs: [...prev.logs, { time: Date.now(), message: `Stack "${op.stack}": Operation ${op.operation_id?.slice(0, 8)}... started` }],
            };
          });
        } else if (op.status === 'failed') {
          setUpdateProgress(prev => {
            if (!prev) return prev;
            const newContainers = prev.containers.map(c => {
              if (op.containers.includes(c.name)) {
                return { ...c, status: 'failed' as const, message: 'Failed to start', error: op.error };
              }
              return c;
            });
            return {
              ...prev,
              containers: newContainers,
              logs: [...prev.logs, { time: Date.now(), message: `Stack "${op.stack}": ✗ ${op.error}` }],
            };
          });
        }
      }

      // Poll for completion of all operations
      const uniqueOpIds = new Set<string>();
      for (const opId of containerToOpId.values()) {
        uniqueOpIds.add(opId);
      }

      // Helper to poll a single operation
      const pollOperation = async (operationId: string) => {
        let completed = false;
        let pollCount = 0;
        const maxPolls = 60; // 5 minutes with 5 second intervals

        while (!completed && pollCount < maxPolls) {
          await new Promise(resolve => setTimeout(resolve, 5000));
          pollCount++;

          try {
            const opResponse = await fetch(`/api/operations/${operationId}`);
            const opData = await opResponse.json();

            if (opData.success && opData.data) {
              const op = opData.data;

              // Find all containers for this operation
              const affectedContainers: string[] = [];
              for (const [containerName, opId] of containerToOpId) {
                if (opId === operationId) {
                  affectedContainers.push(containerName);
                }
              }

              if (op.status === 'complete') {
                completed = true;
                setUpdateProgress(prev => {
                  if (!prev) return prev;
                  const newContainers = prev.containers.map(c => {
                    if (affectedContainers.includes(c.name)) {
                      return { ...c, status: 'success' as const, message: 'Updated successfully' };
                    }
                    return c;
                  });
                  return {
                    ...prev,
                    containers: newContainers,
                    logs: [...prev.logs, { time: Date.now(), message: `Operation ${operationId.slice(0, 8)}...: ✓ Complete (${affectedContainers.join(', ')})` }],
                  };
                });
              } else if (op.status === 'failed') {
                completed = true;
                setUpdateProgress(prev => {
                  if (!prev) return prev;
                  const newContainers = prev.containers.map(c => {
                    if (affectedContainers.includes(c.name)) {
                      return { ...c, status: 'failed' as const, message: 'Update failed', error: op.error_message };
                    }
                    return c;
                  });
                  return {
                    ...prev,
                    containers: newContainers,
                    logs: [...prev.logs, { time: Date.now(), message: `Operation ${operationId.slice(0, 8)}...: ✗ ${op.error_message}` }],
                  };
                });
              } else {
                // Update status message
                setUpdateProgress(prev => {
                  if (!prev) return prev;
                  const newContainers = prev.containers.map(c => {
                    if (affectedContainers.includes(c.name)) {
                      return { ...c, message: `Status: ${op.status}` };
                    }
                    return c;
                  });
                  return { ...prev, containers: newContainers };
                });
              }
            }
          } catch {
            // Continue polling on error
          }
        }

        if (!completed) {
          // Find all containers for this operation
          const affectedContainers: string[] = [];
          for (const [containerName, opId] of containerToOpId) {
            if (opId === operationId) {
              affectedContainers.push(containerName);
            }
          }
          setUpdateProgress(prev => {
            if (!prev) return prev;
            const newContainers = prev.containers.map(c => {
              if (affectedContainers.includes(c.name)) {
                return { ...c, status: 'failed' as const, message: 'Timed out waiting for completion' };
              }
              return c;
            });
            return {
              ...prev,
              containers: newContainers,
              logs: [...prev.logs, { time: Date.now(), message: `Operation ${operationId.slice(0, 8)}...: ✗ Timed out` }],
            };
          });
        }
      };

      // Poll all operations in parallel
      await Promise.all(Array.from(uniqueOpIds).map(opId => pollOperation(opId)));

    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      setUpdateProgress(prev => {
        if (!prev) return prev;
        const newContainers = prev.containers.map(c => ({
          ...c,
          status: 'failed' as const,
          message: 'Error',
          error: errorMsg,
        }));
        return {
          ...prev,
          containers: newContainers,
          logs: [...prev.logs, { time: Date.now(), message: `✗ ${errorMsg}` }],
        };
      });
    }

    // Add completion log entry
    setUpdateProgress(prev => {
      if (!prev) return prev;
      const successful = prev.containers.filter(c => c.status === 'success').length;
      const failed = prev.containers.filter(c => c.status === 'failed').length;
      return {
        ...prev,
        logs: [...prev.logs, {
          time: Date.now(),
          message: `Update complete: ${successful} succeeded, ${failed} failed`,
        }],
      };
    });

    setSelectedContainers(new Set());
    setUpdating(false);
    // Don't auto-close the modal - let user close it manually
  };

  useEffect(() => {
    fetchData();
  }, []);

  const toggleContainer = (name: string) => {
    setSelectedContainers(prev => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const toggleStack = (name: string) => {
    setCollapsedStacks(prev => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const filterContainer = (container: ContainerInfo) => {
    // Hide local images unless explicitly showing them or filtering for them
    if (container.status === 'LOCAL_IMAGE' && !showLocalImages && filter !== 'local') {
      return false;
    }
    switch (filter) {
      case 'updates':
        return container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED' || container.status === 'UP_TO_DATE_PINNABLE';
      case 'current':
        return container.status === 'UP_TO_DATE';
      case 'local':
        return container.status === 'LOCAL_IMAGE';
      default:
        return true;
    }
  };

  if (loading) {
    return (
      <div className="loading">
        <div className="loading-content">
          <div className="spinner"></div>
          {checkProgress && (
            <div className="check-progress">
              <div className="check-progress-bar">
                <div
                  className="check-progress-bar-fill"
                  style={{ width: `${checkProgress.percent}%` }}
                />
                <span className="check-progress-bar-text">
                  {checkProgress.checked}/{checkProgress.total}
                </span>
              </div>
              <div className="check-progress-message">
                {checkProgress.message}
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="error">
        <p>{error}</p>
        <button onClick={fetchData}>Retry</button>
      </div>
    );
  }

  if (!result) {
    return <div className="empty">No containers found</div>;
  }

  const getStageIcon = (stage: string): string => {
    switch (stage) {
      case 'validating':
        return '🔍';
      case 'backup':
        return '💾';
      case 'updating_compose':
        return '📝';
      case 'pulling_image':
        return '⬇️';
      case 'recreating':
        return '🔄';
      case 'health_check':
        return '❤️';
      case 'rolling_back':
        return '⏪';
      case 'complete':
        return '✅';
      case 'failed':
        return '❌';
      default:
        return '⏳';
    }
  };

  return (
    <div className="dashboard">
      <header>
        <div className="header-top">
          <h1>Docksmith</h1>
          <div className="header-actions">
            {onNavigateToHistory && (
              <button onClick={onNavigateToHistory} className="history-btn">
                History
              </button>
            )}
            <button onClick={fetchData} className="refresh-btn">
              Refresh
            </button>
          </div>
        </div>
        <div className="stats-bar">
          <span className="stat">{result.updates_found + result.containers.filter(c => c.status === 'UP_TO_DATE_PINNABLE').length} <small>updates</small></span>
          <span className="stat">{result.up_to_date - result.containers.filter(c => c.status === 'UP_TO_DATE_PINNABLE').length} <small>current</small></span>
          <span className="stat">{result.total_checked} <small>total</small></span>
        </div>
      </header>

      <main>
        {sort === 'stack' ? (
          <>
            {Object.values(result.stacks).map((stack: Stack) => {
              const filteredContainers = stack.containers.filter(filterContainer);
              if (filteredContainers.length === 0) return null;

              return (
                <section key={stack.name} className="stack">
                  <h2 onClick={() => toggleStack(stack.name)}>
                    <span className="toggle">{collapsedStacks.has(stack.name) ? '▸' : '▾'}</span>
                    {stack.name}
                    {stack.has_updates && <span className="badge-dot"></span>}
                  </h2>
                  {!collapsedStacks.has(stack.name) && (
                    <ul>
                      {filteredContainers.map((container) => (
                        <ContainerRow
                          key={container.container_name}
                          container={container}
                          selected={selectedContainers.has(container.container_name)}
                          onToggle={() => toggleContainer(container.container_name)}
                        />
                      ))}
                    </ul>
                  )}
                </section>
              );
            })}

            {result.standalone_containers.filter(filterContainer).length > 0 && (
              <section className="stack">
                <h2 onClick={() => toggleStack('__standalone__')}>
                  <span className="toggle">{collapsedStacks.has('__standalone__') ? '▸' : '▾'}</span>
                  Standalone
                </h2>
                {!collapsedStacks.has('__standalone__') && (
                  <ul>
                    {result.standalone_containers.filter(filterContainer).map((container) => (
                      <ContainerRow
                        key={container.container_name}
                        container={container}
                        selected={selectedContainers.has(container.container_name)}
                        onToggle={() => toggleContainer(container.container_name)}
                      />
                    ))}
                  </ul>
                )}
              </section>
            )}
          </>
        ) : (
          <section className="stack">
            <ul>
              {result.containers
                .filter(filterContainer)
                .sort((a, b) => {
                  if (sort === 'name') return a.container_name.localeCompare(b.container_name);
                  return 0;
                })
                .map((container) => (
                  <ContainerRow
                    key={container.container_name}
                    container={container}
                    selected={selectedContainers.has(container.container_name)}
                    onToggle={() => toggleContainer(container.container_name)}
                  />
                ))}
            </ul>
          </section>
        )}
      </main>

      {selectedContainers.size > 0 && (
        <div className="selection-bar">
          <span>{selectedContainers.size} selected</span>
          <button
            className="update-btn"
            onClick={handleUpdate}
            disabled={updating}
          >
            {updating ? 'Updating...' : 'Update'}
          </button>
        </div>
      )}

      {updateStatus && (
        <div className="update-status">
          {updateStatus}
        </div>
      )}

      {updateProgress && (
        <div className="update-progress-overlay">
          <div className="update-progress-modal tui-style">
            <div className="update-progress-header">
              <h3>Updating Containers</h3>
            </div>

            {/* Overall progress stats */}
            <div className="update-overall-stats">
              <span>
                Progress: {updateProgress.containers.filter(c => c.status === 'success' || c.status === 'failed').length}/{updateProgress.containers.length} containers
              </span>
              <span>
                Successful: {updateProgress.containers.filter(c => c.status === 'success').length} |
                Failed: {updateProgress.containers.filter(c => c.status === 'failed').length}
              </span>
              <span>
                Elapsed: {elapsedTime}s
              </span>
            </div>

            {/* Container list with status icons */}
            <div className="update-container-list">
              {updateProgress.containers.map((container, index) => (
                <div key={container.name} className={`update-container-item status-${container.status}`}>
                  <span className="status-icon">
                    {container.status === 'pending' && '○'}
                    {container.status === 'in_progress' && '◐'}
                    {container.status === 'success' && '✓'}
                    {container.status === 'failed' && '✗'}
                  </span>
                  <span className="container-index">{index + 1}.</span>
                  <span className="container-name">{container.name}</span>
                  {container.message && (
                    <span className="container-message">- {container.message}</span>
                  )}
                  {container.error && (
                    <div className="container-error">Error: {container.error}</div>
                  )}
                </div>
              ))}
            </div>

            {/* Real-time SSE progress for current operation */}
            {progressEvent && updateProgress.containers.some(c => c.status === 'in_progress') && (
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
              <div className="log-header">Recent Activity:</div>
              <div className="log-entries">
                {updateProgress.logs.slice(-10).map((log, i) => (
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
            {!updateProgress.containers.some(c => c.status === 'pending' || c.status === 'in_progress') && (
              <div className="update-completion">
                {updateProgress.containers.every(c => c.status === 'success') ? (
                  <div className="completion-success">✓ All updates completed successfully!</div>
                ) : (
                  <div className="completion-error">✗ Updates completed with errors</div>
                )}
                <button className="close-btn" onClick={() => {
                  setUpdateProgress(null);
                  setUpdating(false);
                  fetchData();
                }}>
                  Close
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      <nav className="bottom-nav">
        <button
          className={filter === 'all' ? 'active' : ''}
          onClick={() => setFilter('all')}
        >
          <span className="nav-icon">⊡</span>
          <span>All</span>
        </button>
        <button
          className={filter === 'updates' ? 'active' : ''}
          onClick={() => setFilter('updates')}
        >
          <span className="nav-icon">↑</span>
          <span>Updates</span>
        </button>
        <button
          className={filter === 'current' ? 'active' : ''}
          onClick={() => setFilter('current')}
        >
          <span className="nav-icon">✓</span>
          <span>Current</span>
        </button>
        <button
          className={showLocalImages ? 'active' : ''}
          onClick={() => setShowLocalImages(!showLocalImages)}
        >
          <span className="nav-icon">{showLocalImages ? '◉' : '○'}</span>
          <span>Local</span>
        </button>
        <button
          className={sort === 'stack' ? 'active' : ''}
          onClick={() => setSort(sort === 'stack' ? 'name' : 'stack')}
        >
          <span className="nav-icon">{sort === 'stack' ? '▤' : '≡'}</span>
          <span>{sort === 'stack' ? 'Stacks' : 'List'}</span>
        </button>
      </nav>
    </div>
  );
}

interface ContainerRowProps {
  container: ContainerInfo;
  selected: boolean;
  onToggle: () => void;
}

function ContainerRow({ container, selected, onToggle }: ContainerRowProps) {
  const hasUpdate = container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED' || container.status === 'UP_TO_DATE_PINNABLE';
  const isBlocked = container.status === 'UPDATE_AVAILABLE_BLOCKED';

  const getStatusIndicator = () => {
    switch (container.status) {
      case 'UPDATE_AVAILABLE':
        if (container.change_type === ChangeType.MajorChange) return <span className="dot major" title="Major update"></span>;
        if (container.change_type === ChangeType.MinorChange) return <span className="dot minor" title="Minor update"></span>;
        if (container.change_type === ChangeType.PatchChange) return <span className="dot patch" title="Patch update"></span>;
        return <span className="dot update" title="Update available"></span>;
      case 'UPDATE_AVAILABLE_BLOCKED':
        return <span className="dot blocked" title="Update blocked"></span>;
      case 'UP_TO_DATE':
        return <span className="dot current" title="Up to date"></span>;
      case 'UP_TO_DATE_PINNABLE':
        const pinnableVersion = container.recommended_tag || container.current_version || 'unknown';
        return <span className="dot pinnable" title={`No version tag specified. Pin to: ${container.image}:${pinnableVersion}`}></span>;
      case 'LOCAL_IMAGE':
        return <span className="dot local" title="Local image"></span>;
      case 'IGNORED':
        return <span className="dot ignored" title="Ignored"></span>;
      default:
        return <span className="dot" title={container.status}></span>;
    }
  };

  const getVersion = () => {
    // For pinnable containers, show the tag migration path (check this FIRST)
    if (container.status === 'UP_TO_DATE_PINNABLE' && container.recommended_tag) {
      const currentTag = container.using_latest_tag ? 'latest' : (container.current_tag || 'untagged');
      return `${currentTag} → ${container.recommended_tag}`;
    }
    if (hasUpdate && container.current_version && container.latest_version) {
      return `${container.current_version} → ${container.latest_version}`;
    }
    if (container.status === 'LOCAL_IMAGE') {
      return 'Local image';
    }
    return container.current_tag || container.current_version || '';
  };

  return (
    <li className={`${hasUpdate ? 'has-update' : ''} ${selected ? 'selected' : ''} ${isBlocked ? 'blocked' : ''}`}>
      {hasUpdate && (
        <input
          type="checkbox"
          checked={selected}
          onChange={onToggle}
        />
      )}
      <div className="container-info">
        <span className="name">{container.container_name}</span>
        <span className="version">{getVersion()}</span>
      </div>
      {getStatusIndicator()}
      {container.pre_update_check_pass && <span className="check" title="Pre-update check passed">✓</span>}
      {isBlocked && container.pre_update_check_fail && (
        <span className="warn" title={container.pre_update_check_fail}>⚠</span>
      )}
    </li>
  );
}
