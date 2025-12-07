import { useState, useEffect, useRef } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getContainerLabels, checkContainers, getContainerStatus } from '../api/client';
import type { ContainerInfo } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { getRegistryUrl } from '../utils/registry';
import './ContainerDetailPage.css';

// Fetch fresh container data with re-run precheck
async function fetchContainerWithCheck(containerName: string): Promise<ContainerInfo | null> {
  const response = await checkContainers();
  if (response.success && response.data) {
    const foundContainer = response.data.containers.find(
      (c) => c.container_name === containerName
    );
    return foundContainer || null;
  }
  return null;
}

export function ContainerDetailPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { containerName } = useParams<{ containerName: string }>();

  const [container, setContainer] = useState<ContainerInfo | undefined>(undefined);
  const [loadingContainer, setLoadingContainer] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<React.ReactNode | null>(null);
  const [selectedScript, setSelectedScript] = useState<string>('');
  const [ignoreFlag, setIgnoreFlag] = useState(false);
  const [allowLatestFlag, setAllowLatestFlag] = useState(false);
  const [versionPin, setVersionPin] = useState<'none' | 'patch' | 'minor' | 'major'>('none');
  const [restartAfter, setRestartAfter] = useState<string>('');
  const [tagRegex, setTagRegex] = useState<string>('');
  const [hasChanges, setHasChanges] = useState(false);
  const [dependentContainers, setDependentContainers] = useState<string[]>([]);
  const [refreshingPrecheck, setRefreshingPrecheck] = useState(false);

  // Track original values to detect changes
  const [originalScript, setOriginalScript] = useState<string>('');
  const [originalIgnore, setOriginalIgnore] = useState(false);
  const [originalAllowLatest, setOriginalAllowLatest] = useState(false);
  const [originalVersionPin, setOriginalVersionPin] = useState<'none' | 'patch' | 'minor' | 'major'>('none');
  const [originalRestartAfter, setOriginalRestartAfter] = useState<string>('');
  const [originalTagRegex, setOriginalTagRegex] = useState<string>('');

  // Fetch container data when component mounts or containerName changes
  useEffect(() => {
    if (!containerName) {
      navigate('/');
      return;
    }

    const fetchContainerData = async () => {
      try {
        setLoadingContainer(true);
        const [statusResponse, labelsResponse] = await Promise.all([
          getContainerStatus(),
          getContainerLabels(containerName),
        ]);

        if (statusResponse.success && statusResponse.data) {
          const foundContainer = statusResponse.data.containers.find(
            (c) => c.container_name === containerName
          );

          if (foundContainer && labelsResponse.success && labelsResponse.data) {
            setContainer({
              ...foundContainer,
              labels: labelsResponse.data.labels || {},
            });
          } else {
            navigate('/');
          }
        }
      } catch (err) {
        console.error('Failed to load container:', err);
        navigate('/');
      } finally {
        setLoadingContainer(false);
      }
    };

    fetchContainerData();
  }, [containerName, navigate]);

  // Track pending state from sub-pages (to avoid overwriting in fetchData)
  const pendingStateRef = useRef<{ selectedScript?: string; restartAfter?: string; tagRegex?: string }>({});

  // Handle location state updates (from sub-pages)
  useEffect(() => {
    const state = location.state as any;
    if (state && (state.selectedScript !== undefined || state.restartAfter !== undefined || state.tagRegex !== undefined)) {
      // Store pending values so fetchData won't overwrite them
      if ('selectedScript' in state) {
        pendingStateRef.current.selectedScript = state.selectedScript;
        setSelectedScript(state.selectedScript);
      }
      if ('restartAfter' in state) {
        pendingStateRef.current.restartAfter = state.restartAfter;
        setRestartAfter(state.restartAfter);
      }
      if ('tagRegex' in state) {
        pendingStateRef.current.tagRegex = state.tagRegex;
        setTagRegex(state.tagRegex);
      }
      // Clear the state after handling to prevent re-applying on back navigation
      navigate(location.pathname, { replace: true, state: {} });
    }
  }, [location.state, location.pathname, navigate]);

  useEffect(() => {
    if (!container) return;
    fetchData();
  }, [container]);

  useEffect(() => {
    // Check if any settings have changed
    const scriptChanged = selectedScript !== originalScript;
    const ignoreChanged = ignoreFlag !== originalIgnore;
    const allowLatestChanged = allowLatestFlag !== originalAllowLatest;
    const versionPinChanged = versionPin !== originalVersionPin;
    const restartAfterChanged = restartAfter !== originalRestartAfter;
    const tagRegexChanged = tagRegex !== originalTagRegex;
    setHasChanges(scriptChanged || ignoreChanged || allowLatestChanged || versionPinChanged || restartAfterChanged || tagRegexChanged);
  }, [selectedScript, ignoreFlag, allowLatestFlag, versionPin, restartAfter, tagRegex, originalScript, originalIgnore, originalAllowLatest, originalVersionPin, originalRestartAfter, originalTagRegex]);

  const fetchData = async () => {
    if (!container) return;

    setLoading(true);
    setError(null);
    try {
      const [labelsResponse, containersResponse] = await Promise.all([
        getContainerLabels(container.container_name),
        checkContainers(),
      ]);

      if (labelsResponse.success && labelsResponse.data) {
        const labels = labelsResponse.data.labels || {};

        const scriptPath = labels['docksmith.pre-update-check'] || '';
        const ignore = labels['docksmith.ignore'] === 'true';
        const allowLatest = labels['docksmith.allow-latest'] === 'true';
        const restartDeps = labels['docksmith.restart-after'] || '';
        const tagRegexValue = labels['docksmith.tag-regex'] || '';

        // Convert version pin labels to single value
        let pin: 'none' | 'patch' | 'minor' | 'major' = 'none';
        if (labels['docksmith.version-pin-major'] === 'true') {
          pin = 'major';
        } else if (labels['docksmith.version-pin-minor'] === 'true') {
          pin = 'minor';
        } else if (labels['docksmith.version-pin-patch'] === 'true') {
          pin = 'patch';
        }

        // Don't overwrite values that came from location.state (sub-page navigation)
        const hasPendingScript = pendingStateRef.current.selectedScript !== undefined;
        const hasPendingRestartAfter = pendingStateRef.current.restartAfter !== undefined;
        const hasPendingTagRegex = pendingStateRef.current.tagRegex !== undefined;

        if (!hasPendingScript) {
          setSelectedScript(scriptPath);
        }
        setIgnoreFlag(ignore);
        setAllowLatestFlag(allowLatest);
        setVersionPin(pin);
        if (!hasPendingRestartAfter) {
          setRestartAfter(restartDeps);
        }
        if (!hasPendingTagRegex) {
          setTagRegex(tagRegexValue);
        }

        // Always set original values from API (these are the "saved" values)
        setOriginalScript(scriptPath);
        setOriginalIgnore(ignore);
        setOriginalAllowLatest(allowLatest);
        setOriginalVersionPin(pin);
        setOriginalRestartAfter(restartDeps);
        setOriginalTagRegex(tagRegexValue);

        // Clear pending state after applying originals (so change detection works)
        pendingStateRef.current = {};
      }

      // Load all containers and find which ones depend on this container
      if (containersResponse.success && containersResponse.data) {
        const containers = containersResponse.data.containers || [];

        // Find containers that have this container in their restart-after label
        const dependentContainersData = containers.filter(c => {
          const deps = c.labels?.['docksmith.restart-after'] || '';
          if (!deps) return false;
          const depList = deps.split(',').map(d => d.trim());
          return depList.includes(container.container_name);
        });

        const dependents = dependentContainersData.map(c => c.container_name);
        setDependentContainers(dependents);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const getChangeSummary = () => {
    const changes: string[] = [];

    if (ignoreFlag !== originalIgnore) {
      changes.push(`${ignoreFlag ? 'Enable' : 'Disable'} ignore`);
    }
    if (allowLatestFlag !== originalAllowLatest) {
      changes.push(`${allowLatestFlag ? 'Allow' : 'Disallow'} :latest tag`);
    }
    if (versionPin !== originalVersionPin) {
      if (versionPin === 'none') {
        changes.push('Remove version pin');
      } else {
        changes.push(`Pin to ${versionPin} version`);
      }
    }
    if (selectedScript !== originalScript) {
      if (selectedScript && !originalScript) {
        changes.push('Add pre-update script');
      } else if (!selectedScript && originalScript) {
        changes.push('Remove pre-update script');
      } else {
        changes.push('Change pre-update script');
      }
    }
    if (restartAfter !== originalRestartAfter) {
      if (restartAfter && !originalRestartAfter) {
        changes.push('Add restart dependencies');
      } else if (!restartAfter && originalRestartAfter) {
        changes.push('Remove restart dependencies');
      } else {
        changes.push('Update restart dependencies');
      }
    }
    if (tagRegex !== originalTagRegex) {
      if (tagRegex && !originalTagRegex) {
        changes.push('Add tag filter');
      } else if (!tagRegex && originalTagRegex) {
        changes.push('Remove tag filter');
      } else {
        changes.push('Update tag filter');
      }
    }

    return changes;
  };

  const handleSave = (force = false) => {
    if (!container) {
      console.error('Container is undefined in handleSave');
      setError('Container not loaded');
      return;
    }

    // Build label changes
    const labelChanges: Record<string, boolean | string> = {};

    if (ignoreFlag !== originalIgnore) {
      labelChanges.ignore = ignoreFlag;
    }
    if (allowLatestFlag !== originalAllowLatest) {
      labelChanges.allow_latest = allowLatestFlag;
    }
    if (versionPin !== originalVersionPin) {
      // Clear all pin labels first, then set the new one
      labelChanges.version_pin_major = versionPin === 'major';
      labelChanges.version_pin_minor = versionPin === 'minor';
      labelChanges.version_pin_patch = versionPin === 'patch';
    }
    if (selectedScript !== originalScript) {
      labelChanges.script = selectedScript || '';
    }
    if (restartAfter !== originalRestartAfter) {
      labelChanges.restart_after = restartAfter || '';
    }
    if (tagRegex !== originalTagRegex) {
      labelChanges.tag_regex = tagRegex || '';
    }

    if (Object.keys(labelChanges).length === 0) {
      return;
    }

    // Navigate to operation progress page with save settings
    navigate('/operation', {
      state: {
        restart: {
          containerName: container.container_name,
          force,
          saveSettings: true,
          labelChanges,
        }
      }
    });
  };

  const handleReset = () => {
    setSelectedScript(originalScript);
    setIgnoreFlag(originalIgnore);
    setAllowLatestFlag(originalAllowLatest);
    setVersionPin(originalVersionPin);
    setRestartAfter(originalRestartAfter);
    setTagRegex(originalTagRegex);
    setError(null);
  };

  const handleRestart = (force = false) => {
    if (!container) return;

    // Navigate to operation progress page
    navigate('/operation', {
      state: {
        restart: {
          containerName: container.container_name,
          force,
        }
      }
    });
  };

  const handleRefreshPrecheck = async () => {
    if (!container || refreshingPrecheck) return;

    setRefreshingPrecheck(true);
    try {
      const updatedContainer = await fetchContainerWithCheck(container.container_name);
      if (updatedContainer) {
        // Update container with fresh precheck status
        const labelsResponse = await getContainerLabels(container.container_name);
        if (labelsResponse.success && labelsResponse.data) {
          setContainer({
            ...updatedContainer,
            labels: labelsResponse.data.labels || {},
          });
        } else {
          setContainer(updatedContainer);
        }
      }
    } catch (err) {
      console.error('Failed to refresh precheck:', err);
      setError('Failed to refresh precheck status');
    } finally {
      setRefreshingPrecheck(false);
    }
  };

  const getStatusBadge = () => {
    if (!container) return null;
    switch (container.status) {
      case 'UPDATE_AVAILABLE':
        return <span className="status-badge update">Update Available</span>;
      case 'UPDATE_AVAILABLE_BLOCKED':
        return <span className="status-badge blocked">Update Blocked</span>;
      case 'UP_TO_DATE':
        return <span className="status-badge current">Up to Date</span>;
      case 'UP_TO_DATE_PINNABLE':
        return <span className="status-badge pinnable">Pinnable</span>;
      case 'LOCAL_IMAGE':
        return <span className="status-badge local">Local Image</span>;
      case 'IGNORED':
        return <span className="status-badge ignored">Ignored</span>;
      case 'METADATA_UNAVAILABLE':
        return <span className="status-badge metadata">Metadata Unavailable</span>;
      default:
        return <span className="status-badge unknown">{container.status}</span>;
    }
  };

  const getChangeTypeBadge = () => {
    if (!container) return null;
    // Don't show badge for NoChange, Unknown, or if the name is "unknown"
    const changeTypeName = getChangeTypeName(container.change_type);
    if (
      container.change_type === ChangeType.NoChange ||
      container.change_type === ChangeType.UnknownChange ||
      changeTypeName === 'unknown' ||
      changeTypeName === 'rebuild'
    ) {
      return null;
    }
    return (
      <span className={`change-badge ${changeTypeName}`}>
        {changeTypeName}
      </span>
    );
  };

  // Helper to get button class based on change type (matches badge colors)
  const getUpdateButtonClass = (changeType: number): string => {
    switch (changeType) {
      case ChangeType.PatchChange:
        return 'patch'; // blue (accent) - matches patch badge
      case ChangeType.MinorChange:
        return 'minor'; // orange - matches minor badge
      case ChangeType.MajorChange:
        return 'major'; // red (danger) - matches major badge
      default:
        return 'accent';
    }
  };

  // Helper to get button label based on change type
  const getUpdateButtonLabel = (changeType: number): string => {
    switch (changeType) {
      case ChangeType.PatchChange:
        return 'Patch';
      case ChangeType.MinorChange:
        return 'Minor';
      case ChangeType.MajorChange:
        return 'Major';
      case ChangeType.Downgrade:
        return 'Downgrade';
      default:
        return 'Update';
    }
  };

  // Show loading state while fetching container
  if (loadingContainer) {
    return (
      <div className="page container-detail-page">
        <header className="page-header">
          <button className="back-button" onClick={() => navigate('/')} aria-label="Go back to dashboard">
            ← Back
          </button>
          <h1>Loading...</h1>
          <div className="header-spacer"></div>
        </header>
      </div>
    );
  }

  // Container not found or error
  if (!container) {
    return null;
  }

  return (
    <div className="page container-detail-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate('/')} aria-label="Go back to dashboard">
          ← Back
        </button>
        <h1>{container.container_name}</h1>
        <div className="header-spacer"></div>
      </header>

      <main className="page-content">

        {error && (
          <div className="error-banner">
            <div className="error-content">
              <i className="fa-solid fa-triangle-exclamation"></i>
              <div className="error-text">
                <strong>Error</strong>
                <p>{error}</p>
              </div>
            </div>
            <button className="error-dismiss" onClick={() => setError(null)} aria-label="Dismiss error">×</button>
          </div>
        )}

        {/* Hero Status Card */}
        <div className="hero-status-card">
          <div className="hero-status-icon">
            {container.status === 'UPDATE_AVAILABLE' && <i className="fa-solid fa-arrow-up-circle"></i>}
            {container.status === 'UPDATE_AVAILABLE_BLOCKED' && <i className="fa-solid fa-ban"></i>}
            {container.status === 'UP_TO_DATE' && <i className="fa-solid fa-circle-check"></i>}
            {container.status === 'UP_TO_DATE_PINNABLE' && <i className="fa-solid fa-thumbtack"></i>}
            {container.status === 'LOCAL_IMAGE' && <i className="fa-solid fa-hard-drive"></i>}
            {container.status === 'IGNORED' && <i className="fa-solid fa-eye-slash"></i>}
            {container.status === 'METADATA_UNAVAILABLE' && <i className="fa-solid fa-circle-question"></i>}
          </div>
          <div className="hero-status-content">
            <div className="hero-status-badges">
              {getStatusBadge()}
              {getChangeTypeBadge()}
            </div>
            {(container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED') &&
             container.current_version && container.latest_version && (
              <div className="hero-version-info">
                <span className="version-current">{container.current_version}</span>
                <i className="fa-solid fa-arrow-right"></i>
                <span className="version-latest">{container.latest_version}</span>
              </div>
            )}
            {container.status === 'UP_TO_DATE' && container.current_version && (
              <div className="hero-version-info">
                <span className="version-current">{container.current_version}</span>
              </div>
            )}
            {container.status === 'METADATA_UNAVAILABLE' && (
              <div className="hero-version-info metadata-unavailable-info">
                {container.current_tag && (
                  <span className="version-current">Tag: {container.current_tag}</span>
                )}
                {container.error && (
                  <span className="metadata-error">{container.error}</span>
                )}
                {!container.error && !container.current_tag && (
                  <span className="metadata-error">Unable to fetch version information from registry</span>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Labels Out of Sync Warning */}
        {container.labels_out_of_sync && (
          <div className="labels-sync-warning">
            <div className="sync-warning-content">
              <i className="fa-solid fa-rotate"></i>
              <div className="sync-warning-text">
                <strong>Labels Out of Sync</strong>
                <p>Compose file labels differ from running container. Restart to apply changes.</p>
              </div>
            </div>
            <button
              className="button button-small button-warning"
              onClick={() => handleRestart(false)}
            >
              <i className="fa-solid fa-rotate-right"></i>
              Sync
            </button>
          </div>
        )}

        {/* Unsaved Changes Warning */}
        {hasChanges && (
          <div className="changes-warning">
            <div className="changes-warning-content">
              <i className="fa-solid fa-exclamation-triangle"></i>
              <div className="changes-warning-text">
                <strong>Container will be restarted</strong>
                <p>Changes: {getChangeSummary().join(', ')}</p>
              </div>
            </div>
          </div>
        )}

          {/* Normal content */}
          <>
              {/* Container Settings */}
              <div className="detail-section settings-section">
            <h3 className="section-title"><i className="fa-solid fa-sliders"></i>Settings</h3>

            {loading ? (
              <div className="loading-inline">
                <div className="spinner-small"></div>
                <span>Loading settings...</span>
              </div>
            ) : (
              <>
                <div className="settings-grid">
                  {/* Ignore Flag */}
                  <label className="checkbox-row">
                    <span className="row-label">Ignore Container</span>
                    <input
                      type="checkbox"
                      checked={ignoreFlag}
                      onChange={(e) => setIgnoreFlag(e.target.checked)}
                    />
                  </label>

                  {/* Allow Latest Flag */}
                  <label className="checkbox-row">
                    <span className="row-label">Allow :latest Tag</span>
                    <input
                      type="checkbox"
                      checked={allowLatestFlag}
                      onChange={(e) => setAllowLatestFlag(e.target.checked)}
                    />
                  </label>

                  {/* Version Pin - Segmented Control */}
                  <div className="setting-item segmented-row">
                    <span className="nav-title">Pin to Version</span>
                    <div className="segmented-control">
                      {(['none', 'patch', 'minor', 'major'] as const).map((option) => (
                        <button
                          key={option}
                          className={`segment ${versionPin === option ? 'active' : ''}`}
                          onClick={() => setVersionPin(option)}
                        >
                          {option === 'none' ? 'None' : option.charAt(0).toUpperCase() + option.slice(1)}
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Tag Filter - Navigate to regex tester */}
                  <div
                    className="setting-item nav-row"
                    onClick={() => navigate(`/container/${container.container_name}/tag-filter`, { state: {
                      currentTagRegex: tagRegex,
                      pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                      pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                    } })}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${container.container_name}/tag-filter`, { state: {
                      currentTagRegex: tagRegex,
                      pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                      pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                    } })}
                  >
                    <span className="nav-title">Tag Filter</span>
                    <span className="nav-value">
                      {tagRegex || 'None'}
                      <span className="nav-arrow">›</span>
                    </span>
                  </div>

                  {/* Pre-Update Check */}
                  <div className="setting-item precheck-row">
                    <div
                      className="precheck-nav-area"
                      onClick={() => navigate(`/container/${container.container_name}/script-selection`, { state: {
                        currentScript: selectedScript,
                        pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                        pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                      } })}
                      role="button"
                      tabIndex={0}
                      onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${container.container_name}/script-selection`, { state: {
                        currentScript: selectedScript,
                        pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                        pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                      } })}
                    >
                      <span className="nav-title">
                        Pre-Update Script
                        {/* Show status indicator if a check has run (pass or fail results exist) */}
                        {(container.pre_update_check_pass || container.pre_update_check_fail) && (
                          <span className={`precheck-status ${container.pre_update_check_pass ? 'pass' : 'fail'}`} title={container.pre_update_check_pass ? 'Check passed' : (container.pre_update_check_fail || 'Check failed')}>
                            {container.pre_update_check_pass ? (
                              <i className="fa-solid fa-circle-check"></i>
                            ) : (
                              <i className="fa-solid fa-circle-xmark"></i>
                            )}
                          </span>
                        )}
                      </span>
                      <span className="nav-value">
                        {selectedScript ? selectedScript.split('/').pop() : 'None'}
                        <span className="nav-arrow">›</span>
                      </span>
                    </div>
                    {/* Refresh button - show if a script is configured (from label or selectedScript) */}
                    {(selectedScript || container.pre_update_check) && (
                      <button
                        className="precheck-refresh-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleRefreshPrecheck();
                        }}
                        disabled={refreshingPrecheck}
                        title="Re-run pre-update check"
                        aria-label="Refresh pre-update check"
                      >
                        <i className={`fa-solid fa-rotate-right ${refreshingPrecheck ? 'spinning' : ''}`}></i>
                      </button>
                    )}
                  </div>

                  {/* Restart Dependencies */}
                  <div
                    className="setting-item nav-row"
                    onClick={() => navigate(`/container/${container.container_name}/restart-dependencies`, { state: {
                      currentDependencies: restartAfter,
                      pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                      pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                    } })}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${container.container_name}/restart-dependencies`, { state: {
                      currentDependencies: restartAfter,
                      pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                      pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                    } })}
                  >
                    <span className="nav-title">Restart Dependencies</span>
                    <span className="nav-value">
                      {restartAfter ? `${restartAfter.split(',').length}` : 'None'}
                      <span className="nav-arrow">›</span>
                    </span>
                  </div>

                  {/* Docker Compose Dependencies (read-only) */}
                  {container.dependencies && container.dependencies.length > 0 && (
                    <div className="setting-item compose-deps">
                      <span className="nav-title">Compose Dependencies</span>
                      <span className="nav-value">
                        {container.dependencies.join(', ')}
                      </span>
                    </div>
                  )}
                </div>

                {/* Compact warning for dependents */}
                {dependentContainers.length > 0 && (
                  <div className="dependents-note">
                    <i className="fa-solid fa-exclamation-triangle"></i>
                    <span>Restart affects: {dependentContainers.join(', ')}</span>
                  </div>
                )}
              </>
            )}
          </div>

          {/* Image Information */}
          <div className="detail-section">
            <h3 className="section-title"><i className="fa-solid fa-cube"></i>Image Details</h3>
            <div className="detail-grid">
              <div className="detail-item">
                <span className="detail-label">Repository</span>
                <span className="detail-value mono">
                  {getRegistryUrl(container.image) ? (
                    <a
                      href={getRegistryUrl(container.image)!}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="image-link"
                    >
                      {container.image}
                      <i className="fa-solid fa-external-link-alt" style={{ marginLeft: '6px', fontSize: '12px', opacity: 0.7 }}></i>
                    </a>
                  ) : (
                    container.image
                  )}
                </span>
              </div>
              {container.current_tag && (
                <div className="detail-item">
                  <span className="detail-label">Current Tag</span>
                  <span className="detail-value mono">{container.current_tag}</span>
                </div>
              )}
              {container.current_version && (
                <div className="detail-item">
                  <span className="detail-label">Current Version</span>
                  <span className="detail-value mono">{container.current_version}</span>
                </div>
              )}
              {container.latest_version && (
                <div className="detail-item">
                  <span className="detail-label">Latest Version</span>
                  <span className="detail-value mono">{container.latest_version}</span>
                </div>
              )}
              {container.recommended_tag && (
                <div className="detail-item">
                  <span className="detail-label">Recommended Tag</span>
                  <span className="detail-value mono">{container.recommended_tag}</span>
                </div>
              )}
            </div>
          </div>

          {/* Stack & Service */}
          {(container.stack || container.service) && (
            <div className="detail-section">
              <h3 className="section-title"><i className="fa-solid fa-layer-group"></i>Stack</h3>
              <div className="detail-grid">
                {container.stack && (
                  <div className="detail-item">
                    <span className="detail-label">Stack</span>
                    <span className="detail-value">{container.stack}</span>
                  </div>
                )}
                {container.service && (
                  <div className="detail-item">
                    <span className="detail-label">Service</span>
                    <span className="detail-value">{container.service}</span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* All Labels - Always show */}
          <div className="detail-section labels-section">
            <h3 className="section-title"><i className="fa-solid fa-tags"></i>Container Labels</h3>
            <div className="labels-list">
              {container.labels && Object.keys(container.labels).length > 0 ? (
                Object.entries(container.labels)
                  .sort(([a], [b]) => {
                    // Sort docksmith labels first
                    const aIsDocksmith = a.startsWith('docksmith.');
                    const bIsDocksmith = b.startsWith('docksmith.');
                    if (aIsDocksmith && !bIsDocksmith) return -1;
                    if (!aIsDocksmith && bIsDocksmith) return 1;
                    return a.localeCompare(b);
                  })
                  .map(([key, value]) => {
                    const isDocksmith = key.startsWith('docksmith.');
                    const getLabelIcon = (labelKey: string) => {
                      switch (labelKey) {
                        case 'docksmith.ignore':
                          return <i className="fa-solid fa-eye-slash label-icon-inline"></i>;
                        case 'docksmith.allow-latest':
                          return <i className="fa-solid fa-tag label-icon-inline"></i>;
                        case 'docksmith.version-pin-major':
                        case 'docksmith.version-pin-minor':
                        case 'docksmith.version-pin-patch':
                          return <i className="fa-solid fa-thumbtack label-icon-inline"></i>;
                        case 'docksmith.tag-regex':
                          return <i className="fa-solid fa-filter label-icon-inline"></i>;
                        case 'docksmith.pre-update-check':
                          return <i className="fa-solid fa-terminal label-icon-inline"></i>;
                        case 'docksmith.restart-after':
                          return <i className="fa-solid fa-link label-icon-inline"></i>;
                        default:
                          if (labelKey.startsWith('docksmith.')) {
                            return <i className="fa-solid fa-gear label-icon-inline"></i>;
                          }
                          return null;
                      }
                    };
                    return (
                      <div key={key} className={`label-item ${isDocksmith ? 'docksmith-label' : ''}`}>
                        <span className="label-key">
                          {getLabelIcon(key)}
                          {key}
                        </span>
                        <span className="label-value">{value || '(empty)'}</span>
                      </div>
                    );
                  })
              ) : (
                <div className="empty-labels">No labels defined</div>
              )}
            </div>
          </div>
            </>
      </main>

      {/* Page Footer */}
      <footer className="page-footer">
        {/* Cancel button when there are changes */}
        {hasChanges && (
          <button
            className="button button-secondary"
            onClick={handleReset}
          >
            <i className="fa-solid fa-undo"></i>
            <span>Cancel</span>
          </button>
        )}

        {/* Restart/Save button - always show */}
        {hasChanges ? (
          // Save & Restart when there are label changes
          <button
            className="button button-primary"
            onClick={() => handleSave(false)}
          >
            <i className="fa-solid fa-save"></i>
            <span>Save & Restart</span>
          </button>
        ) : (
          // Plain Restart when no changes
          <button
            className="button button-secondary"
            onClick={() => handleRestart(false)}
          >
            <i className="fa-solid fa-rotate-right"></i>
            <span>Restart</span>
          </button>
        )}

        {/* Force Save & Restart when changes + blocked update */}
        {hasChanges && container.status === 'UPDATE_AVAILABLE_BLOCKED' && (
          <button
            className="button button-warning"
            onClick={() => handleSave(true)}
            title="Force restart despite pre-update check failure"
          >
            <i className="fa-solid fa-triangle-exclamation"></i>
            <span>Force</span>
          </button>
        )}

        {/* Update button when update available (regardless of type) */}
        {(container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED' || container.status === 'UP_TO_DATE_PINNABLE') && !hasChanges && (
          <>
            {/* Normal Update button */}
            {container.status === 'UPDATE_AVAILABLE' && (
              <button
                className={`button button-${getUpdateButtonClass(container.change_type)}`}
                onClick={() => navigate('/operation', {
                  state: {
                    update: {
                      containers: [{
                        name: container.container_name,
                        target_version: container.latest_version || container.recommended_tag || '',
                        stack: container.stack || ''
                      }]
                    }
                  }
                })}
              >
                <i className="fa-solid fa-arrow-up"></i>
                <span>{getUpdateButtonLabel(container.change_type)}</span>
              </button>
            )}

            {/* Force Update button when blocked - uses same color as change type */}
            {container.status === 'UPDATE_AVAILABLE_BLOCKED' && (
              <button
                className={`button button-${getUpdateButtonClass(container.change_type)}`}
                onClick={() => navigate('/operation', {
                  state: {
                    update: {
                      containers: [{
                        name: container.container_name,
                        target_version: container.latest_version || '',
                        stack: container.stack || '',
                        force: true
                      }]
                    }
                  }
                })}
                title={`Force update despite: ${container.pre_update_check_fail || 'pre-update check failure'}`}
              >
                <i className="fa-solid fa-triangle-exclamation"></i>
                <span>Force {getUpdateButtonLabel(container.change_type)}</span>
              </button>
            )}

            {/* Pin button for pinnable containers */}
            {container.status === 'UP_TO_DATE_PINNABLE' && (
              <button
                className="button button-pin"
                onClick={() => navigate('/operation', {
                  state: {
                    update: {
                      containers: [{
                        name: container.container_name,
                        target_version: container.recommended_tag || '',
                        stack: container.stack || ''
                      }]
                    }
                  }
                })}
              >
                <i className="fa-solid fa-thumbtack"></i>
                <span>Pin</span>
              </button>
            )}
          </>
        )}
      </footer>

    </div>
  );
}
