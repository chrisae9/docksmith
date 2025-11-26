import { useState, useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getContainerLabels, checkContainers, getContainerStatus } from '../api/client';
import type { ContainerInfo } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { getRegistryUrl } from '../utils/registry';
import './ContainerDetailPage.css';

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
  const [versionPinMajor, setVersionPinMajor] = useState(false);
  const [versionPinMinor, setVersionPinMinor] = useState(false);
  const [restartDependsOn, setRestartDependsOn] = useState<string>('');
  const [hasChanges, setHasChanges] = useState(false);
  const [dependentContainers, setDependentContainers] = useState<string[]>([]);

  // Track original values to detect changes
  const [originalScript, setOriginalScript] = useState<string>('');
  const [originalIgnore, setOriginalIgnore] = useState(false);
  const [originalAllowLatest, setOriginalAllowLatest] = useState(false);
  const [originalVersionPinMajor, setOriginalVersionPinMajor] = useState(false);
  const [originalVersionPinMinor, setOriginalVersionPinMinor] = useState(false);
  const [originalRestartDependsOn, setOriginalRestartDependsOn] = useState<string>('');

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

  // Handle location state updates (from sub-pages)
  useEffect(() => {
    const state = location.state as any;
    if (state) {
      if ('selectedScript' in state) {
        setSelectedScript(state.selectedScript);
      }
      if ('restartDependsOn' in state) {
        setRestartDependsOn(state.restartDependsOn);
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
    const versionPinMajorChanged = versionPinMajor !== originalVersionPinMajor;
    const versionPinMinorChanged = versionPinMinor !== originalVersionPinMinor;
    const restartDependsOnChanged = restartDependsOn !== originalRestartDependsOn;
    setHasChanges(scriptChanged || ignoreChanged || allowLatestChanged || versionPinMajorChanged || versionPinMinorChanged || restartDependsOnChanged);
  }, [selectedScript, ignoreFlag, allowLatestFlag, versionPinMajor, versionPinMinor, restartDependsOn, originalScript, originalIgnore, originalAllowLatest, originalVersionPinMajor, originalVersionPinMinor, originalRestartDependsOn]);

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
        const pinMajor = labels['docksmith.version-pin-major'] === 'true';
        const pinMinor = labels['docksmith.version-pin-minor'] === 'true';
        const restartDeps = labels['docksmith.restart-depends-on'] || '';

        setSelectedScript(scriptPath);
        setIgnoreFlag(ignore);
        setAllowLatestFlag(allowLatest);
        setVersionPinMajor(pinMajor);
        setVersionPinMinor(pinMinor);
        setRestartDependsOn(restartDeps);

        setOriginalScript(scriptPath);
        setOriginalIgnore(ignore);
        setOriginalAllowLatest(allowLatest);
        setOriginalVersionPinMajor(pinMajor);
        setOriginalVersionPinMinor(pinMinor);
        setOriginalRestartDependsOn(restartDeps);
      }

      // Load all containers and find which ones depend on this container
      if (containersResponse.success && containersResponse.data) {
        const containers = containersResponse.data.containers || [];

        // Find containers that have this container in their restart-depends-on label
        const dependentContainersData = containers.filter(c => {
          const deps = c.labels?.['docksmith.restart-depends-on'] || '';
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
    if (versionPinMajor !== originalVersionPinMajor) {
      changes.push(`${versionPinMajor ? 'Pin' : 'Unpin'} to major version`);
    }
    if (versionPinMinor !== originalVersionPinMinor) {
      changes.push(`${versionPinMinor ? 'Pin' : 'Unpin'} to minor version`);
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
    if (restartDependsOn !== originalRestartDependsOn) {
      if (restartDependsOn && !originalRestartDependsOn) {
        changes.push('Add restart dependencies');
      } else if (!restartDependsOn && originalRestartDependsOn) {
        changes.push('Remove restart dependencies');
      } else {
        changes.push('Update restart dependencies');
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
    if (versionPinMajor !== originalVersionPinMajor) {
      labelChanges.version_pin_major = versionPinMajor;
    }
    if (versionPinMinor !== originalVersionPinMinor) {
      labelChanges.version_pin_minor = versionPinMinor;
    }
    if (selectedScript !== originalScript) {
      labelChanges.script = selectedScript || '';
    }
    if (restartDependsOn !== originalRestartDependsOn) {
      labelChanges.restart_depends_on = restartDependsOn || '';
    }

    if (Object.keys(labelChanges).length === 0) {
      return;
    }

    // Navigate to restart progress page with save settings
    navigate('/restart', {
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
    setVersionPinMajor(originalVersionPinMajor);
    setVersionPinMinor(originalVersionPinMinor);
    setRestartDependsOn(originalRestartDependsOn);
    setError(null);
  };

  const handleRestart = (force = false) => {
    if (!container) return;

    // Navigate to restart progress page
    navigate('/restart', {
      state: {
        restart: {
          containerName: container.container_name,
          force,
        }
      }
    });
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
      default:
        return <span className="status-badge">{container.status}</span>;
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

        {/* Status Badge */}
        <div className="status-section">
          {getStatusBadge()}
          {getChangeTypeBadge()}
        </div>
          {/* Normal content */}
          <>
              {/* Container Settings */}
              <div className="detail-section settings-section">
            <h3 className="section-title">
              <i className="fa-solid fa-cog"></i> Container Settings
            </h3>

            {loading ? (
              <div className="loading-inline">
                <div className="spinner-small"></div>
                <span>Loading settings...</span>
              </div>
            ) : (
              <>
                <div className="settings-grid">
                  {/* Ignore Flag */}
                  <div className="setting-item">
                    <label className="toggle-label">
                      <input
                        type="checkbox"
                        checked={ignoreFlag}
                        onChange={(e) => setIgnoreFlag(e.target.checked)}
                        className="toggle-input"
                      />
                      <span className="toggle-switch"></span>
                      <span className="toggle-text">
                        <strong>Ignore Container</strong>
                        <small>Exclude from update checks</small>
                      </span>
                    </label>
                  </div>

                  {/* Allow Latest Flag */}
                  <div className="setting-item">
                    <label className="toggle-label">
                      <input
                        type="checkbox"
                        checked={allowLatestFlag}
                        onChange={(e) => setAllowLatestFlag(e.target.checked)}
                        className="toggle-input"
                      />
                      <span className="toggle-switch"></span>
                      <span className="toggle-text">
                        <strong>Allow :latest Tag</strong>
                        <small>Don't suggest semver migration</small>
                      </span>
                    </label>
                  </div>

                  {/* Pin to Major Version */}
                  <div className="setting-item">
                    <label className="toggle-label">
                      <input
                        type="checkbox"
                        checked={versionPinMajor}
                        onChange={(e) => setVersionPinMajor(e.target.checked)}
                        className="toggle-input"
                      />
                      <span className="toggle-switch"></span>
                      <span className="toggle-text">
                        <strong>Pin to Major Version</strong>
                        <small>Only allow minor/patch updates</small>
                      </span>
                    </label>
                  </div>

                  {/* Pin to Minor Version */}
                  <div className="setting-item">
                    <label className="toggle-label">
                      <input
                        type="checkbox"
                        checked={versionPinMinor}
                        onChange={(e) => setVersionPinMinor(e.target.checked)}
                        className="toggle-input"
                      />
                      <span className="toggle-switch"></span>
                      <span className="toggle-text">
                        <strong>Pin to Minor Version</strong>
                        <small>Only allow patch updates</small>
                      </span>
                    </label>
                  </div>

                  {/* Tag Filter - Navigate to regex tester */}
                  <div className="setting-item full-width">
                    <label className="select-label">
                      <strong>Tag Filter (Regex)</strong>
                      <small>Filter which tags are considered</small>
                    </label>
                    <button
                      className="setting-button"
                      onClick={() => {
                        navigate(`/container/${container.container_name}/tag-filter`);
                      }}
                      style={{
                        width: '100%',
                        padding: '12px 16px',
                        background: 'var(--color-bg-tertiary)',
                        border: '1px solid var(--color-separator)',
                        borderRadius: '10px',
                        color: 'var(--color-accent)',
                        fontSize: '15px',
                        fontWeight: '500',
                        cursor: 'pointer',
                        textAlign: 'left',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                      }}
                    >
                      <span>
                        {container.labels?.['docksmith.tag-regex']
                          ? `Pattern: ${container.labels['docksmith.tag-regex']}`
                          : 'Set tag filter pattern...'}
                      </span>
                      <span style={{ marginLeft: '8px' }}>→</span>
                    </button>
                  </div>

                  {/* Pre-Update Check */}
                  <div className="setting-item full-width">
                    <label className="select-label">
                      <strong>Pre-Update Check</strong>
                      <small>Run script before updates</small>
                    </label>
                    <button
                      className="setting-button"
                      onClick={() => {
                        navigate(`/container/${container.container_name}/script-selection`, {
                          state: { currentScript: selectedScript }
                        });
                      }}
                      style={{
                        width: '100%',
                        padding: '12px 16px',
                        background: 'var(--color-bg-tertiary)',
                        border: '1px solid var(--color-separator)',
                        borderRadius: '10px',
                        color: 'var(--color-accent)',
                        fontSize: '15px',
                        fontWeight: '500',
                        cursor: 'pointer',
                        textAlign: 'left',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                      }}
                    >
                      <span>
                        {selectedScript
                          ? selectedScript.split('/').pop()
                          : 'No script selected'}
                      </span>
                      <span style={{ marginLeft: '8px' }}>→</span>
                    </button>
                    {selectedScript && (
                      <div className="current-script-indicator">
                        <i className="fa-solid fa-shield-alt"></i>
                        <strong>Current:</strong> {selectedScript.split('/').pop()}
                      </div>
                    )}
                  </div>

                  {/* Restart Dependencies */}
                  <div className="setting-item full-width">
                    <label className="select-label">
                      <strong>Restart When These Restart</strong>
                      <small>Select containers this depends on</small>
                    </label>
                    <button
                      className="setting-button"
                      onClick={() => {
                        navigate(`/container/${container.container_name}/restart-dependencies`, {
                          state: { currentDependencies: restartDependsOn }
                        });
                      }}
                      style={{
                        width: '100%',
                        padding: '12px 16px',
                        background: 'var(--color-bg-tertiary)',
                        border: '1px solid var(--color-separator)',
                        borderRadius: '10px',
                        color: 'var(--color-accent)',
                        fontSize: '15px',
                        fontWeight: '500',
                        cursor: 'pointer',
                        textAlign: 'left',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                      }}
                    >
                      <span>
                        {restartDependsOn
                          ? `${restartDependsOn.split(',').length} container${restartDependsOn.split(',').length > 1 ? 's' : ''} selected`
                          : 'No dependencies'}
                      </span>
                      <span style={{ marginLeft: '8px' }}>→</span>
                    </button>
                    {restartDependsOn && (
                      <div className="current-script-indicator">
                        <i className="fa-solid fa-link"></i>
                        <strong>Current:</strong> {restartDependsOn}
                      </div>
                    )}

                    {/* Info: This container will restart when */}
                    {restartDependsOn && (
                      <div className="info-box" style={{ marginTop: '8px' }}>
                        <i className="fa-solid fa-link"></i>
                        <div>
                          <strong>This container will restart when:</strong>
                          <p style={{ marginTop: '4px' }}>
                            {restartDependsOn.split(',').map((dep, i, arr) => (
                              <span key={dep}>
                                <code>{dep.trim()}</code>
                                {i < arr.length - 1 ? ', ' : ' restarts'}
                              </span>
                            ))}
                          </p>
                        </div>
                      </div>
                    )}

                    {/* Info: Containers that depend on this one */}
                    {dependentContainers.length > 0 && (
                      <div className="info-box warn-box" style={{ marginTop: '8px' }}>
                        <i className="fa-solid fa-triangle-exclamation"></i>
                        <div>
                          <strong>Restarting this container will also restart:</strong>
                          <p style={{ marginTop: '4px' }}>
                            {dependentContainers.map((dep, i) => (
                              <span key={dep}>
                                <code>{dep}</code>
                                {i < dependentContainers.length - 1 ? ', ' : ''}
                              </span>
                            ))}
                          </p>
                          <p style={{ marginTop: '8px', fontSize: '12px', opacity: 0.8 }}>
                            Pre-update checks will run before restarting dependents. Use Force Restart to bypass.
                          </p>
                        </div>
                      </div>
                    )}
                  </div>
                </div>

                {!hasChanges && (
                  <div className="info-box">
                    <i className="fa-solid fa-info-circle"></i>
                    <div>
                      <strong>Settings are stored in Docker Compose labels</strong>
                      <p>Changes are applied atomically: compose file → restart container → verify</p>
                    </div>
                  </div>
                )}
              </>
            )}
          </div>

          {/* Image Information */}
          <div className="detail-section">
            <h3 className="section-title">
              <i className="fa-solid fa-box"></i> Image
            </h3>
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
              {getChangeTypeBadge()}
            </div>
          </div>

          {/* Stack & Service */}
          {(container.stack || container.service) && (
            <div className="detail-section">
              <h3 className="section-title">
                <i className="fa-solid fa-layer-group"></i> Stack & Service
              </h3>
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

          {/* Dependencies */}
          {container.dependencies && container.dependencies.length > 0 && (
            <div className="detail-section">
              <h3 className="section-title">
                <i className="fa-solid fa-link"></i> Dependencies
              </h3>
              <div className="dependencies-list">
                {container.dependencies.map(dep => (
                  <span key={dep} className="dependency-tag">{dep}</span>
                ))}
              </div>
            </div>
          )}

          {/* All Labels - READ ONLY */}
          {container.labels && Object.keys(container.labels).length > 0 && (
            <div className="detail-section">
              <h3 className="section-title">
                <i className="fa-solid fa-tags"></i> All Container Labels
              </h3>
              <div className="labels-list">
                {Object.entries(container.labels)
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
                    return (
                      <div key={key} className={`label-item ${isDocksmith ? 'docksmith-label' : ''}`}>
                        <span className="label-key">{key}</span>
                        <span className="label-value">{value}</span>
                      </div>
                    );
                  })}
              </div>
            </div>
          )}
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
                onClick={() => navigate('/update', {
                  state: {
                    containers: [{
                      name: container.container_name,
                      target_version: container.latest_version || container.recommended_tag || '',
                      stack: container.stack || ''
                    }]
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
                onClick={() => navigate('/update', {
                  state: {
                    containers: [{
                      name: container.container_name,
                      target_version: container.latest_version || '',
                      stack: container.stack || '',
                      force: true
                    }]
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
                onClick={() => navigate('/update', {
                  state: {
                    containers: [{
                      name: container.container_name,
                      target_version: container.recommended_tag || '',
                      stack: container.stack || ''
                    }]
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

      {/* Changes Warning - Shows when unsaved changes */}
      {hasChanges && (
        <div className="changes-warning-banner">
          <div className="warning-content">
            <div className="warning-header">
              <i className="fa-solid fa-exclamation-triangle"></i>
              <strong>Container will be restarted</strong>
            </div>
            <div className="changes-list">
              <span>Changes to apply:</span>
              <ul>
                {getChangeSummary().map((change, i) => (
                  <li key={i}>{change}</li>
                ))}
              </ul>
            </div>
            {originalScript && (
              <div className="warning-note">
                <i className="fa-solid fa-info-circle"></i>
                Pre-update check will run before restart
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
