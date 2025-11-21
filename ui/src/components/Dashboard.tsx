import { useState, useEffect, useRef } from 'react';
import { checkContainers, getContainerStatus, triggerBatchUpdate, restartStack } from '../api/client';
import type { DiscoveryResult, ContainerInfo, Stack } from '../types/api';
import { ChangeType } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';
import { ContainerDetailModal } from './ContainerDetailModal';
import { isUpdatable } from '../utils/status';
import { STORAGE_KEY_FILTER, STORAGE_KEY_INITIAL_SWITCH } from '../utils/constants';

type FilterType = 'all' | 'updates' | 'local';
type SortType = 'stack' | 'name' | 'status';

interface DashboardProps {
  onNavigateToHistory?: () => void;
}

export function Dashboard({ onNavigateToHistory: _onNavigateToHistory }: DashboardProps) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const [collapsedStacks, setCollapsedStacks] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState<FilterType>(() => {
    // Try to restore filter from localStorage, or default to 'updates'
    const saved = localStorage.getItem(STORAGE_KEY_FILTER);
    return (saved as FilterType) || 'updates';
  });
  const [sort, setSort] = useState<SortType>('stack');
  const [initialAutoSwitchDone, setInitialAutoSwitchDone] = useState(() => {
    // Check if we've already done the initial auto-switch in this session
    return sessionStorage.getItem(STORAGE_KEY_INITIAL_SWITCH) === 'done';
  });
  const [showLocalImages, setShowLocalImages] = useState(false);
  const [showIgnored, setShowIgnored] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [updating, setUpdating] = useState(false);
  const [updateStatus, setUpdateStatus] = useState<string | null>(null);
  const [elapsedTime, setElapsedTime] = useState(0);
  const [expandedContainer, setExpandedContainer] = useState<string | null>(null);
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
  const [restartingStack, setRestartingStack] = useState<string | null>(null);
  const elapsedIntervalRef = useRef<number | null>(null);
  const logEntriesRef = useRef<HTMLDivElement>(null);

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

  // Auto-scroll logs to bottom when new entries are added
  useEffect(() => {
    if (logEntriesRef.current && updateProgress?.logs) {
      logEntriesRef.current.scrollTop = logEntriesRef.current.scrollHeight;
    }
  }, [updateProgress?.logs]);

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

  // Calculate time ago from ISO timestamp
  // Fetch cached status (for initial load)
  const fetchCachedStatus = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getContainerStatus();
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

  // Background refresh - triggers background check without clearing cache
  const backgroundRefresh = async () => {
    setLoading(true);
    setError(null);
    try {
      await fetch('/api/trigger-check', { method: 'POST' });
      await new Promise(resolve => setTimeout(resolve, 500));
      const response = await getContainerStatus();
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

  // Cache refresh - clears cache and triggers fresh registry queries
  const cacheRefresh = async () => {
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

  const handleSingleUpdate = async (containerName: string) => {
    if (!result) return;

    setUpdating(true);
    setUpdateStatus(null);
    clearEvents(); // Clear previous events

    // Initialize progress tracking for just this container
    const initialProgress = {
      containers: [{
        name: containerName,
        status: 'pending' as const,
      }],
      currentIndex: 0,
      startTime: Date.now(),
      logs: [{ time: Date.now(), message: `Starting update of ${containerName}` }],
    };
    setUpdateProgress(initialProgress);

    // Build container info
    const container = result.containers.find(c => c.container_name === containerName);
    if (!container) {
      setUpdateStatus('Container not found');
      setUpdating(false);
      return;
    }

    const containerToUpdate = {
      name: containerName,
      target_version: container.latest_version || '',
      stack: container.stack || '',
    };

    try {
      // Update progress to in_progress
      setUpdateProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          containers: [{ name: containerName, status: 'in_progress' as const, message: 'Triggering update...' }],
          logs: [...prev.logs, { time: Date.now(), message: `→ Updating ${containerName} to ${containerToUpdate.target_version}` }],
        };
      });

      // Trigger batch update with single container
      const response = await triggerBatchUpdate([containerToUpdate]);

      if (response.success && response.data) {
        const operations = response.data.operations;

        // Update progress with operation IDs
        setUpdateProgress(prev => {
          if (!prev) return prev;
          const op = operations.find(o => o.containers.includes(containerName));
          return {
            ...prev,
            containers: [{
              name: containerName,
              status: 'in_progress' as const,
              message: 'Update triggered, polling for completion...',
              operationId: op?.operation_id,
            }],
            logs: [...prev.logs, { time: Date.now(), message: `✓ Update triggered (${op?.operation_id?.slice(0, 8)}...)` }],
          };
        });

        // Poll for completion
        const op = operations.find(o => o.containers.includes(containerName));
        if (op?.operation_id) {
          let completed = false;
          let pollCount = 0;
          const maxPolls = 120;

          while (!completed && pollCount < maxPolls) {
            await new Promise(resolve => setTimeout(resolve, 5000));
            pollCount++;

            try {
              const opResponse = await fetch(`/api/operations/${op.operation_id}`);
              const opData = await opResponse.json();

              if (opData.success && opData.data) {
                const operation = opData.data;

                if (operation.status === 'complete') {
                  completed = true;
                  setUpdateProgress(prev => {
                    if (!prev) return prev;
                    return {
                      ...prev,
                      containers: [{ name: containerName, status: 'success' as const, message: 'Updated successfully' }],
                      logs: [...prev.logs, { time: Date.now(), message: `✓ ${containerName} updated successfully` }],
                    };
                  });
                } else if (operation.status === 'failed') {
                  completed = true;
                  setUpdateProgress(prev => {
                    if (!prev) return prev;
                    return {
                      ...prev,
                      containers: [{ name: containerName, status: 'failed' as const, message: 'Update failed', error: operation.error_message }],
                      logs: [...prev.logs, { time: Date.now(), message: `✗ ${containerName}: ${operation.error_message}` }],
                    };
                  });
                }
              }
            } catch {
              // Continue polling on error
            }
          }

          if (!completed) {
            setUpdateProgress(prev => {
              if (!prev) return prev;
              return {
                ...prev,
                containers: [{ name: containerName, status: 'failed' as const, message: 'Timed out waiting for completion' }],
                logs: [...prev.logs, { time: Date.now(), message: `✗ ${containerName}: Timed out` }],
              };
            });
          }
        }
      } else {
        throw new Error(response.error || 'Update failed');
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error';
      setUpdateProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          containers: [{ name: containerName, status: 'failed' as const, message: 'Error', error: errorMsg }],
          logs: [...prev.logs, { time: Date.now(), message: `✗ ${errorMsg}` }],
        };
      });
    }

    setUpdating(false);
  };

  const handleStackRestart = async (stackName: string, e: React.MouseEvent) => {
    e.stopPropagation(); // Don't trigger stack collapse/expand

    setRestartingStack(stackName);
    setError(null);

    try {
      const response = await restartStack(stackName);

      if (response.success && response.data) {
        // Success - wait a moment then refresh
        setTimeout(() => {
          setRestartingStack(null);
          backgroundRefresh();
        }, 1000);
      } else {
        setError(response.error || 'Failed to restart stack');
        setRestartingStack(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to restart stack');
      setRestartingStack(null);
    }
  };

  const toggleAllStacks = () => {
    if (!result) return;

    // If any stacks are collapsed, expand all; otherwise collapse all
    if (collapsedStacks.size > 0) {
      setCollapsedStacks(new Set());
    } else {
      const allStackNames = new Set<string>();
      Object.keys(result.stacks).forEach(stackName => allStackNames.add(stackName));
      if (result.standalone_containers.length > 0) {
        allStackNames.add('__standalone__');
      }
      setCollapsedStacks(allStackNames);
    }
  };

  // Load cached status on initial mount
  useEffect(() => {
    fetchCachedStatus();
  }, []);

  // Persist filter changes to localStorage
  useEffect(() => {
    localStorage.setItem(STORAGE_KEY_FILTER, filter);
  }, [filter]);

  // Auto-switch to 'all' tab if on 'updates' but no updates available (only once per session)
  useEffect(() => {
    if (result && !initialAutoSwitchDone && filter === 'updates') {
      const hasUpdates = result.containers.some(c => isUpdatable(c.status));
      if (!hasUpdates) {
        setFilter('all');
      }
      // Mark that we've done the initial auto-switch for this session
      sessionStorage.setItem(STORAGE_KEY_INITIAL_SWITCH, 'done');
      setInitialAutoSwitchDone(true);
    }
  }, [result, initialAutoSwitchDone, filter]);

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

  const selectAll = () => {
    if (!result) return;
    const updatableContainers = result.containers
      .filter(c => isUpdatable(c.status))
      .filter(filterContainer)
      .map(c => c.container_name);
    setSelectedContainers(new Set(updatableContainers));
  };

  const deselectAll = () => {
    setSelectedContainers(new Set());
  };

  const filterContainer = (container: ContainerInfo) => {
    // Apply search filter first
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      const matchesSearch =
        container.container_name.toLowerCase().includes(query) ||
        container.image.toLowerCase().includes(query) ||
        (container.stack && container.stack.toLowerCase().includes(query));
      if (!matchesSearch) {
        return false;
      }
    }
    // Hide local images unless explicitly showing them or filtering for them
    if (container.status === 'LOCAL_IMAGE' && !showLocalImages && filter !== 'local') {
      return false;
    }
    // Hide ignored containers unless explicitly showing them
    if (container.status === 'IGNORED' && !showIgnored) {
      return false;
    }
    switch (filter) {
      case 'updates':
        return isUpdatable(container.status);
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
        <button onClick={cacheRefresh}>Retry</button>
      </div>
    );
  }

  if (!result) {
    return <div className="empty">No containers found</div>;
  }

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

  return (
    <div className="dashboard">
      <header>
        <div className="header-top">
          <h1>Docksmith</h1>
          {result && result.containers.some(c => isUpdatable(c.status)) && (
            <button
              onClick={selectedContainers.size > 0 ? deselectAll : selectAll}
              className="select-all-btn"
            >
              {selectedContainers.size > 0 ? 'Deselect All' : 'Select All'}
            </button>
          )}
        </div>
        <div className="search-bar">
          <i className="fa-solid fa-magnifying-glass search-icon"></i>
          <input
            type="text"
            placeholder="Search containers..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="search-input"
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
        <div className="filter-toolbar">
          <div className="segmented-control">
            <button
              className={filter === 'all' ? 'active' : ''}
              onClick={() => setFilter('all')}
            >
              All
            </button>
            <button
              className={filter === 'updates' ? 'active' : ''}
              onClick={() => setFilter('updates')}
            >
              Updates
            </button>
          </div>
          <div className="toolbar-options">
            {sort === 'stack' && (
              <button
                className="icon-btn"
                onClick={toggleAllStacks}
                title={collapsedStacks.size > 0 ? 'Expand all stacks' : 'Collapse all stacks'}
              >
                <i className={`fa-solid ${collapsedStacks.size > 0 ? 'fa-chevron-right' : 'fa-chevron-down'}`}></i>
              </button>
            )}
            <button
              className={`icon-btn ${showIgnored ? 'active' : ''}`}
              onClick={() => setShowIgnored(!showIgnored)}
              title="Show ignored containers"
            >
              <i className={`fa-solid fa-eye${showIgnored ? '' : '-slash'}`}></i>
            </button>
            <button
              className={`icon-btn ${showLocalImages ? 'active' : ''}`}
              onClick={() => setShowLocalImages(!showLocalImages)}
              title="Show local images"
            >
              {showLocalImages ? '◉' : '○'}
            </button>
            <button
              className={`icon-btn ${sort === 'stack' ? 'active' : ''}`}
              onClick={() => setSort(sort === 'stack' ? 'name' : 'stack')}
              title={sort === 'stack' ? 'Group by stack' : 'List view'}
            >
              {sort === 'stack' ? '▤' : '≡'}
            </button>
          </div>
        </div>
      </header>

      <main>
        {(() => {
          // Check if there are any containers after filtering
          const filteredContainerCount = result.containers.filter(filterContainer).length;

          if (filteredContainerCount === 0) {
            // Show different message based on why there are no results
            if (searchQuery) {
              return (
                <div className="empty-state">
                  <i className="fa-solid fa-magnifying-glass"></i>
                  <h2>No search results</h2>
                  <p>No containers match "{searchQuery}"</p>
                </div>
              );
            } else if (filter === 'updates') {
              return (
                <div className="empty-state">
                  <i className="fa-solid fa-circle-check"></i>
                  <h2>All containers are up to date</h2>
                  <p>There are no updates available at this time</p>
                </div>
              );
            } else {
              return (
                <div className="empty-state">
                  <i className="fa-solid fa-box-open"></i>
                  <h2>No containers found</h2>
                  <p>No containers match the current filter</p>
                </div>
              );
            }
          }

          return sort === 'stack' ? (
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
                      <button
                        className="stack-restart-btn"
                        onClick={(e) => handleStackRestart(stack.name, e)}
                        disabled={restartingStack === stack.name}
                        title={`Restart all containers in ${stack.name}`}
                      >
                        <i className={`fa-solid fa-rotate-right ${restartingStack === stack.name ? 'fa-spin' : ''}`}></i>
                      </button>
                    </h2>
                    {!collapsedStacks.has(stack.name) && (
                      <ul>
                        {filteredContainers.map((container) => (
                          <ContainerRow
                            key={container.container_name}
                            container={container}
                            selected={selectedContainers.has(container.container_name)}
                            onToggle={() => toggleContainer(container.container_name)}
                            onContainerClick={() => setExpandedContainer(container.container_name)}
                            allContainers={result.containers}
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
                          onContainerClick={() => setExpandedContainer(container.container_name)}
                          allContainers={result.containers}
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
                      onContainerClick={() => setExpandedContainer(container.container_name)}
                      allContainers={result.containers}
                    />
                  ))}
              </ul>
            </section>
          );
        })()}
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
                    {container.status === 'pending' && <i className="fa-regular fa-circle"></i>}
                    {container.status === 'in_progress' && <i className="fa-solid fa-spinner fa-spin"></i>}
                    {container.status === 'success' && <i className="fa-solid fa-check"></i>}
                    {container.status === 'failed' && <i className="fa-solid fa-xmark"></i>}
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
              <div className="log-entries" ref={logEntriesRef}>
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
                  <div className="completion-success"><i className="fa-solid fa-check"></i> All updates completed successfully!</div>
                ) : (
                  <div className="completion-error"><i className="fa-solid fa-xmark"></i> Updates completed with errors</div>
                )}
                <button className="close-btn" onClick={() => {
                  setUpdateProgress(null);
                  setUpdating(false);
                  backgroundRefresh();
                }}>
                  Close
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {expandedContainer && result && (
        <ContainerDetailModal
          container={result.containers.find(c => c.container_name === expandedContainer)!}
          onClose={() => setExpandedContainer(null)}
          onRefresh={backgroundRefresh}
          onUpdate={handleSingleUpdate}
        />
      )}

    </div>
  );
}

