import { useState, useEffect, useRef } from 'react';
import { getScripts, getContainerLabels, setLabels, restartContainer, checkContainers } from '../api/client';
import type { ContainerInfo, Script } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { useElapsedTime } from '../hooks/useElapsedTime';

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
  const [hasChanges, setHasChanges] = useState(false);
  const [preCheckFailed, setPreCheckFailed] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [dependentContainers, setDependentContainers] = useState<string[]>([]);
  const [showForceRestart, setShowForceRestart] = useState(false);
  const [restartProgress, setRestartProgress] = useState<{
    stage: 'stopping' | 'starting' | 'checking' | 'dependents' | 'complete' | 'failed';
    message: string;
    startTime: number;
    logs: Array<{ time: number; message: string; type: 'info' | 'success' | 'error' }>;
    dependentsRestarted?: string[];
    dependentsBlocked?: string[];
    errors?: string[];
  } | null>(null);
  const restartTimeoutsRef = useRef<number[]>([]);

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

  // Use custom hook for elapsed time tracking
  const isRestarting = !!(restartProgress && restartProgress.stage !== 'complete' && restartProgress.stage !== 'failed');
  const restartElapsed = useElapsedTime(restartProgress?.startTime ?? null, isRestarting);

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
    setPreCheckFailed(false);

    // Clear any existing timeouts
    restartTimeoutsRef.current.forEach(id => clearTimeout(id));
    restartTimeoutsRef.current = [];

    // Initialize progress tracking
    const startTime = Date.now();
    const addLog = (message: string, type: 'info' | 'success' | 'error' = 'info') => {
      setRestartProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          logs: [...prev.logs, { time: Date.now(), message, type }],
        };
      });
    };

    // Start progress view
    setRestartProgress({
      stage: 'stopping',
      message: 'Saving settings and restarting...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Saving settings with force restart for ${container.container_name}`
            : `Saving settings for ${container.container_name}`,
          type: 'info'
        }
      ],
    });

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
        setRestartProgress(null);
        return;
      }

      addLog('Updating compose file...', 'info');

      // Simulate stages for visual feedback
      const timeout1 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'stopping', message: 'Updating compose file...' } : null);
      }, 300);
      restartTimeoutsRef.current.push(timeout1);

      const timeout2 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'starting', message: 'Restarting container...' } : null);
      }, 800);
      restartTimeoutsRef.current.push(timeout2);

      const timeout3 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'checking', message: 'Verifying changes...' } : null);
      }, 1200);
      restartTimeoutsRef.current.push(timeout3);

      // Call atomic API
      const result = await setLabels(container.container_name, {
        ...changes,
        force,
      });

      // Clear all pending timeouts since API completed
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];

      if (result.success && result.data) {
        const data = result.data;

        // Log what happened
        addLog('✓ Compose file updated', 'success');

        if (data.pre_check_ran && data.pre_check_passed) {
          addLog('✓ Pre-update check passed', 'success');
        }

        if (data.restarted) {
          addLog('✓ Container restarted successfully', 'success');
        }

        // Update originals
        setOriginalIgnore(ignoreFlag);
        setOriginalAllowLatest(allowLatestFlag);
        setOriginalScript(selectedScript);
        setOriginalRestartDependsOn(restartDependsOn);

        // Mark as complete
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: 'Settings saved and container restarted',
        } : null);

        addLog('✓ Settings saved successfully', 'success');

        // Don't auto-refresh - let user close the modal first
      } else {
        throw new Error(result.error || 'Failed to update labels');
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : String(err);

      // Clear all pending timeouts on error
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];

      // Check if it's a pre-update check failure
      if (errorMessage.includes('pre-update check failed') || errorMessage.includes('script exited with code')) {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Pre-update check failed',
          errors: [errorMessage],
        } : null);
        addLog(`✗ Pre-update check failed: ${errorMessage}`, 'error');
        setPreCheckFailed(true);
      } else {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Save failed',
          errors: [errorMessage],
        } : null);
        addLog(`✗ Save failed: ${errorMessage}`, 'error');
      }
    } finally {
      // Ensure all timeouts are cleared
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];
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
    setError(null);

    // Clear any existing timeouts
    restartTimeoutsRef.current.forEach(id => clearTimeout(id));
    restartTimeoutsRef.current = [];

    // Initialize progress tracking
    const startTime = Date.now();
    const addLog = (message: string, type: 'info' | 'success' | 'error' = 'info') => {
      setRestartProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          logs: [...prev.logs, { time: Date.now(), message, type }],
        };
      });
    };

    setRestartProgress({
      stage: 'stopping',
      message: force ? 'Force restarting container...' : 'Restarting container...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Force restarting ${container.container_name} (bypassing pre-checks)`
            : `Restarting ${container.container_name}`,
          type: 'info'
        }
      ],
    });

    // Simulate stages since backend doesn't emit events - store timeout IDs
    const timeout1 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'stopping', message: 'Stopping container...' } : null);
      addLog('Stopping container...', 'info');
    }, 300);
    restartTimeoutsRef.current.push(timeout1);

    const timeout2 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'starting', message: 'Starting container...' } : null);
      addLog('Starting container...', 'info');
    }, 800);
    restartTimeoutsRef.current.push(timeout2);

    const timeout3 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'checking', message: 'Checking container status...' } : null);
    }, 1200);
    restartTimeoutsRef.current.push(timeout3);

    try {
      const response = await restartContainer(container.container_name, force);

      // Clear all pending timeouts since API completed
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];

      if (response.success && response.data) {
        addLog(`✓ Container restarted successfully`, 'success');

        // Handle dependents
        const hasDependents = dependentContainers.length > 0;
        const dependentsRestarted = response.data.dependents_restarted || [];
        const dependentsBlocked = response.data.dependents_blocked || [];
        const errors = response.data.errors || [];

        if (hasDependents) {
          setRestartProgress(prev => prev ? { ...prev, stage: 'dependents', message: 'Processing dependent containers...' } : null);
          addLog(`Processing ${dependentContainers.length} dependent container(s)...`, 'info');
        }

        // Show which dependents were restarted
        if (dependentsRestarted.length > 0) {
          dependentsRestarted.forEach(dep => {
            addLog(`✓ Dependent restarted: ${dep}`, 'success');
          });
        }

        // Show which dependents were blocked
        if (dependentsBlocked.length > 0) {
          dependentsBlocked.forEach(dep => {
            addLog(`⚠ Dependent blocked by pre-check: ${dep}`, 'error');
          });
        }

        // Show any errors
        if (errors.length > 0) {
          errors.forEach(err => {
            addLog(`✗ ${err}`, 'error');
          });
        }

        // Mark as complete
        const hasErrors = dependentsBlocked.length > 0 || errors.length > 0;
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: hasErrors
            ? 'Restart completed with warnings'
            : 'Restart completed successfully',
          dependentsRestarted,
          dependentsBlocked,
          errors,
        } : null);

        addLog(
          hasErrors
            ? '⚠ Restart completed with warnings - some dependents may not have restarted'
            : '✓ Restart completed successfully',
          hasErrors ? 'error' : 'success'
        );

        // Don't auto-refresh - let user close the modal first
      } else {
        throw new Error(response.error || 'Failed to restart container');
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Unknown error';

      // Clear all pending timeouts on error
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];

      setRestartProgress(prev => prev ? {
        ...prev,
        stage: 'failed',
        message: 'Restart failed',
        errors: [errorMessage],
      } : null);

      addLog(`✗ Restart failed: ${errorMessage}`, 'error');
    } finally {
      // Ensure all timeouts are cleared
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];
      setRestarting(false);
    }
  };

  const getRestartStageIcon = (stage: string) => {
    switch (stage) {
      case 'stopping':
        return <i className="fa-solid fa-circle-stop"></i>;
      case 'starting':
        return <i className="fa-solid fa-circle-play"></i>;
      case 'checking':
        return <i className="fa-solid fa-heartbeat"></i>;
      case 'dependents':
        return <i className="fa-solid fa-link"></i>;
      case 'complete':
        return <i className="fa-solid fa-circle-check"></i>;
      case 'failed':
        return <i className="fa-solid fa-circle-xmark"></i>;
      default:
        return <i className="fa-solid fa-rotate-right"></i>;
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

        <div className="modal-body">
          {/* Restart Progress View - replaces modal content during restart */}
          {restartProgress ? (
            <div className="restart-progress-view">
              {/* Container info */}
              <div className="restart-container-info">
                <div className="container-name-display">
                  <i className="fa-solid fa-box"></i>
                  <span>{container.container_name}</span>
                </div>
                <div className="restart-stats">
                  <span>Elapsed: {restartElapsed}s</span>
                  {dependentContainers.length > 0 && (
                    <span>Dependents: {dependentContainers.length}</span>
                  )}
                </div>
              </div>

              {/* Current stage indicator */}
              <div className="restart-stage-display">
                <div className={`restart-stage-icon ${restartProgress.stage === 'failed' ? 'failed' : restartProgress.stage === 'complete' ? 'complete' : 'in-progress'}`}>
                  {restartProgress.stage !== 'complete' && restartProgress.stage !== 'failed' ? (
                    <div className="stage-icon-wrapper">
                      {getRestartStageIcon(restartProgress.stage)}
                      <div className="spinner-ring"></div>
                    </div>
                  ) : (
                    getRestartStageIcon(restartProgress.stage)
                  )}
                </div>
                <div className="restart-stage-message">{restartProgress.message}</div>
              </div>

              {/* Activity log */}
              <div className="update-activity-log">
                <div className="log-header">Activity Log:</div>
                <div className="log-entries" style={{ maxHeight: '250px', overflowY: 'auto' }}>
                  {restartProgress.logs.map((log, i) => (
                    <div key={i} className={`log-entry log-${log.type}`}>
                      <span className="log-time">
                        [{new Date(log.time).toLocaleTimeString('en-US', { hour12: false })}]
                      </span>
                      <span className="log-message">{log.message}</span>
                    </div>
                  ))}
                </div>
              </div>

              {/* Completion summary */}
              {(restartProgress.stage === 'complete' || restartProgress.stage === 'failed') && (
                <div className="restart-completion">
                  {restartProgress.stage === 'complete' ? (
                    <>
                      <div className={restartProgress.dependentsBlocked && restartProgress.dependentsBlocked.length > 0 ? 'completion-warning' : 'completion-success'}>
                        <i className={restartProgress.dependentsBlocked && restartProgress.dependentsBlocked.length > 0 ? 'fa-solid fa-exclamation-triangle' : 'fa-solid fa-check'}></i>
                        {' '}
                        {restartProgress.dependentsBlocked && restartProgress.dependentsBlocked.length > 0
                          ? 'Restart completed with warnings'
                          : 'Container restarted successfully!'}
                      </div>
                      {restartProgress.dependentsRestarted && restartProgress.dependentsRestarted.length > 0 && (
                        <div className="dependents-summary success">
                          <i className="fa-solid fa-check-circle"></i> {restartProgress.dependentsRestarted.length} dependent(s) restarted
                        </div>
                      )}
                      {restartProgress.dependentsBlocked && restartProgress.dependentsBlocked.length > 0 && (
                        <div className="dependents-summary warning">
                          <i className="fa-solid fa-exclamation-circle"></i> {restartProgress.dependentsBlocked.length} dependent(s) blocked by pre-checks
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="completion-error">
                      <i className="fa-solid fa-xmark"></i> Restart failed
                      {restartProgress.errors && restartProgress.errors.length > 0 && (
                        <div style={{ marginTop: '8px', fontSize: '0.9em' }}>
                          {restartProgress.errors.map((err, i) => (
                            <div key={i}>{err}</div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          ) : (
            <>
              {/* Normal modal content */}
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
            </>
          )}
        </div>

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
          {restartProgress ? (
            // Show simple close button during restart (disabled until complete)
            <button
              className="btn-primary"
              onClick={() => {
                setRestartProgress(null);
                // Refresh and close modal when user dismisses restart view
                if (onRefresh) {
                  onRefresh();
                }
                onClose();
              }}
              disabled={restartProgress.stage !== 'complete' && restartProgress.stage !== 'failed'}
              style={{ width: '100%' }}
            >
              {restartProgress.stage === 'complete' || restartProgress.stage === 'failed'
                ? 'Close'
                : 'Restarting...'}
            </button>
          ) : (
            // Normal footer buttons
            <>
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
            </>
          )}
        </div>

      </div>
    </div>
  );
}
