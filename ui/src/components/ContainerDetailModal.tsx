import { useState, useEffect } from 'react';
import { getScripts, getContainerLabels, setLabels, restartContainer, checkContainers } from '../api/client';
import type { ContainerInfo, Script } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';

interface ContainerDetailModalProps {
  container: ContainerInfo;
  onClose: () => void;
  onRefresh?: () => void;
  onUpdate?: (containerName: string) => void;
}

export function ContainerDetailModal({ container, onClose, onRefresh, onUpdate }: ContainerDetailModalProps) {
  const [scripts, setScripts] = useState<Script[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<React.ReactNode | null>(null);
  const [selectedScript, setSelectedScript] = useState<string>('');
  const [ignoreFlag, setIgnoreFlag] = useState(false);
  const [allowLatestFlag, setAllowLatestFlag] = useState(false);
  const [restartDependsOn, setRestartDependsOn] = useState<string>('');
  const [saving, setSaving] = useState(false);
  const [saveStatus, setSaveStatus] = useState<string>('');
  const [hasChanges, setHasChanges] = useState(false);
  const [preCheckFailed, setPreCheckFailed] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [restartStatus, setRestartStatus] = useState<string>('');
  const [dependentContainers, setDependentContainers] = useState<string[]>([]);
  const [showForceRestart, setShowForceRestart] = useState(false);

  // Track original values to detect changes
  const [originalScript, setOriginalScript] = useState<string>('');
  const [originalIgnore, setOriginalIgnore] = useState(false);
  const [originalAllowLatest, setOriginalAllowLatest] = useState(false);
  const [originalRestartDependsOn, setOriginalRestartDependsOn] = useState<string>('');

  useEffect(() => {
    fetchData();
  }, [container.container_name]);

  useEffect(() => {
    // Check if any settings have changed
    const scriptChanged = selectedScript !== originalScript;
    const ignoreChanged = ignoreFlag !== originalIgnore;
    const allowLatestChanged = allowLatestFlag !== originalAllowLatest;
    const restartDependsOnChanged = restartDependsOn !== originalRestartDependsOn;
    setHasChanges(scriptChanged || ignoreChanged || allowLatestChanged || restartDependsOnChanged);
  }, [selectedScript, ignoreFlag, allowLatestFlag, restartDependsOn, originalScript, originalIgnore, originalAllowLatest, originalRestartDependsOn]);

  const fetchData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [scriptsResponse, labelsResponse, containersResponse] = await Promise.all([
        getScripts(),
        getContainerLabels(container.container_name),
        checkContainers(),
      ]);

      if (scriptsResponse.success && scriptsResponse.data) {
        setScripts(scriptsResponse.data.scripts || []);
      }

      if (labelsResponse.success && labelsResponse.data) {
        const labels = labelsResponse.data.labels || {};

        const scriptPath = labels['docksmith.pre-update-check'] || '';
        const ignore = labels['docksmith.ignore'] === 'true';
        const allowLatest = labels['docksmith.allow-latest'] === 'true';
        const restartDeps = labels['docksmith.restart-depends-on'] || '';

        setSelectedScript(scriptPath);
        setIgnoreFlag(ignore);
        setAllowLatestFlag(allowLatest);
        setRestartDependsOn(restartDeps);

        setOriginalScript(scriptPath);
        setOriginalIgnore(ignore);
        setOriginalAllowLatest(allowLatest);
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

        // Determine if Force Restart should be shown
        // Show if this container has a pre-update check OR any dependent has a pre-update check
        const hasOwnPreCheck = !!(labelsResponse.success && labelsResponse.data?.labels?.['docksmith.pre-update-check']);
        const hasDependentPreChecks = dependentContainersData.some(c =>
          c.labels?.['docksmith.pre-update-check']
        );
        setShowForceRestart(hasOwnPreCheck || hasDependentPreChecks);
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

  const handleSave = async (force = false) => {
    setSaving(true);
    setError(null);
    setSaveStatus('Preparing changes...');
    setPreCheckFailed(false);

    try {
      // Build label changes
      const changes: any = {};

      if (ignoreFlag !== originalIgnore) {
        changes.ignore = ignoreFlag;
      }
      if (allowLatestFlag !== originalAllowLatest) {
        changes.allow_latest = allowLatestFlag;
      }
      if (selectedScript !== originalScript) {
        changes.script = selectedScript || '';
      }
      if (restartDependsOn !== originalRestartDependsOn) {
        changes.restart_depends_on = restartDependsOn || '';
      }

      if (Object.keys(changes).length === 0) {
        setSaving(false);
        return;
      }

      // Show what's happening
      setSaveStatus('Updating compose file...');

      // Call atomic API
      const result = await setLabels(container.container_name, {
        ...changes,
        force,
      });

      if (result.success && result.data) {
        const data = result.data;

        // Update status based on what happened
        if (data.pre_check_ran && data.pre_check_passed) {
          setSaveStatus('Pre-update check passed ✓');
        }

        if (data.restarted) {
          setSaveStatus('Container restarted successfully ✓');
        }

        // Update originals
        setOriginalIgnore(ignoreFlag);
        setOriginalAllowLatest(allowLatestFlag);
        setOriginalScript(selectedScript);
        setOriginalRestartDependsOn(restartDependsOn);

        // Refresh parent
        if (onRefresh) {
          setTimeout(() => {
            onRefresh();
          }, 500);
        }

        // Show success briefly
        setTimeout(() => {
          setSaveStatus('');
          setSaving(false);
        }, 2000);

      } else {
        throw new Error(result.error || 'Failed to update labels');
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : String(err);

      // Check if it's a pre-update check failure
      if (errorMessage.includes('pre-update check failed') || errorMessage.includes('script exited with code')) {
        setError(errorMessage);
        setPreCheckFailed(true);
        setSaveStatus('');
      } else {
        setError(errorMessage);
        setSaveStatus('');
      }
      setSaving(false);
    }
  };

  const handleReset = () => {
    setSelectedScript(originalScript);
    setIgnoreFlag(originalIgnore);
    setAllowLatestFlag(originalAllowLatest);
    setRestartDependsOn(originalRestartDependsOn);
    setError(null);
    setPreCheckFailed(false);
  };

  const handleRestart = async (force = false) => {
    setRestarting(true);
    setRestartStatus(force ? 'Force restarting container...' : 'Restarting container...');
    setError(null);

    try {
      const response = await restartContainer(container.container_name, force);

      if (response.success && response.data) {
        let message = 'Container restarted successfully';

        // Show dependent containers that were also restarted
        if (response.data.dependents_restarted && response.data.dependents_restarted.length > 0) {
          const deps = response.data.dependents_restarted.join(', ');
          message = `Container and ${response.data.dependents_restarted.length} dependent(s) restarted: ${deps}`;
        }

        // Show blocked containers and offer force option
        if (response.data.dependents_blocked && response.data.dependents_blocked.length > 0) {
          const blockedDeps = response.data.dependents_blocked.join(', ');
          const errorMsg = `${response.data.dependents_blocked.length} dependent(s) blocked by pre-update checks: ${blockedDeps}`;

          setError(
            <div>
              <p>{errorMsg}</p>
              <div style={{ marginTop: '8px' }}>
                <button
                  className="btn-warning"
                  onClick={() => handleRestart(true)}
                  disabled={restarting}
                  style={{ marginRight: '8px' }}
                >
                  <i className="fa-solid fa-bolt"></i> Force Restart All
                </button>
              </div>
            </div>
          );
          setRestartStatus('');
          setRestarting(false);
          return;
        }

        // Show any errors from dependent restarts
        if (response.data.errors && response.data.errors.length > 0) {
          setError(`Some dependents had issues: ${response.data.errors.join('; ')}`);
        }

        setRestartStatus(message);
        setTimeout(() => {
          setRestartStatus('');
          if (onRefresh) {
            onRefresh();
          }
        }, 3000);
      } else {
        setError(response.error || 'Failed to restart container');
        setRestartStatus('');
      }
    } catch (err) {
      // Improve error message for network/timeout errors
      if (err instanceof Error) {
        if (err.message.includes('fetch')) {
          setError('Request timed out or network error. Check if the container restarted successfully.');
        } else {
          setError(err.message);
        }
      } else {
        setError('Failed to restart container: Unknown error');
      }
      setRestartStatus('');
    } finally {
      setRestarting(false);
    }
  };

  const getStatusBadge = () => {
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

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal container-detail-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div className="container-detail-title">
            <h2>{container.container_name}</h2>
            {getStatusBadge()}
          </div>
          <button className="close-btn" onClick={onClose}>×</button>
        </div>

        {error && (
          <div className={`error-banner ${preCheckFailed ? 'error-with-action' : ''}`}>
            <div className="error-content">
              <i className="fa-solid fa-triangle-exclamation"></i>
              <div className="error-text">
                <strong>{preCheckFailed ? 'Pre-Update Check Failed' : 'Error'}</strong>
                <p>{error}</p>
              </div>
            </div>
            <button className="error-dismiss" onClick={() => setError(null)}>×</button>
          </div>
        )}

        {saveStatus && (
          <div className="status-banner">
            <div className="status-content">
              {saving && <div className="spinner-small"></div>}
              <span>{saveStatus}</span>
            </div>
          </div>
        )}

        <div className="modal-body">
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
                        disabled={saving}
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
                        disabled={saving}
                        className="toggle-input"
                      />
                      <span className="toggle-switch"></span>
                      <span className="toggle-text">
                        <strong>Allow :latest Tag</strong>
                        <small>Don't suggest semver migration</small>
                      </span>
                    </label>
                  </div>

                  {/* Pre-Update Check */}
                  <div className="setting-item full-width">
                    <label className="select-label">
                      <strong>Pre-Update Check</strong>
                      <small>Run script before updates</small>
                    </label>
                    <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                      <select
                        value={selectedScript}
                        onChange={(e) => setSelectedScript(e.target.value)}
                        disabled={saving}
                        className="setting-select"
                        style={{ flex: 1 }}
                      >
                        <option value="">No script</option>
                        {scripts.map(s => (
                          <option key={s.path} value={s.path} disabled={!s.executable}>
                            {s.name} {s.executable ? '' : '(not executable)'}
                          </option>
                        ))}
                      </select>
                      {selectedScript && (
                        <button
                          className="btn-clear"
                          onClick={() => setSelectedScript('')}
                          disabled={saving}
                          title="Clear script"
                        >
                          <i className="fa-solid fa-times"></i>
                        </button>
                      )}
                    </div>
                    {originalScript && (
                      <div className="current-script-indicator">
                        <i className="fa-solid fa-shield-alt"></i>
                        <strong>Current:</strong> {originalScript.split('/').pop()}
                      </div>
                    )}
                  </div>

                  {/* Restart Dependencies */}
                  <div className="setting-item full-width">
                    <label className="select-label">
                      <strong>Restart When These Restart</strong>
                      <small>Comma-separated container names</small>
                    </label>
                    <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                      <input
                        type="text"
                        value={restartDependsOn}
                        onChange={(e) => setRestartDependsOn(e.target.value)}
                        disabled={saving}
                        placeholder="e.g., vpn, tailscale"
                        className="setting-select"
                        style={{ flex: 1 }}
                      />
                      {restartDependsOn && (
                        <button
                          className="btn-clear"
                          onClick={() => setRestartDependsOn('')}
                          disabled={saving}
                          title="Clear dependencies"
                        >
                          <i className="fa-solid fa-times"></i>
                        </button>
                      )}
                    </div>
                    {originalRestartDependsOn && (
                      <div className="current-script-indicator">
                        <i className="fa-solid fa-link"></i>
                        <strong>Current:</strong> {originalRestartDependsOn}
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
                <span className="detail-value mono">{container.image}</span>
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
        </div>

        {restartStatus && (
          <div className="save-status success">
            <i className="fa-solid fa-check-circle"></i> {restartStatus}
          </div>
        )}

        {/* Changes Warning - Anchored near footer */}
        {hasChanges && (
          <div className="settings-changes" style={{
            margin: '0',
            borderTop: '1px solid var(--border-color)',
            borderRadius: '0',
            padding: '12px 20px'
          }}>
            <div className="changes-warning">
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

        <div className="modal-footer">
          <button className="btn-secondary" onClick={onClose}>Close</button>

          {hasChanges && (
            <button
              className="btn-secondary"
              onClick={handleReset}
              disabled={saving || restarting}
            >
              <i className="fa-solid fa-undo"></i> Cancel
            </button>
          )}

          {!showForceRestart && (
            <button
              className="btn-secondary"
              onClick={() => hasChanges ? handleSave(false) : handleRestart(false)}
              disabled={saving || restarting}
            >
              {saving || restarting ? (
                <>
                  <div className="spinner-inline"></div> {hasChanges ? 'Saving...' : 'Restarting...'}
                </>
              ) : (
                <>
                  <i className={hasChanges ? "fa-solid fa-save" : "fa-solid fa-rotate-right"}></i> {hasChanges ? 'Save & Restart' : 'Restart'}
                </>
              )}
            </button>
          )}

          {showForceRestart && (
            <button
              className="btn-warning"
              onClick={() => hasChanges ? handleSave(true) : handleRestart(true)}
              disabled={saving || restarting}
              title="Force restart, bypassing pre-update checks"
            >
              <i className="fa-solid fa-bolt"></i> Force {hasChanges ? 'Save & ' : ''}Restart
            </button>
          )}

          {!hasChanges && container.status === 'UPDATE_AVAILABLE' && onUpdate && (
            <button
              className="btn-primary"
              onClick={() => {
                onUpdate(container.container_name);
                onClose();
              }}
            >
              <i className="fa-solid fa-arrow-up"></i> Update Now
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
