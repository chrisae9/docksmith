import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { checkContainers, getContainerStatus } from '../api/client';
import type { DiscoveryResult, ContainerInfo, Stack } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';
import { usePeriodicRefresh } from '../hooks/usePeriodicRefresh';
import { isMismatch, isActionable, isUpdatable } from '../utils/status';
import { useToast } from './Toast';
import { SearchBar, StackGroup } from './shared';
import { ContainerRow } from './Dashboard/ContainerRow';
import { SkeletonDashboard } from './Skeleton/Skeleton';
import {
  DashboardSettingsMenu,
  DEFAULT_DASHBOARD_SETTINGS,
  type DashboardSettings,
} from './Dashboard/DashboardSettings';

const STORAGE_KEY_DASHBOARD_SETTINGS = 'dashboard_settings';

interface DashboardProps {
  onNavigateToHistory?: () => void;
}

export function Dashboard({ onNavigateToHistory: _onNavigateToHistory }: DashboardProps) {
  const navigate = useNavigate();
  const toast = useToast();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const [collapsedStacks, setCollapsedStacks] = useState<Set<string>>(new Set());
  const [searchQuery, setSearchQuery] = useState('');
  const [showSettings, setShowSettings] = useState(false);
  const [settings, setSettings] = useState<DashboardSettings>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_DASHBOARD_SETTINGS);
    if (saved) {
      try {
        return { ...DEFAULT_DASHBOARD_SETTINGS, ...JSON.parse(saved) };
      } catch {
        return DEFAULT_DASHBOARD_SETTINGS;
      }
    }
    return DEFAULT_DASHBOARD_SETTINGS;
  });

  // Destructure settings for convenience
  const { filter, sort, showIgnored, showLocalImages, showMismatch } = settings;

  // Update settings and persist to localStorage
  const updateSettings = (updates: Partial<DashboardSettings>) => {
    setSettings(prev => {
      const newSettings = { ...prev, ...updates };
      localStorage.setItem(STORAGE_KEY_DASHBOARD_SETTINGS, JSON.stringify(newSettings));
      return newSettings;
    });
  };

  // Pull-to-refresh state
  const [pullDistance, setPullDistance] = useState(0);
  const [isPulling, setIsPulling] = useState(false);
  const pullStartY = useRef<number | null>(null);
  const mainRef = useRef<HTMLElement>(null);

  // Connect to SSE for real-time progress (always connected for check progress)
  const { checkProgress, containerUpdated, reconnecting, wasDisconnected, clearWasDisconnected } = useEventStream(true);

  // Fetch cached status (for initial load)
  // Does NOT show loading state to avoid screen flashing when switching tabs
  const fetchCachedStatus = async () => {
    try {
      const response = await getContainerStatus();
      if (response.success && response.data) {
        setResult(response.data);
        setError(null);
      } else {
        // Only set error if we don't have existing data
        if (!result) {
          setError(response.error || 'Failed to fetch data');
        }
      }
    } catch (err) {
      // Only set error if we don't have existing data
      if (!result) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      }
    }
    // If this is truly the first load (no data), end loading state
    if (!result) {
      setLoading(false);
    }
  };

  // Background refresh - triggers background check using cached registry data
  // Does NOT show loading state to avoid screen flashing
  const backgroundRefresh = async () => {
    try {
      // Trigger background check (uses cached registry data, respects CACHE_TTL)
      await fetch('/api/trigger-check', { method: 'POST' });
      await new Promise(resolve => setTimeout(resolve, 500));
      const response = await getContainerStatus();
      if (response.success && response.data) {
        setResult(response.data);
        // Clear any previous errors on successful refresh
        setError(null);
      }
      // Don't set error on background refresh failures to avoid disrupting UI
    } catch (err) {
      // Silently fail - background refresh shouldn't disrupt the UI
      console.error('Background refresh failed:', err);
    }
  };

  // Periodic background refresh (every 60 seconds)
  usePeriodicRefresh(
    backgroundRefresh,
    60000, // 60 seconds
    true, // enabled
    () => loading
  );

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

  const handleUpdate = () => {
    if (selectedContainers.size === 0 || !result) return;

    const containerNames = Array.from(selectedContainers);

    // Separate mismatch containers from regular updates
    const mismatchContainers: string[] = [];
    const containersToUpdate: Array<{ name: string; target_version: string; stack: string; change_type: number; old_resolved_version: string; new_resolved_version: string }> = [];

    containerNames.forEach(name => {
      const container = result.containers.find(c => c.container_name === name);
      if (!container) return;

      if (isMismatch(container.status)) {
        mismatchContainers.push(name);
      } else {
        containersToUpdate.push({
          name,
          target_version: container.recommended_tag || container.latest_version || '',
          stack: container.stack || '',
          change_type: container.change_type || 0,
          old_resolved_version: container.current_version || '',
          new_resolved_version: container.latest_resolved_version || container.latest_version || '',
        });
      }
    });

    // Clear selection and navigate to progress page
    setSelectedContainers(new Set());

    // Navigate with appropriate state based on what's selected
    if (mismatchContainers.length > 0 && containersToUpdate.length > 0) {
      // Mixed: both updates and mismatches
      navigate('/operation', {
        state: {
          mixed: {
            updates: containersToUpdate,
            mismatches: mismatchContainers,
          }
        }
      });
    } else if (mismatchContainers.length > 0) {
      // Only mismatches
      if (mismatchContainers.length === 1) {
        navigate('/operation', {
          state: { fixMismatch: { containerName: mismatchContainers[0] } }
        });
      } else {
        navigate('/operation', {
          state: { batchFixMismatch: { containerNames: mismatchContainers } }
        });
      }
    } else {
      // Only updates
      navigate('/operation', { state: { update: { containers: containersToUpdate } } });
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

  // Trigger background refresh on browser page refresh or focus
  useEffect(() => {
    // Trigger background refresh when component mounts (page reload)
    // Use a small delay to avoid race with initial fetchCachedStatus
    const mountTimer = setTimeout(() => {
      backgroundRefresh();
    }, 100);

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        // Page became visible (tab switched back or browser restored)
        backgroundRefresh();
      }
    };

    const handleFocus = () => {
      // Window gained focus (user clicked back into browser)
      backgroundRefresh();
    };

    // Listen for visibility changes (tab switching, minimize/restore)
    document.addEventListener('visibilitychange', handleVisibilityChange);

    // Listen for window focus (clicking back into the browser)
    window.addEventListener('focus', handleFocus);

    return () => {
      clearTimeout(mountTimer);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('focus', handleFocus);
    };
  }, []);

  // Auto-refresh when container.updated event is received
  useEffect(() => {
    if (!containerUpdated) return;

    // If this is from an update completion, trigger a background check first
    // to ensure we get fresh status showing the container is now up-to-date
    if (containerUpdated.status === 'updated') {
      backgroundRefresh();
    } else {
      // For background check completions, just fetch cached status
      fetchCachedStatus();
    }
  }, [containerUpdated]);

  // Auto-refresh when recovering from a disconnection (e.g., after traefik-ts restart)
  useEffect(() => {
    if (wasDisconnected) {
      toast.success('Connection restored - refreshing...');
      backgroundRefresh();
      clearWasDisconnected();
    }
  }, [wasDisconnected, clearWasDisconnected, toast]);

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
    const actionableContainers = result.containers
      .filter(c => isActionable(c))
      .filter(filterContainer)
      .map(c => c.container_name);
    setSelectedContainers(new Set(actionableContainers));
  };

  const deselectAll = () => {
    setSelectedContainers(new Set());
  };

  const toggleStackSelection = (containers: ContainerInfo[]) => {
    const actionableInStack = containers
      .filter(c => isActionable(c))
      .map(c => c.container_name);

    if (actionableInStack.length === 0) return;

    // Check if all actionable containers in this stack are already selected
    const allSelected = actionableInStack.every(name => selectedContainers.has(name));

    setSelectedContainers(prev => {
      const next = new Set(prev);
      if (allSelected) {
        // Deselect all in this stack
        actionableInStack.forEach(name => next.delete(name));
      } else {
        // Select all actionable in this stack
        actionableInStack.forEach(name => next.add(name));
      }
      return next;
    });
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
    // Hide local images unless explicitly showing them
    if (container.status === 'LOCAL_IMAGE' && !showLocalImages) {
      return false;
    }
    // Hide ignored containers unless explicitly showing them
    if (container.status === 'IGNORED' && !showIgnored) {
      return false;
    }
    switch (filter) {
      case 'updates':
        // Show updatable containers, and mismatch containers if showMismatch is enabled
        if (isUpdatable(container)) return true;
        if (showMismatch && isMismatch(container.status)) return true;
        return false;
      default:
        return true;
    }
  };

  if (loading) {
    return (
      <div className="dashboard">
        <header>
          <div className="header-top">
            <h1>Docksmith</h1>
          </div>
          <SearchBar
            value=""
            onChange={() => {}}
            placeholder="Search containers..."
            disabled
            className="search-bar-skeleton"
          />
          <div className="filter-toolbar">
            <div className="segmented-control">
              <button disabled>All</button>
              <button className="active" disabled>Updates</button>
            </div>
            <div className="toolbar-options">
              <button className="icon-btn" disabled><i className="fa-solid fa-chevron-down"></i></button>
              <button className="icon-btn" disabled><i className="fa-solid fa-eye-slash"></i></button>
              <button className="icon-btn" disabled>○</button>
              <button className="icon-btn active" disabled>▤</button>
            </div>
          </div>
        </header>
        <main>
          {checkProgress ? (
            <div className="check-progress-overlay">
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
            </div>
          ) : (
            <SkeletonDashboard />
          )}
        </main>
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

  // Pull-to-refresh handlers
  const handleTouchStart = (e: React.TouchEvent) => {
    // Only allow pull-to-refresh when scrolled to the top of the container
    const scrollTop = mainRef.current?.scrollTop ?? 0;
    if (scrollTop === 0 && !loading) {
      pullStartY.current = e.touches[0].clientY;
      setIsPulling(true);
    }
  };

  const handleTouchMove = (e: React.TouchEvent) => {
    if (!isPulling || pullStartY.current === null) return;

    const currentY = e.touches[0].clientY;
    const distance = currentY - pullStartY.current;

    // Only track downward pulls
    if (distance > 0) {
      setPullDistance(Math.min(distance, 150)); // Cap at 150px for two-level refresh
    }
  };

  const handleTouchEnd = () => {
    // Two-level threshold:
    // - 70-119px: Background refresh (blue)
    // - 120px+: Cache refresh (orange)
    if (pullDistance >= 120) {
      cacheRefresh();
    } else if (pullDistance >= 70) {
      backgroundRefresh();
    }

    // Reset pull state
    setIsPulling(false);
    setPullDistance(0);
    pullStartY.current = null;
  };

  return (
    <div
      className="dashboard"
      onTouchStart={handleTouchStart}
      onTouchMove={handleTouchMove}
      onTouchEnd={handleTouchEnd}
    >
      {/* Pull-to-refresh indicator */}
      {pullDistance > 0 && (() => {
        const isBackgroundLevel = pullDistance >= 70 && pullDistance < 120;
        const isCacheLevel = pullDistance >= 120;
        const backgroundColor = isCacheLevel ? '#ff8c42' : isBackgroundLevel ? '#4a9eff' : '#2c2c2c';
        const iconColor = isCacheLevel || isBackgroundLevel ? '#ffffff' : '#888888';
        const label = isCacheLevel ? 'Cache Refresh' : isBackgroundLevel ? 'Background Refresh' : 'Pull to refresh';

        return (
          <div
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              right: 0,
              height: `${pullDistance}px`,
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              background: backgroundColor,
              transition: isPulling ? 'background 0.1s ease-out' : 'height 0.2s ease-out, background 0.1s ease-out',
              zIndex: 1000,
            }}
          >
            <i
              className="fa-solid fa-rotate"
              style={{
                fontSize: '1.5rem',
                color: iconColor,
                transform: `rotate(${pullDistance * 3.6}deg)`,
                opacity: Math.min(pullDistance / 70, 1),
              }}
            ></i>
            {pullDistance >= 40 && (
              <div
                style={{
                  marginTop: '8px',
                  fontSize: '0.875rem',
                  fontWeight: '500',
                  color: iconColor,
                  opacity: Math.min(pullDistance / 70, 1),
                }}
              >
                {label}
              </div>
            )}
          </div>
        );
      })()}

      <header>
        <div className="header-top">
          <h1>Docksmith</h1>
          {reconnecting && (
            <div className="connection-status reconnecting" title="Attempting to reconnect...">
              <i className="fa-solid fa-wifi fa-fade"></i>
              <span>Reconnecting...</span>
            </div>
          )}
        </div>
        <SearchBar
          value={searchQuery}
          onChange={setSearchQuery}
          placeholder="Search containers..."
        />
        <div className="filter-toolbar">
          <div className="segmented-control">
            <button
              className={filter === 'all' ? 'active' : ''}
              onClick={() => updateSettings({ filter: 'all' })}
            >
              All
            </button>
            <button
              className={filter === 'updates' ? 'active' : ''}
              onClick={() => updateSettings({ filter: 'updates' })}
            >
              Updates
            </button>
          </div>
          <div className="toolbar-options">
            {result && result.containers.some(c => isActionable(c)) && (
              <button
                onClick={selectedContainers.size > 0 ? deselectAll : selectAll}
                className="icon-btn select-all-btn"
                title={selectedContainers.size > 0 ? 'Deselect all' : 'Select all'}
                aria-label={selectedContainers.size > 0 ? 'Deselect all' : 'Select all'}
              >
                <i className={`fa-solid ${selectedContainers.size > 0 ? 'fa-square-minus' : 'fa-square-check'}`} aria-hidden="true"></i>
              </button>
            )}
            {sort === 'stack' && (
              <button
                className="icon-btn"
                onClick={toggleAllStacks}
                title={collapsedStacks.size > 0 ? 'Expand all stacks' : 'Collapse all stacks'}
                aria-label={collapsedStacks.size > 0 ? 'Expand all stacks' : 'Collapse all stacks'}
              >
                <i className={`fa-solid ${collapsedStacks.size > 0 ? 'fa-chevron-right' : 'fa-chevron-down'}`} aria-hidden="true"></i>
              </button>
            )}
            <div className="settings-menu-wrapper">
              <button
                className={`icon-btn dashboard-settings-btn ${showSettings ? 'active' : ''}`}
                onClick={() => setShowSettings(!showSettings)}
                title="View Options"
                aria-label="View Options"
                aria-expanded={showSettings}
              >
                <i className="fa-solid fa-sliders" aria-hidden="true"></i>
              </button>
              {showSettings && (
                <DashboardSettingsMenu
                  settings={settings}
                  onSettingsChange={updateSettings}
                  onClose={() => setShowSettings(false)}
                />
              )}
            </div>
          </div>
        </div>
      </header>

      <main ref={mainRef} className="main-content">
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
            <div className="stack-list">
              {Object.values(result.stacks).map((stack: Stack) => {
                const filteredContainers = stack.containers.filter(filterContainer);
                if (filteredContainers.length === 0) return null;

                const actionableInStack = filteredContainers.filter(c => isActionable(c));
                const allStackSelected = actionableInStack.length > 0 &&
                  actionableInStack.every(c => selectedContainers.has(c.container_name));

                return (
                  <StackGroup
                    key={stack.name}
                    name={stack.name}
                    isCollapsed={collapsedStacks.has(stack.name)}
                    onToggle={() => toggleStack(stack.name)}
                    actions={actionableInStack.length > 0 && (
                      <button
                        className="stack-select-btn"
                        onClick={() => toggleStackSelection(filteredContainers)}
                        title={allStackSelected ? 'Deselect all in stack' : 'Select all in stack'}
                        aria-label={allStackSelected ? 'Deselect all in stack' : 'Select all in stack'}
                      >
                        <i className={`fa-solid ${allStackSelected ? 'fa-square-minus' : 'fa-square-check'}`} aria-hidden="true"></i>
                      </button>
                    )}
                  >
                    {filteredContainers.map((container) => (
                      <ContainerRow
                        key={container.container_name}
                        container={container}
                        selected={selectedContainers.has(container.container_name)}
                        onToggle={() => toggleContainer(container.container_name)}
                        onContainerClick={() => navigate(`/container/${container.container_name}`)}
                        allContainers={result.containers}
                      />
                    ))}
                  </StackGroup>
                );
              })}

              {(() => {
                const standaloneFiltered = result.standalone_containers.filter(filterContainer);
                if (standaloneFiltered.length === 0) return null;

                const actionableStandalone = standaloneFiltered.filter(c => isActionable(c));
                const allStandaloneSelected = actionableStandalone.length > 0 &&
                  actionableStandalone.every(c => selectedContainers.has(c.container_name));

                return (
                  <StackGroup
                    name="Standalone"
                    isCollapsed={collapsedStacks.has('__standalone__')}
                    onToggle={() => toggleStack('__standalone__')}
                    isStandalone
                    actions={actionableStandalone.length > 0 && (
                      <button
                        className="stack-select-btn"
                        onClick={() => toggleStackSelection(standaloneFiltered)}
                        title={allStandaloneSelected ? 'Deselect all standalone' : 'Select all standalone'}
                        aria-label={allStandaloneSelected ? 'Deselect all standalone' : 'Select all standalone'}
                      >
                        <i className={`fa-solid ${allStandaloneSelected ? 'fa-square-minus' : 'fa-square-check'}`} aria-hidden="true"></i>
                      </button>
                    )}
                  >
                    {standaloneFiltered.map((container) => (
                      <ContainerRow
                        key={container.container_name}
                        container={container}
                        selected={selectedContainers.has(container.container_name)}
                        onToggle={() => toggleContainer(container.container_name)}
                        onContainerClick={() => navigate(`/container/${container.container_name}`)}
                        allContainers={result.containers}
                      />
                    ))}
                  </StackGroup>
                );
              })()}
            </div>
          ) : (
            <div className="stack-group">
              <ul className="stack-container-list">
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
                      onContainerClick={() => navigate(`/container/${container.container_name}`)}
                      allContainers={result.containers}
                    />
                  ))}
              </ul>
            </div>
          );
        })()}
      </main>

      {selectedContainers.size > 0 && (() => {
        // Check if docksmith is in the selected containers
        const hasSelfUpdate = result?.containers.some(c =>
          selectedContainers.has(c.container_name) &&
          (c.container_name.toLowerCase().includes('docksmith') ||
           c.image.toLowerCase().includes('docksmith'))
        );

        return (
          <div className="selection-bar">
            {hasSelfUpdate && (
              <div className="self-update-warning">
                <i className="fa-solid fa-triangle-exclamation"></i>
                <span>Docksmith will restart to apply the update</span>
              </div>
            )}
            <div className="selection-actions">
              <span>{selectedContainers.size} selected</span>
              <div className="selection-buttons">
                <button
                  className="cancel-btn"
                  onClick={() => setSelectedContainers(new Set())}
                >
                  Cancel
                </button>
                <button
                  className="update-btn"
                  onClick={handleUpdate}
                >
                  Update
                </button>
              </div>
            </div>
          </div>
        );
      })()}

    </div>
  );
}