interface ContainerRowProps {
  container: ContainerInfo;
  selected: boolean;
  onToggle: () => void;
  onContainerClick: () => void;
  allContainers: ContainerInfo[];
}

function ContainerRow({ container, selected, onToggle, onContainerClick, allContainers }: ContainerRowProps) {
  const hasUpdate = isUpdatable(container.status);
  const isBlocked = container.status === 'UPDATE_AVAILABLE_BLOCKED';

  // Check restart dependencies
  const restartDependsOn = container.labels?.['docksmith.restart-depends-on'] || '';
  const restartDeps = restartDependsOn ? restartDependsOn.split(',').map(d => d.trim()) : [];

  // Find containers that depend on this one
  const dependents = allContainers.filter(c => {
    const deps = c.labels?.['docksmith.restart-depends-on'] || '';
    if (!deps) return false;
    const depList = deps.split(',').map(d => d.trim());
    return depList.includes(container.container_name);
  }).map(c => c.container_name);

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
        const pinnableVersion = container.recommended_tag || container.current_version || (container.using_latest_tag ? 'latest' : '(no tag)');
        return <span className="dot pinnable" title={`No version tag specified. Pin to: ${container.image}:${pinnableVersion}`}></span>;
      case 'LOCAL_IMAGE':
        return <span className="dot local" title="Local image"></span>;
      case 'COMPOSE_MISMATCH':
        return <span className="dot error" title="Container image doesn't match compose file"></span>;
      case 'IGNORED':
        return <span className="dot ignored" title="Ignored"></span>;
      default:
        return <span className="dot" title={container.status}></span>;
    }
  };

  const getChangeTypeBadge = () => {
    if (container.status !== 'UPDATE_AVAILABLE' && container.status !== 'UPDATE_AVAILABLE_BLOCKED') return null;

    switch (container.change_type) {
      case ChangeType.MajorChange:
        return <span className="change-badge major">MAJOR</span>;
      case ChangeType.MinorChange:
        return <span className="change-badge minor">MINOR</span>;
      case ChangeType.PatchChange:
        return <span className="change-badge patch">PATCH</span>;
      default:
        return null;
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
    // Handle case where we have latest_version but no current_version (e.g., :latest tag updates)
    if (hasUpdate && !container.current_version && container.latest_version) {
      const currentTag = container.current_tag || (container.using_latest_tag ? 'latest' : 'current');
      return `${currentTag} → ${container.latest_version}`;
    }
    if (container.status === 'LOCAL_IMAGE') {
      return 'Local image';
    }
    if (container.status === 'COMPOSE_MISMATCH') {
      return 'Mismatch detected';
    }
    if (container.status === 'IGNORED') {
      return 'Ignored';
    }
    return container.current_tag || container.current_version || '';
  };

  const handleRowClick = (e: React.MouseEvent) => {
    // Don't open detail modal if clicking checkbox
    const target = e.target as HTMLElement;
    if (target.tagName === 'INPUT' && (target as HTMLInputElement).type === 'checkbox') return;
    onContainerClick();
  };

  return (
    <li
      className={`${hasUpdate ? 'has-update' : ''} ${selected ? 'selected' : ''} ${isBlocked ? 'blocked' : ''} container-row-clickable`}
      onClick={handleRowClick}
    >
      {hasUpdate && (
        <input
          type="checkbox"
          checked={selected}
          onChange={onToggle}
        />
      )}
      <div className="container-info">
        <span className="name">{container.container_name}</span>
        <span className="version">{getVersion()} {getChangeTypeBadge()}</span>
      </div>
      {getStatusIndicator()}
      {container.pre_update_check_pass && <span className="check" title="Pre-update check passed"><i className="fa-solid fa-check"></i></span>}
      {container.pre_update_check_fail && (
        <span className="warn" title={container.pre_update_check_fail}><i className="fa-solid fa-triangle-exclamation"></i></span>
      )}
      {container.health_status === 'unhealthy' && (
        <span className="warn" title="Container is currently unhealthy"><i className="fa-solid fa-heart-crack"></i></span>
      )}
      {container.health_status === 'starting' && (
        <span className="info" title="Container health check is starting"><i className="fa-solid fa-heartbeat"></i></span>
      )}
      {restartDeps.length > 0 && (
        <span className="info restart-dep" title={`Restarts when ${restartDeps.join(', ')} restart${restartDeps.length > 1 ? '' : 's'}`}>
          <i className="fa-solid fa-link"></i> {restartDeps.length}
        </span>
      )}
      {dependents.length > 0 && (
        <span className="warn restart-dep-by" title={`${dependents.length} container${dependents.length > 1 ? 's' : ''} will restart: ${dependents.join(', ')}`}>
          <i className="fa-solid fa-link"></i> {dependents.length}
        </span>
      )}
    </li>
  );
}
