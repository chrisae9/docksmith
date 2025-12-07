import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { checkContainers, getContainerStatus, restartStack } from '../api/client';
import type { DiscoveryResult, ContainerInfo, Stack } from '../types/api';
import { ChangeType } from '../types/api';
import { useEventStream } from '../hooks/useEventStream';
import { usePeriodicRefresh } from '../hooks/usePeriodicRefresh';
import { isUpdatable } from '../utils/status';
import { STORAGE_KEY_FILTER, STORAGE_KEY_INITIAL_SWITCH } from '../utils/constants';
import { useToast } from './Toast';
import { SkeletonDashboard } from './Skeleton';

type FilterType = 'all' | 'updates' | 'local';
type SortType = 'stack' | 'name' | 'status';

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
  const [restartingStack, setRestartingStack] = useState<string | null>(null);

  // Pull-to-refresh state
  const [pullDistance, setPullDistance] = useState(0);
  const [isPulling, setIsPulling] = useState(false);
  const pullStartY = useRef<number | null>(null);
  const mainRef = useRef<HTMLElement>(null);

  // Connect to SSE for real-time progress (always connected for check progress)
  const { checkProgress, containerUpdated } = useEventStream(true);

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

    // Build container info with stack grouping
    const containersToUpdate = containerNames.map(name => {
      const container = result.containers.find(c => c.container_name === name);
      return {
        name,
        target_version: container?.latest_version || '',
        stack: container?.stack || '',
      };
    });

    // Clear selection and navigate to progress page
    setSelectedContainers(new Set());
    navigate('/operation', { state: { update: { containers: containersToUpdate } } });
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
          toast.success(`Stack "${stackName}" restarted successfully`);
        }, 1000);
      } else {
        const errorMsg = response.error || 'Failed to restart stack';
        setRestartingStack(null);
        toast.error(errorMsg);
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to restart stack';
      setRestartingStack(null);
      toast.error(errorMsg);
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
      <div className="dashboard">
        <header>
          <div className="header-top">
            <h1>Docksmith</h1>
          </div>
          <div className="search-bar search-bar-skeleton">
            <i className="fa-solid fa-search"></i>
            <input
              type="text"
              placeholder="Search containers..."
              disabled
              className="search-input"
            />
          </div>
          <div className="filter-toolbar">
            <div className="segmented-control">
              <button className="active" disabled>All</button>
              <button disabled>Updates</button>
            </div>
            <div className="toolbar-options">
              <button className="icon-btn" disabled><i className="fa-solid fa-layer-group"></i></button>
              <button className="icon-btn" disabled><i className="fa-solid fa-font"></i></button>
              <button className="icon-btn" disabled>○</button>
              <button className="icon-btn" disabled>▤</button>
            </div>
          </div>
        </header>
        <main className="main-loading">
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
          <i className="fa-solid fa-search" aria-hidden="true"></i>
          <input
            type="text"
            placeholder="Search containers..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="search-input"
            aria-label="Search containers"
          />
          {searchQuery && (
            <button className="clear-search" onClick={() => setSearchQuery('')} aria-label="Clear search">
              <i className="fa-solid fa-times" aria-hidden="true"></i>
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
                aria-label={collapsedStacks.size > 0 ? 'Expand all stacks' : 'Collapse all stacks'}
              >
                <i className={`fa-solid ${collapsedStacks.size > 0 ? 'fa-chevron-right' : 'fa-chevron-down'}`} aria-hidden="true"></i>
              </button>
            )}
            <button
              className={`icon-btn ${showIgnored ? 'active' : ''}`}
              onClick={() => setShowIgnored(!showIgnored)}
              title="Show ignored containers"
              aria-label={showIgnored ? 'Hide ignored containers' : 'Show ignored containers'}
              aria-pressed={showIgnored}
            >
              <i className={`fa-solid fa-eye${showIgnored ? '' : '-slash'}`} aria-hidden="true"></i>
            </button>
            <button
              className={`icon-btn ${showLocalImages ? 'active' : ''}`}
              onClick={() => setShowLocalImages(!showLocalImages)}
              title="Show local images"
              aria-label={showLocalImages ? 'Hide local images' : 'Show local images'}
              aria-pressed={showLocalImages}
            >
              {showLocalImages ? '◉' : '○'}
            </button>
            <button
              className={`icon-btn ${sort === 'stack' ? 'active' : ''}`}
              onClick={() => setSort(sort === 'stack' ? 'name' : 'stack')}
              title={sort === 'stack' ? 'Group by stack' : 'List view'}
              aria-label={sort === 'stack' ? 'Switch to list view' : 'Group by stack'}
            >
              {sort === 'stack' ? '▤' : '≡'}
            </button>
          </div>
        </div>
      </header>

      <main ref={mainRef}>
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
                    <h2
                      onClick={() => toggleStack(stack.name)}
                      role="button"
                      tabIndex={0}
                      aria-expanded={!collapsedStacks.has(stack.name)}
                      onKeyDown={(e) => e.key === 'Enter' && toggleStack(stack.name)}
                    >
                      <span className="toggle" aria-hidden="true">{collapsedStacks.has(stack.name) ? '▸' : '▾'}</span>
                      {stack.name}
                      {stack.has_updates && <span className="badge-dot" aria-label="Has updates"></span>}
                      <button
                        className="stack-restart-btn"
                        onClick={(e) => handleStackRestart(stack.name, e)}
                        disabled={restartingStack === stack.name}
                        title={`Restart all containers in ${stack.name}`}
                        aria-label={`Restart all containers in ${stack.name}`}
                      >
                        <i className={`fa-solid fa-rotate-right ${restartingStack === stack.name ? 'fa-spin' : ''}`} aria-hidden="true"></i>
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
                            onContainerClick={() => navigate(`/container/${container.container_name}`)}
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
                  <h2
                    onClick={() => toggleStack('__standalone__')}
                    role="button"
                    tabIndex={0}
                    aria-expanded={!collapsedStacks.has('__standalone__')}
                    onKeyDown={(e) => e.key === 'Enter' && toggleStack('__standalone__')}
                  >
                    <span className="toggle" aria-hidden="true">{collapsedStacks.has('__standalone__') ? '▸' : '▾'}</span>
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
                          onContainerClick={() => navigate(`/container/${container.container_name}`)}
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
                      onContainerClick={() => navigate(`/container/${container.container_name}`)}
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
          >
            Update
          </button>
        </div>
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
  const restartAfter = container.labels?.['docksmith.restart-after'] || '';
  const restartDeps = restartAfter ? restartAfter.split(',').map(d => d.trim()) : [];

  // Find containers that depend on this one
  const dependents = allContainers.filter(c => {
    const deps = c.labels?.['docksmith.restart-after'] || '';
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
    // Show PIN badge for pinnable containers (migrating from :latest to semver)
    if (container.status === 'UP_TO_DATE_PINNABLE') {
      return <span className="change-badge pin">PIN</span>;
    }

    if (container.status !== 'UPDATE_AVAILABLE' && container.status !== 'UPDATE_AVAILABLE_BLOCKED') return null;

    switch (container.change_type) {
      case ChangeType.MajorChange:
        return <span className="change-badge major">MAJOR</span>;
      case ChangeType.MinorChange:
        return <span className="change-badge minor">MINOR</span>;
      case ChangeType.PatchChange:
        return <span className="change-badge patch">PATCH</span>;
      case ChangeType.UnknownChange:
        // For :latest tag updates or when version parsing fails
        return <span className="change-badge rebuild">REBUILD</span>;
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

  // Get docksmith label settings
  const hasTagRegex = !!container.labels?.['docksmith.tag-regex'];
  const hasPreUpdateScript = !!container.labels?.['docksmith.pre-update-check'];
  const allowsLatest = container.labels?.['docksmith.allow-latest'] === 'true';
  const versionPinMajor = container.labels?.['docksmith.version-pin-major'] === 'true';
  const versionPinMinor = container.labels?.['docksmith.version-pin-minor'] === 'true';
  const versionPinPatch = container.labels?.['docksmith.version-pin-patch'] === 'true';
  const versionPin = versionPinMajor ? 'major' : versionPinMinor ? 'minor' : versionPinPatch ? 'patch' : null;

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
          aria-label={`Select ${container.container_name} for update`}
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
      {/* Docksmith label indicators */}
      {versionPin && (
        <span className={`label-icon pin-${versionPin}`} title={`Version pinned to ${versionPin}`}>
          <i className="fa-solid fa-thumbtack"></i>
        </span>
      )}
      {hasTagRegex && (
        <span className="label-icon regex" title="Tag regex filter applied">
          <i className="fa-solid fa-filter"></i>
        </span>
      )}
      {hasPreUpdateScript && !container.pre_update_check_pass && !container.pre_update_check_fail && (
        <span className="label-icon script" title="Pre-update script configured">
          <i className="fa-solid fa-terminal"></i>
        </span>
      )}
      {allowsLatest && (
        <span className="label-icon latest" title="Allows :latest tag">
          <i className="fa-solid fa-tag"></i>
        </span>
      )}
    </li>
  );
}
