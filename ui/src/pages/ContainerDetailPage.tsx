import { useState, useEffect, useRef } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getContainerLabels, setLabels, restartContainer, checkContainers, getContainerStatus } from '../api/client';
import type { ContainerInfo } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { useElapsedTime } from '../hooks/useElapsedTime';
import './ContainerDetailPage.css';

// Generate a clickable URL for an image repository
function getRegistryUrl(image: string): string | null {
  // Remove tag if present
  const imageWithoutTag = image.split(':')[0];

  // GHCR
  if (imageWithoutTag.startsWith('ghcr.io/')) {
    const parts = imageWithoutTag.replace('ghcr.io/', '').split('/');
    if (parts.length >= 2) {
      const owner = parts[0];
      const repo = parts.slice(1).join('/');
      return `https://github.com/${owner}/${repo}/pkgs/container/${parts[parts.length - 1]}`;
    }
  }

  // LinuxServer (lscr.io)
  if (imageWithoutTag.startsWith('lscr.io/')) {
    const path = imageWithoutTag.replace('lscr.io/', '');
    return `https://fleet.linuxserver.io/image?name=${path}`;
  }

  // Quay.io
  if (imageWithoutTag.startsWith('quay.io/')) {
    const path = imageWithoutTag.replace('quay.io/', '');
    return `https://quay.io/repository/${path}`;
  }

  // Docker Hub (docker.io or no registry prefix)
  if (imageWithoutTag.startsWith('docker.io/') || !imageWithoutTag.includes('/') || (!imageWithoutTag.includes('.') && imageWithoutTag.includes('/'))) {
    let path = imageWithoutTag.replace('docker.io/', '');
    // Official images (no slash or library/)
    if (!path.includes('/') || path.startsWith('library/')) {
      const imageName = path.replace('library/', '');
      return `https://hub.docker.com/_/${imageName}`;
    }
    return `https://hub.docker.com/r/${path}`;
  }

  // Generic registry - just return null, can't reliably link
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
  const [versionPinMajor, setVersionPinMajor] = useState(false);
  const [versionPinMinor, setVersionPinMinor] = useState(false);
  const [restartDependsOn, setRestartDependsOn] = useState<string>('');
  const [saving, setSaving] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);
  const [preCheckFailed, setPreCheckFailed] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [dependentContainers, setDependentContainers] = useState<string[]>([]);
  const [restartProgress, setRestartProgress] = useState<{
    stage: 'stopping' | 'starting' | 'checking' | 'dependents' | 'complete' | 'failed';
    message: string;
    description?: string;
    startTime: number;
    logs: Array<{ time: number; message: string; type: 'info' | 'success' | 'error' | 'stage'; icon?: string }>;
    dependentsRestarted?: string[];
    dependentsBlocked?: string[];
    errors?: string[];
  } | null>(null);
  const restartTimeoutsRef = useRef<number[]>([]);

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

  // Use custom hook for elapsed time tracking
  const isRestarting = !!(restartProgress && restartProgress.stage !== 'complete' && restartProgress.stage !== 'failed');
  const restartElapsed = useElapsedTime(restartProgress?.startTime ?? null, isRestarting);

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

  const handleSave = async (force = false) => {
    if (!container) {
      console.error('Container is undefined in handleSave');
      setError('Container not loaded');
      return;
    }

    console.log('Starting handleSave, force:', force);
    setSaving(true);
    setError(null);
    setPreCheckFailed(false);

    // Clear any existing timeouts
    restartTimeoutsRef.current.forEach(id => clearTimeout(id));
    restartTimeoutsRef.current = [];

    // Initialize progress tracking
    const startTime = Date.now();
    const addLog = (message: string, type: 'info' | 'success' | 'error' | 'stage' = 'info', icon?: string) => {
      setRestartProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          logs: [...prev.logs, { time: Date.now(), message, type, icon }],
        };
      });
    };

    // Start progress view
    console.log('Setting restart progress');
    setRestartProgress({
      stage: 'stopping',
      message: 'Saving Settings',
      description: 'Updating compose file with new settings...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Saving settings with force restart for ${container.container_name}`
            : `Saving settings for ${container.container_name}`,
          type: 'info',
          icon: 'fa-floppy-disk'
        }
      ],
    });
    console.log('Restart progress set');

    try {
      // Build label changes
      const changes: any = {};

      if (ignoreFlag !== originalIgnore) {
        changes.ignore = ignoreFlag;
      }
      if (allowLatestFlag !== originalAllowLatest) {
        changes.allow_latest = allowLatestFlag;
      }
      if (versionPinMajor !== originalVersionPinMajor) {
        changes.version_pin_major = versionPinMajor;
      }
      if (versionPinMinor !== originalVersionPinMinor) {
        changes.version_pin_minor = versionPinMinor;
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

      addLog('Updating compose file...', 'stage', 'fa-file-pen');

      // Simulate stages for visual feedback
      const timeout1 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'stopping', message: 'Updating Compose', description: 'Writing new settings to compose file...' } : null);
      }, 300);
      restartTimeoutsRef.current.push(timeout1);

      const timeout2 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'starting', message: 'Restarting', description: 'Restarting container with new settings...' } : null);
        addLog('Restarting container...', 'stage', 'fa-rotate');
      }, 800);
      restartTimeoutsRef.current.push(timeout2);

      const timeout3 = window.setTimeout(() => {
        setRestartProgress(prev => prev ? { ...prev, stage: 'checking', message: 'Verifying', description: 'Checking that settings were applied...' } : null);
        addLog('Verifying changes...', 'stage', 'fa-check-double');
      }, 1200);
      restartTimeoutsRef.current.push(timeout3);

      // Call atomic API
      if (!container) return;
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
        addLog('Compose file updated', 'success', 'fa-check');

        if (data.pre_check_ran && data.pre_check_passed) {
          addLog('Pre-update check passed', 'success', 'fa-shield-check');
        }

        if (data.restarted) {
          addLog('Container restarted successfully', 'success', 'fa-check');
        }

        // Update originals
        setOriginalIgnore(ignoreFlag);
        setOriginalAllowLatest(allowLatestFlag);
        setOriginalVersionPinMajor(versionPinMajor);
        setOriginalVersionPinMinor(versionPinMinor);
        setOriginalScript(selectedScript);
        setOriginalRestartDependsOn(restartDependsOn);

        // Mark as complete
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: 'Complete',
          description: 'Settings saved and container restarted',
        } : null);

        addLog('Settings saved successfully', 'success', 'fa-circle-check');

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
          message: 'Pre-check Failed',
          description: 'Pre-update check script returned an error',
          errors: [errorMessage],
        } : null);
        addLog(`Pre-update check failed: ${errorMessage}`, 'error', 'fa-shield-xmark');
        setPreCheckFailed(true);
      } else {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Failed',
          description: errorMessage,
          errors: [errorMessage],
        } : null);
        addLog(`Save failed: ${errorMessage}`, 'error', 'fa-circle-xmark');
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
    setVersionPinMajor(originalVersionPinMajor);
    setRestartDependsOn(originalRestartDependsOn);
    setError(null);
    setPreCheckFailed(false);
  };

  const handleRestart = async (force = false) => {
    if (!container) return;

    setRestarting(true);
    setError(null);

    // Clear any existing timeouts
    restartTimeoutsRef.current.forEach(id => clearTimeout(id));
    restartTimeoutsRef.current = [];

    // Initialize progress tracking
    const startTime = Date.now();
    const addLog = (message: string, type: 'info' | 'success' | 'error' | 'stage' = 'info', icon?: string) => {
      setRestartProgress(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          logs: [...prev.logs, { time: Date.now(), message, type, icon }],
        };
      });
    };

    setRestartProgress({
      stage: 'stopping',
      message: force ? 'Force Restarting' : 'Restarting',
      description: 'Stopping the container gracefully...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Force restarting ${container.container_name} (bypassing pre-checks)`
            : `Restarting ${container.container_name}`,
          type: 'info',
          icon: 'fa-rotate-right'
        }
      ],
    });

    // Simulate stages since backend doesn't emit events - store timeout IDs
    const timeout1 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'stopping', message: 'Stopping', description: 'Stopping the container gracefully...' } : null);
      addLog('Stopping container...', 'stage', 'fa-circle-stop');
    }, 300);
    restartTimeoutsRef.current.push(timeout1);

    const timeout2 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'starting', message: 'Starting', description: 'Starting the container with current configuration...' } : null);
      addLog('Starting container...', 'stage', 'fa-circle-play');
    }, 800);
    restartTimeoutsRef.current.push(timeout2);

    const timeout3 = window.setTimeout(() => {
      setRestartProgress(prev => prev ? { ...prev, stage: 'checking', message: 'Health Check', description: 'Verifying container is running correctly...' } : null);
      addLog('Checking container status...', 'stage', 'fa-heart-pulse');
    }, 1200);
    restartTimeoutsRef.current.push(timeout3);

    try {
      const response = await restartContainer(container.container_name, force);

      // Clear all pending timeouts since API completed
      restartTimeoutsRef.current.forEach(id => clearTimeout(id));
      restartTimeoutsRef.current = [];

      if (response.success && response.data) {
        addLog(`Container restarted successfully`, 'success', 'fa-check');

        // Handle dependents
        const hasDependents = dependentContainers.length > 0;
        const dependentsRestarted = response.data.dependents_restarted || [];
        const dependentsBlocked = response.data.dependents_blocked || [];
        const errors = response.data.errors || [];

        if (hasDependents) {
          setRestartProgress(prev => prev ? { ...prev, stage: 'dependents', message: 'Processing Dependents', description: 'Restarting dependent containers...' } : null);
          addLog(`Processing ${dependentContainers.length} dependent container(s)...`, 'stage', 'fa-link');
        }

        // Show which dependents were restarted
        if (dependentsRestarted.length > 0) {
          dependentsRestarted.forEach(dep => {
            addLog(`Dependent restarted: ${dep}`, 'success', 'fa-check');
          });
        }

        // Show which dependents were blocked
        if (dependentsBlocked.length > 0) {
          dependentsBlocked.forEach(dep => {
            addLog(`Dependent blocked by pre-check: ${dep}`, 'error', 'fa-exclamation-triangle');
          });
        }

        // Show any errors
        if (errors.length > 0) {
          errors.forEach(err => {
            addLog(err, 'error', 'fa-xmark');
          });
        }

        // Mark as complete
        const hasErrors = dependentsBlocked.length > 0 || errors.length > 0;
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: hasErrors
            ? 'Completed with Warnings'
            : 'Complete',
          description: hasErrors
            ? 'Some dependents could not be restarted'
            : 'Container restarted successfully',
          dependentsRestarted,
          dependentsBlocked,
          errors,
        } : null);

        addLog(
          hasErrors
            ? 'Restart completed with warnings'
            : 'Restart completed successfully',
          hasErrors ? 'error' : 'success',
          hasErrors ? 'fa-exclamation-triangle' : 'fa-circle-check'
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
        message: 'Failed',
        description: errorMessage,
        errors: [errorMessage],
      } : null);

      addLog(`Restart failed: ${errorMessage}`, 'error', 'fa-circle-xmark');
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

  // Show loading state while fetching container
  if (loadingContainer) {
    return (
      <div className="container-detail-page">
        <div className="page-header">
          <button className="back-button" onClick={() => navigate('/')}>
            ← Back
          </button>
          <h1>Loading...</h1>
          <div className="header-spacer"></div>
        </div>
      </div>
    );
  }

  // Container not found or error
  if (!container) {
    return null;
  }

  return (
    <div className="container-detail-page">
      <div className="page-header">
        <button className="back-button" onClick={() => navigate('/')}>
          ← Back
        </button>
        <h1>{container.container_name}</h1>
        <div className="header-spacer"></div>
      </div>

      <div className="page-content">

        {error && (
          <div className={`error-banner ${preCheckFailed ? 'error-with-action' : ''}`}>
            <div className="error-content">
              <i className="fa-solid fa-triangle-exclamation"></i>
              <div className="error-text">
                <strong>{preCheckFailed ? 'Pre-Update Check Failed' : 'Error'}</strong>
                <p>{error}</p>
              </div>
              {preCheckFailed && (
                <button
                  className="button button-danger"
                  onClick={() => {
                    setError(null);
                    setPreCheckFailed(false);
                    hasChanges ? handleSave(true) : handleRestart(true);
                  }}
                  style={{ marginLeft: '12px' }}
                >
                  <i className="fa-solid fa-bolt"></i> Force
                </button>
              )}
            </div>
            <button className="error-dismiss" onClick={() => {
              setError(null);
              setPreCheckFailed(false);
            }}>×</button>
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

                  {/* Pin to Major Version */}
                  <div className="setting-item">
                    <label className="toggle-label">
                      <input
                        type="checkbox"
                        checked={versionPinMajor}
                        onChange={(e) => setVersionPinMajor(e.target.checked)}
                        disabled={saving}
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
                        disabled={saving}
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
                      disabled={saving}
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
                      disabled={saving}
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
                      disabled={saving}
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
      </div>

      {/* Restart Progress Modal Overlay */}
      {restartProgress && (
        <div className="progress-overlay">
          <div className="progress-modal">
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
                {restartProgress.description && (
                  <div className="restart-stage-description">{restartProgress.description}</div>
                )}
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
                      {log.icon && (
                        <span className="log-icon">
                          <i className={`fa-solid ${log.icon}`}></i>
                        </span>
                      )}
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
          </div>
        </div>
      )}

      {/* Page Footer */}
      <div className="page-footer">
        {restartProgress ? (
          // Show simple close button during restart (disabled until complete)
          <button
            className="button button-primary"
            onClick={() => {
              setRestartProgress(null);
              // Refresh container data after restart
              if (containerName) {
                const fetchContainerData = async () => {
                  try {
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
                      }
                    }
                  } catch (err) {
                    console.error('Failed to refresh container data:', err);
                  }
                };
                fetchContainerData();
              }
            }}
            disabled={restartProgress.stage !== 'complete' && restartProgress.stage !== 'failed'}
            style={{ width: '100%' }}
          >
            {restartProgress.stage === 'complete' || restartProgress.stage === 'failed'
              ? 'Done'
              : 'Restarting...'}
          </button>
        ) : (
          // Normal footer buttons
          <>
            {hasChanges && (
              <button
                className="button button-secondary"
                onClick={handleReset}
                disabled={saving || restarting}
              >
                <i className="fa-solid fa-undo"></i>
                <span>Cancel</span>
              </button>
            )}

            {/* Show Restart button when no update available or has changes */}
            {(hasChanges || (container.status !== 'UPDATE_AVAILABLE' && container.status !== 'UPDATE_AVAILABLE_BLOCKED')) && (
              <button
                className="button button-primary"
                onClick={() => hasChanges ? handleSave(false) : handleRestart(false)}
                disabled={saving || restarting}
              >
                {saving || restarting ? (
                  <>
                    <div className="spinner-inline"></div>
                    <span>{hasChanges ? 'Saving...' : 'Restarting...'}</span>
                  </>
                ) : (
                  <>
                    <i className={hasChanges ? "fa-solid fa-save" : "fa-solid fa-rotate-right"}></i>
                    <span>{hasChanges ? 'Save & Restart' : 'Restart'}</span>
                  </>
                )}
              </button>
            )}

            {/* Show Update button on the right when update available */}
            {(container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED') && !hasChanges && (
              <button
                className="button button-accent"
                onClick={() => {
                  // Navigate to dashboard, which will handle the update
                  navigate('/');
                }}
                disabled={saving || restarting}
              >
                <i className="fa-solid fa-arrow-up"></i>
                <span>Update</span>
              </button>
            )}
          </>
        )}
      </div>

      {/* Changes Warning - Shows when unsaved changes */}
      {hasChanges && !restartProgress && (
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
