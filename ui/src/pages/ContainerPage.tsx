import { useState, useEffect, useLayoutEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import {
  getContainerLogs,
  inspectContainer,
  getContainerLabels,
  getContainerStatus,
  recheckContainer,
} from '../api/client';
import type { ContainerInspect, ContainerInfo } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { getRegistryUrl } from '../utils/registry';
import { isUpdatable } from '../utils/status';
import { useToast } from '../components/Toast';
import { Tooltip } from '../components/shared';
import { SkeletonDetailSection } from '../components/Skeleton/Skeleton';
import { ansiToHtml } from '../utils/ansi';
import { getCachedInspect, setCachedInspect } from '../api/snapshot';
import { useEventStream } from '../context/EventStreamContext';
import '../styles/container-page.css';

type TabId = 'overview' | 'config' | 'logs' | 'inspect';

// Format bytes to human-readable size
function formatSize(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

// Format timestamp
function formatTimestamp(timestamp: string): string {
  if (!timestamp || timestamp === '0001-01-01T00:00:00Z') return 'Never';
  const date = new Date(timestamp);
  return date.toLocaleString();
}


export function ContainerPage() {
  // Support both route patterns: /container/:containerName and /explorer/container/:name
  const { name, containerName: routeContainerName } = useParams<{ name?: string; containerName?: string }>();
  const containerName = name || routeContainerName || '';
  return <ContainerDetail key={containerName} containerName={containerName} />;
}

function ContainerDetail({ containerName }: { containerName: string }) {

  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const { reconnecting } = useEventStream();

  // Get initial tab from location state
  const incoming = location.state as { tab?: TabId; selectedScript?: string; restartAfter?: string; tagRegex?: string } | null;
  const initialTab = incoming?.tab || 'overview';
  const [activeTab, setActiveTab] = useState<TabId>(initialTab);

  // Navigate back to containers list
  const handleBack = useCallback(() => {
    navigate('/');
  }, [navigate]);

  // Core state
  const [loading, setLoading] = useState(() => !getCachedInspect(containerName));
  const [error, setError] = useState<string | null>(null);
  const [inspectData, setInspectData] = useState<ContainerInspect | null>(() => getCachedInspect(containerName));
  const [docksmithData, setDocksmithData] = useState<ContainerInfo | null>(null);

  // Logs state
  const [logs, setLogs] = useState<string>('');
  const [logsLoading, setLogsLoading] = useState(initialTab === 'logs');
  const [tailLines, setTailLines] = useState(500);
  const [autoScroll, setAutoScroll] = useState(true);
  const [showScrollButton, setShowScrollButton] = useState(false);
  const logsViewerRef = useRef<HTMLDivElement>(null);

  // Action state
  const [confirmRemove, setConfirmRemove] = useState(false);

  // Docksmith settings state
  const [selectedScript, setSelectedScript] = useState(incoming?.selectedScript || '');
  const [ignoreFlag, setIgnoreFlag] = useState(false);
  const [allowLatestFlag, setAllowLatestFlag] = useState(false);
  const [versionPin, setVersionPin] = useState<'none' | 'patch' | 'minor' | 'major'>('none');
  const [restartAfter, setRestartAfter] = useState(incoming?.restartAfter || '');
  const [tagRegex, setTagRegex] = useState(incoming?.tagRegex || '');
  const [notifyFlag, setNotifyFlag] = useState(true);
  const [notifyLevel, setNotifyLevel] = useState<string>('');
  const [notifyDigest, setNotifyDigest] = useState<'' | 'true' | 'false'>('');
  const [refreshingPrecheck, setRefreshingPrecheck] = useState(false);
  const [activeHelp, setActiveHelp] = useState<string | null>(null);

  // Help content for each setting
  const helpContent: Record<string, { title: string; description: string }> = {
    ignore: {
      title: 'Ignore Container',
      description: 'When enabled, Docksmith will skip this container during update checks. Useful for containers you want to manage manually or that should stay on a specific version.',
    },
    allowLatest: {
      title: 'Allow :latest Tag',
      description: 'By default, containers using the :latest tag are skipped because version comparison isn\'t possible. Enable this to allow updates for :latest tagged containers.',
    },
    versionPin: {
      title: 'Version pin',
      description: 'Restrict updates to certain version increments. Patch: only bug fixes (1.0.0 → 1.0.1). Minor: features + fixes (1.0.0 → 1.1.0). Major: all updates including breaking changes.',
    },
    tagFilter: {
      title: 'Tag Filter',
      description: 'Use a regex pattern to filter which tags are considered for updates. For example, "^v?[0-9.]+-alpine$" only matches Alpine-based tags.',
    },
    preUpdateScript: {
      title: 'Pre-Update Script',
      description: 'Run a custom script before updating. If the script exits with a non-zero code, the update is blocked. Useful for health checks or backup verification.',
    },
    notify: {
      title: 'Notifications',
      description: 'Turn off to mute all notifications for this container. Notification services are configured in Settings.',
    },
    notifyLevel: {
      title: 'Notification Level',
      description: 'Override the global minimum update level for new-version notifications on this container. "Use global" follows the setting in Settings.',
    },
    notifyDigest: {
      title: 'Digest Notifications',
      description: 'Notify when the image digest changes for this container, even if the tag stays the same.',
    },
    restartDeps: {
      title: 'Restart Dependencies',
      description: 'Automatically restart selected containers after this container is updated. Useful for services that depend on this container.',
    },
  };

  // Track original values for change detection
  const [originalScript, setOriginalScript] = useState<string>('');
  const [originalIgnore, setOriginalIgnore] = useState(false);
  const [originalAllowLatest, setOriginalAllowLatest] = useState(false);
  const [originalVersionPin, setOriginalVersionPin] = useState<'none' | 'patch' | 'minor' | 'major'>('none');
  const [originalRestartAfter, setOriginalRestartAfter] = useState<string>('');
  const [originalTagRegex, setOriginalTagRegex] = useState<string>('');
  const [originalNotify, setOriginalNotify] = useState(true);
  const [originalNotifyLevel, setOriginalNotifyLevel] = useState<string>('');
  const [originalNotifyDigest, setOriginalNotifyDigest] = useState<'' | 'true' | 'false'>('');

  // Track pending state from sub-pages
  const pendingStateRef = useRef({ selectedScript: incoming?.selectedScript, restartAfter: incoming?.restartAfter, tagRegex: incoming?.tagRegex });
  const [previousIncoming, setPreviousIncoming] = useState(incoming);
  if (previousIncoming !== incoming) {
    setPreviousIncoming(incoming);
    if (incoming?.selectedScript !== undefined) setSelectedScript(incoming.selectedScript);
    if (incoming?.restartAfter !== undefined) setRestartAfter(incoming.restartAfter);
    if (incoming?.tagRegex !== undefined) setTagRegex(incoming.tagRegex);
  }
  useLayoutEffect(() => {
    if (incoming?.selectedScript !== undefined || incoming?.restartAfter !== undefined || incoming?.tagRegex !== undefined) {
      pendingStateRef.current = { selectedScript: incoming.selectedScript, restartAfter: incoming.restartAfter, tagRegex: incoming.tagRegex };
    }
  }, [incoming]);

  // Fetch container inspection data
  const fetchInspect = useCallback(() => inspectContainer(containerName).then(response => {
      if (response.success && response.data) {
        setCachedInspect(containerName, response.data);
        setInspectData(response.data);
        setError(null);
      } else {
        setError(response.error || 'Failed to fetch container info');
      }
  }).catch(error => setError(error instanceof Error ? error.message : 'Unknown error'))
    .finally(() => setLoading(false)), [containerName]);

  // Fetch Docksmith-specific data (update status, labels, settings)
  const fetchDocksmithData = useCallback(() => Promise.all([
        getContainerStatus(),
        getContainerLabels(containerName),
      ]).then(([statusResponse, labelsResponse]) => {
      if (statusResponse.success && statusResponse.data) {
        const foundContainer = statusResponse.data.containers.find(
          (c) => c.container_name === containerName
        );

        if (foundContainer && labelsResponse.success && labelsResponse.data) {
          setDocksmithData({
            ...foundContainer,
            labels: labelsResponse.data.labels || {},
          });

          // Parse labels into settings
          const labels = labelsResponse.data.labels || {};
          const scriptPath = labels['docksmith.pre-update-check'] || '';
          const ignore = labels['docksmith.ignore'] === 'true';
          const allowLatest = labels['docksmith.allow-latest'] === 'true';
          const restartDeps = labels['docksmith.restart-after'] || '';
          const tagRegexValue = labels['docksmith.tag-regex'] || '';
          const notify = labels['docksmith.notify'] !== 'false';
          const notifyLevelValue = labels['docksmith.notify-level'] || '';
          const notifyDigestValue: '' | 'true' | 'false' = labels['docksmith.notify-digest'] === 'true' ? 'true' : labels['docksmith.notify-digest'] === 'false' ? 'false' : '';

          // Convert version pin labels to single value
          let pin: 'none' | 'patch' | 'minor' | 'major' = 'none';
          if (labels['docksmith.version-pin-major'] === 'true') pin = 'major';
          else if (labels['docksmith.version-pin-minor'] === 'true') pin = 'minor';
          else if (labels['docksmith.version-pin-patch'] === 'true') pin = 'patch';

          // Don't overwrite values from sub-page navigation
          const hasPendingScript = pendingStateRef.current.selectedScript !== undefined;
          const hasPendingRestartAfter = pendingStateRef.current.restartAfter !== undefined;
          const hasPendingTagRegex = pendingStateRef.current.tagRegex !== undefined;

          if (!hasPendingScript) setSelectedScript(scriptPath);
          setIgnoreFlag(ignore);
          setAllowLatestFlag(allowLatest);
          setNotifyFlag(notify);
          setNotifyLevel(notifyLevelValue);
          setNotifyDigest(notifyDigestValue);
          setVersionPin(pin);
          if (!hasPendingRestartAfter) setRestartAfter(restartDeps);
          if (!hasPendingTagRegex) setTagRegex(tagRegexValue);

          // Set original values
          setOriginalScript(scriptPath);
          setOriginalIgnore(ignore);
          setOriginalAllowLatest(allowLatest);
          setOriginalVersionPin(pin);
          setOriginalRestartAfter(restartDeps);
          setOriginalTagRegex(tagRegexValue);
          setOriginalNotify(notify);
          setOriginalNotifyLevel(notifyLevelValue);
          setOriginalNotifyDigest(notifyDigestValue);

          // Keep pending navigation edits protected until saved or explicitly reset.
        }
      }
  }).catch(error => console.error('Failed to fetch Docksmith data:', error)), [containerName]);

  // Retry in place instead of reloading the page
  const retry = useCallback(() => {
    setError(null);
    setLoading(!getCachedInspect(containerName));
    fetchInspect();
    fetchDocksmithData();
  }, [containerName, fetchInspect, fetchDocksmithData]);

  // Ref to hold the streaming AbortController so we can cancel from anywhere
  const streamAbortRef = useRef<AbortController | null>(null);

  // Fetch initial logs then start streaming
  const fetchLogs = useCallback((showSpinner = true) => {
    streamAbortRef.current?.abort();
    const abortController = new AbortController();
    streamAbortRef.current = abortController;
    return getContainerLogs(containerName, { tail: tailLines }).then(async response => {
      if (abortController.signal.aborted) return;
      if (showSpinner) setLogsLoading(false);
      if (!response.success || !response.data) {
        setLogs(`Error: ${response.error || 'Failed to fetch logs'}`);
        return;
      }
      setLogs(response.data.logs);
    // Start streaming new logs with follow=true&tail=0
    if (abortController.signal.aborted) return;
    try {
      const streamRes = await fetch(
        `/api/containers/${encodeURIComponent(containerName)}/logs?follow=true&tail=0`,
        { signal: abortController.signal }
      );
      if (!streamRes.ok || !streamRes.body) return;

      const reader = streamRes.body.getReader();
      const decoder = new TextDecoder();

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        const chunk = decoder.decode(value, { stream: true });
        if (chunk) {
          setLogs(prev => prev + chunk);
        }
      }
      // Flush any remaining bytes from the decoder
      const remaining = decoder.decode();
      if (remaining) {
        setLogs(prev => prev + remaining);
      }
    } catch {
      // AbortError is expected on cleanup, ignore all stream errors
    }
    }).catch(error => {
      if (abortController.signal.aborted) return;
      if (showSpinner) setLogsLoading(false);
      setLogs(`Error: ${error instanceof Error ? error.message : 'Unknown error'}`);
    });
  }, [containerName, tailLines]);

  const logInput = activeTab === 'logs' ? tailLines : null;
  const [previousLogInput, setPreviousLogInput] = useState(logInput);
  if (previousLogInput !== logInput) {
    setPreviousLogInput(logInput);
    setLogsLoading(logInput !== null);
  }

  // Initial fetch
  useEffect(() => {
    fetchInspect();
    fetchDocksmithData();
  }, [fetchInspect, fetchDocksmithData]);

  // Pending edits were seeded before this render; clear the navigation payload
  // only after they are captured so refresh does not replay old edits.
  useEffect(() => {
    if (incoming?.selectedScript !== undefined || incoming?.restartAfter !== undefined || incoming?.tagRegex !== undefined) {
      navigate(location.pathname, { replace: true, state: {} });
    }
  }, [incoming, location.pathname, navigate]);

  // Fetch logs + start streaming when logs tab is active
  useEffect(() => {
    if (activeTab !== 'logs') return;
    fetchLogs(true);
    return () => {
      // Cancel stream when leaving logs tab
      if (streamAbortRef.current) {
        streamAbortRef.current.abort();
        streamAbortRef.current = null;
      }
    };
  }, [activeTab, fetchLogs]);

  // Auto-scroll logs
  useEffect(() => {
    if (autoScroll && logsViewerRef.current) {
      requestAnimationFrame(() => {
        if (logsViewerRef.current) {
          logsViewerRef.current.scrollTop = logsViewerRef.current.scrollHeight;
        }
      });
    }
  }, [logs, autoScroll]);

  const hasChanges = selectedScript !== originalScript || ignoreFlag !== originalIgnore ||
    allowLatestFlag !== originalAllowLatest || versionPin !== originalVersionPin ||
    restartAfter !== originalRestartAfter || tagRegex !== originalTagRegex ||
    notifyFlag !== originalNotify || notifyLevel !== originalNotifyLevel || notifyDigest !== originalNotifyDigest;

  // Handle scroll events
  const handleLogsScroll = useCallback(() => {
    if (!logsViewerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = logsViewerRef.current;
    const isNearBottom = scrollHeight - scrollTop - clientHeight < 200;
    setShowScrollButton(!isNearBottom);
  }, []);

  const scrollToBottom = useCallback(() => {
    if (logsViewerRef.current) {
      logsViewerRef.current.scrollTo({
        top: logsViewerRef.current.scrollHeight,
        behavior: 'smooth'
      });
    }
  }, []);

  // Container actions - all go through operation page
  const handleAction = (action: 'start' | 'stop' | 'restart') => {
    navigate('/operation', {
      state: { [action]: { containerName } }
    });
  };

  // Remove container - uses operation page
  const handleRemove = () => {
    setConfirmRemove(false);
    navigate('/operation', {
      state: {
        remove: {
          containerName,
          force: inspectData?.state.running,
        }
      }
    });
  };

  // Download logs
  const downloadLogs = () => {
    const blob = new Blob([logs], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${containerName}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  // Docksmith settings handlers
  const handleSave = (force = false) => {
    if (!docksmithData) return;

    const labelChanges: Record<string, boolean | string | string[]> = {};

    if (ignoreFlag !== originalIgnore) labelChanges.ignore = ignoreFlag;
    if (allowLatestFlag !== originalAllowLatest) labelChanges.allow_latest = allowLatestFlag;
    if (versionPin !== originalVersionPin) {
      labelChanges.version_pin_major = versionPin === 'major';
      labelChanges.version_pin_minor = versionPin === 'minor';
      labelChanges.version_pin_patch = versionPin === 'patch';
    }
    if (selectedScript !== originalScript) labelChanges.script = selectedScript || '';
    if (restartAfter !== originalRestartAfter) labelChanges.restart_after = restartAfter || '';
    if (tagRegex !== originalTagRegex) labelChanges.tag_regex = tagRegex || '';
    if (notifyFlag !== originalNotify) labelChanges.notify = notifyFlag;
    if (notifyLevel !== originalNotifyLevel) labelChanges.notify_level = notifyLevel;
    if (notifyDigest !== originalNotifyDigest) {
      if (notifyDigest === '') labelChanges.remove_labels = ['docksmith.notify-digest'];
      else labelChanges.notify_digest = notifyDigest === 'true';
    }

    if (Object.keys(labelChanges).length === 0) return;

    navigate('/operation', {
      state: {
        restart: {
          containerName: docksmithData.container_name,
          force,
          saveSettings: true,
          labelChanges,
        }
      }
    });
  };

  const handleReset = () => {
    pendingStateRef.current = { selectedScript: undefined, restartAfter: undefined, tagRegex: undefined };
    setSelectedScript(originalScript);
    setIgnoreFlag(originalIgnore);
    setAllowLatestFlag(originalAllowLatest);
    setVersionPin(originalVersionPin);
    setRestartAfter(originalRestartAfter);
    setTagRegex(originalTagRegex);
    setNotifyFlag(originalNotify);
    setNotifyLevel(originalNotifyLevel);
    setNotifyDigest(originalNotifyDigest);
    setError(null);
  };

  const handleRestart = (force = false) => {
    if (!docksmithData) return;
    navigate('/operation', {
      state: {
        restart: {
          containerName: docksmithData.container_name,
          force,
        }
      }
    });
  };

  const handleRefreshPrecheck = async () => {
    if (!docksmithData || refreshingPrecheck) return;

    setRefreshingPrecheck(true);
    try {
      // Use the synchronous single-container recheck endpoint
      const response = await recheckContainer(docksmithData.container_name);
      if (response.success && response.data) {
        const labelsResponse = await getContainerLabels(docksmithData.container_name);
        if (labelsResponse.success && labelsResponse.data) {
          setDocksmithData({
            ...response.data,
            labels: labelsResponse.data.labels || {},
          });
        } else {
          setDocksmithData(response.data);
        }
      }
    } catch (err) {
      console.error('Failed to refresh precheck:', err);
      setError('Failed to refresh precheck status');
    } finally {
      setRefreshingPrecheck(false);
    }
  };

  // Status badge helpers
  const getStatusBadge = () => {
    if (!docksmithData) return null;
    const allowLatest = docksmithData.labels?.['docksmith.allow-latest'] === 'true';
    if (
      docksmithData.using_floating_tag &&
      !allowLatest &&
      docksmithData.status !== 'IGNORED' &&
      !isUpdatable(docksmithData.status) &&
      docksmithData.status !== 'COMPOSE_MISMATCH' &&
      docksmithData.status !== 'LOCAL_IMAGE'
    ) {
      return <span className="docksmith-badge floating" title="Floating tag — no exact version tag could be resolved. Mount a read:packages PAT for a target.">Floating Tag</span>;
    }
    switch (docksmithData.status) {
      case 'UPDATE_AVAILABLE':
        return <span className="docksmith-badge update">Update Available</span>;
      case 'UPDATE_AVAILABLE_BLOCKED':
        return <span className="docksmith-badge blocked">Update Blocked</span>;
      case 'UP_TO_DATE':
        return <span className="docksmith-badge current">Up to Date</span>;
      case 'UP_TO_DATE_PINNABLE':
        if (docksmithData?.env_controlled) {
          return <span className="docksmith-badge pinnable" title={`Image set by $${docksmithData.env_var_name || 'ENV'} in .env`}>Pinnable <i className="fa-solid fa-file-code"></i></span>;
        }
        return <span className="docksmith-badge pinnable">Pinnable</span>;
      case 'LOCAL_IMAGE':
        return <span className="docksmith-badge local">Local Image</span>;
      case 'IGNORED':
        return <span className="docksmith-badge ignored">Ignored</span>;
      case 'METADATA_UNAVAILABLE':
        return <span className="docksmith-badge metadata">Metadata Unavailable</span>;
      case 'COMPOSE_MISMATCH':
        return <span className="docksmith-badge mismatch">Compose Mismatch</span>;
      default:
        return <span className="docksmith-badge unknown">{docksmithData.status}</span>;
    }
  };

  const getChangeTypeBadge = () => {
    if (!docksmithData) return null;
    const changeTypeName = getChangeTypeName(docksmithData.change_type);
    if (
      docksmithData.change_type === ChangeType.NoChange ||
      docksmithData.change_type === ChangeType.UnknownChange ||
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

  const getUpdateButtonClass = (changeType: number): string => {
    switch (changeType) {
      case ChangeType.PatchChange:
      case ChangeType.BuildChange: return 'patch';
      case ChangeType.MinorChange: return 'minor';
      case ChangeType.MajorChange: return 'major';
      default: return 'primary';
    }
  };

  const getUpdateButtonLabel = (changeType: number): string => {
    switch (changeType) {
      case ChangeType.PatchChange: return 'Patch';
      case ChangeType.BuildChange: return 'Build';
      case ChangeType.MinorChange: return 'Minor';
      case ChangeType.MajorChange: return 'Major';
      case ChangeType.Downgrade: return 'Downgrade';
      default: return 'Update';
    }
  };

  // Loading state
  if (loading) {
    return (
      <div className="container-page">
        <header>
          <Tooltip label="Back" placement="bottom" align="start">
            <button className="back-btn" onClick={handleBack}>
              <i className="fa-solid fa-chevron-left"></i>
            </button>
          </Tooltip>
          <h1>{containerName}</h1>
        </header>
        <main className="main-loading">
          <div className="loading-hint-row">
            <i className="fa-solid fa-circle-notch fa-spin"></i>
            <p className="loading-hint">
              {reconnecting
                ? 'Reconnecting to Docksmith — the page will fill in when it responds.'
                : 'Waiting for a response from Docksmith…'}
            </p>
            <button className="retry-btn" onClick={retry}>Retry now</button>
          </div>
          <SkeletonDetailSection />
          <SkeletonDetailSection />
          <SkeletonDetailSection />
        </main>
      </div>
    );
  }

  // Error state
  if (error && !inspectData) {
    return (
      <div className="container-page">
        <header>
          <Tooltip label="Back" placement="bottom" align="start">
            <button className="back-btn" onClick={handleBack}>
              <i className="fa-solid fa-chevron-left"></i>
            </button>
          </Tooltip>
          <h1>{containerName}</h1>
        </header>
        <main>
          <div className="error-state">
            <i className="fa-solid fa-triangle-exclamation"></i>
            <p>{error}</p>
            <button onClick={retry}>Retry</button>
          </div>
        </main>
      </div>
    );
  }

  const isRunning = inspectData?.state.running;

  return (
    <div className="container-page">
      <header>
        <Tooltip label="Back" placement="bottom" align="start">
          <button className="back-btn" onClick={handleBack}>
            <i className="fa-solid fa-chevron-left"></i>
          </button>
        </Tooltip>
        <div className="header-info">
          <h1>{containerName}</h1>
          <div className="header-badges">
            <span className={`status-badge ${inspectData?.state.status || 'unknown'}`}>
              {inspectData?.state.status || 'Unknown'}
              {inspectData?.state.health && ` (${inspectData.state.health})`}
            </span>
            {getStatusBadge()}
            {getChangeTypeBadge()}
            {reconnecting && (
              <div className="connection-status reconnecting" title="Reconnecting...">
                <i className="fa-solid fa-wifi fa-fade"></i>
                <span>Reconnecting...</span>
              </div>
            )}
          </div>
        </div>
        <div className="header-actions">
          {isRunning ? (
            <>
              <Tooltip label="Stop container" placement="bottom" align="end">
                <button
                  className="action-btn"
                  onClick={() => handleAction('stop')}
                >
                  <i className="fa-solid fa-stop"></i>
                </button>
              </Tooltip>
              <Tooltip label="Restart container" placement="bottom" align="end">
                <button
                  className="action-btn"
                  onClick={() => handleAction('restart')}
                >
                  <i className="fa-solid fa-rotate"></i>
                </button>
              </Tooltip>
            </>
          ) : (
            <Tooltip label="Start container" placement="bottom" align="end">
              <button
                className="action-btn"
                onClick={() => handleAction('start')}
              >
                <i className="fa-solid fa-play"></i>
              </button>
            </Tooltip>
          )}
          {confirmRemove ? (
            <div className="confirm-remove">
              <Tooltip label="Confirm remove" placement="bottom" align="end">
                <button
                  className="action-btn danger"
                  onClick={handleRemove}
                >
                  <i className="fa-solid fa-check"></i>
                </button>
              </Tooltip>
              <Tooltip label="Cancel remove" placement="bottom" align="end">
                <button
                  className="action-btn"
                  onClick={() => setConfirmRemove(false)}
                >
                  <i className="fa-solid fa-xmark"></i>
                </button>
              </Tooltip>
            </div>
          ) : (
            <Tooltip label="Remove container" placement="bottom" align="end">
              <button
                className="action-btn danger"
                onClick={() => setConfirmRemove(true)}
              >
                <i className="fa-solid fa-trash"></i>
              </button>
            </Tooltip>
          )}
        </div>
      </header>

      <nav className="tab-nav">
        <button
          className={activeTab === 'overview' ? 'active' : ''}
          onClick={() => setActiveTab('overview')}
        >
          <i className="fa-solid fa-circle-info"></i>
          Overview
        </button>
        <button
          className={activeTab === 'config' ? 'active' : ''}
          onClick={() => setActiveTab('config')}
        >
          <i className="fa-solid fa-sliders"></i>
          Config
          {hasChanges && <span className="tab-indicator"></span>}
        </button>
        <button
          className={activeTab === 'logs' ? 'active' : ''}
          onClick={() => setActiveTab('logs')}
        >
          <i className="fa-solid fa-file-lines"></i>
          Logs
        </button>
        <button
          className={activeTab === 'inspect' ? 'active' : ''}
          onClick={() => setActiveTab('inspect')}
        >
          <i className="fa-solid fa-magnifying-glass"></i>
          Inspect
        </button>
      </nav>

      <main className="tab-content">
        {/* A failed refetch leaves the cached inspect on screen; the error belongs
            above the tabs so it is visible wherever the user happens to be. */}
        {error && (
          <div className="error-banner">
            <i className="fa-solid fa-triangle-exclamation"></i>
            <span>{error}</span>
            <Tooltip label="Dismiss error" placement="bottom" align="end">
              <button onClick={() => setError(null)}>×</button>
            </Tooltip>
          </div>
        )}

        {/* Overview Tab */}
        {activeTab === 'overview' && inspectData && (
          <div className="overview-tab">
            {/* Docksmith Version Card */}
            {docksmithData && (docksmithData.status === 'UPDATE_AVAILABLE' || docksmithData.status === 'UPDATE_AVAILABLE_BLOCKED') &&
             (docksmithData.current_version || docksmithData.current_tag) && docksmithData.latest_version && (
              <section className="version-card">
                <div className="version-info">
                  <span className="version-current">{(() => {
                    // Show tag with resolved version: "latest (2026.1.29)" or just version if tag contains it
                    const tag = docksmithData.current_tag || '';
                    const version = docksmithData.current_version || '';
                    const tagContainsVersion = tag && version && tag.includes(version);
                    if (tag && version && tag !== version && !tagContainsVersion) {
                      return `${tag} (${version})`;
                    }
                    return tag || version;
                  })()}</span>
                  <i className="fa-solid fa-arrow-right"></i>
                  <span className="version-latest">{(() => {
                    // Show tag with resolved version: "latest (2026.2.3)" or just tag if no resolved version
                    const latestTag = docksmithData.latest_version || '';
                    const latestResolved = docksmithData.latest_resolved_version || '';
                    const tagContainsVersion = latestTag && latestResolved && latestTag.includes(latestResolved);
                    if (latestTag && latestResolved && latestTag !== latestResolved && !tagContainsVersion) {
                      return `${latestTag} (${latestResolved})`;
                    }
                    return latestTag;
                  })()}</span>
                </div>
                {!hasChanges && (docksmithData.status === 'UPDATE_AVAILABLE' || docksmithData.status === 'UPDATE_AVAILABLE_BLOCKED') && (
                  <button
                    className={`update-btn ${docksmithData.status === 'UPDATE_AVAILABLE_BLOCKED' ? 'force' : getUpdateButtonClass(docksmithData.change_type)}`}
                    onClick={() => navigate('/operation', {
                      state: {
                        update: {
                          containers: [{
                            name: docksmithData.container_name,
                            target_version: docksmithData.latest_version || docksmithData.recommended_tag || '',
                            stack: docksmithData.stack || '',
                            force: docksmithData.status === 'UPDATE_AVAILABLE_BLOCKED',
                          }]
                        }
                      }
                    })}
                  >
                    <i className="fa-solid fa-arrow-up"></i>
                    {docksmithData.status === 'UPDATE_AVAILABLE_BLOCKED' ? 'Force' : getUpdateButtonLabel(docksmithData.change_type)}
                  </button>
                )}
              </section>
            )}

            {/* Pinnable Version Card */}
            {docksmithData?.status === 'UP_TO_DATE_PINNABLE' && docksmithData.recommended_tag && (
              <section className="version-card">
                <div className="version-info">
                  <span className="version-current">
                    {docksmithData.using_latest_tag ? 'latest' : (docksmithData.current_tag || 'untagged')}
                  </span>
                  <i className="fa-solid fa-arrow-right"></i>
                  <span className="version-latest">{docksmithData.recommended_tag}</span>
                </div>
                {!hasChanges && (
                  <Tooltip label={`Replace the floating tag with ${docksmithData.recommended_tag}`} placement="top" align="end">
                    <button
                      className="update-btn pin"
                      onClick={() => navigate('/operation', {
                        state: {
                          update: {
                            containers: [{
                              name: docksmithData.container_name,
                              target_version: docksmithData.recommended_tag || '',
                              stack: docksmithData.stack || '',
                            }]
                          }
                        }
                      })}
                    >
                      <i className="fa-solid fa-thumbtack"></i>
                      Pin to tag
                    </button>
                  </Tooltip>
                )}
              </section>
            )}

            {/* Compose Mismatch Card */}
            {docksmithData?.status === 'COMPOSE_MISMATCH' && (
              <section className="mismatch-card">
                <div className="mismatch-info">
                  <i className="fa-solid fa-triangle-exclamation"></i>
                  <div className="mismatch-text">
                    <strong>Compose Mismatch</strong>
                    <div className="mismatch-details">
                      <span className="mismatch-row">
                        <span className="mismatch-label">Running:</span>
                        <code>{docksmithData.image}</code>
                      </span>
                      <span className="mismatch-row">
                        <span className="mismatch-label">Compose:</span>
                        <code>{docksmithData.compose_image || 'unknown'}</code>
                      </span>
                    </div>
                  </div>
                </div>
                <button
                  className="fix-mismatch-btn"
                  onClick={() => navigate('/operation', {
                    state: {
                      fixMismatch: {
                        containerName: docksmithData.container_name,
                      }
                    }
                  })}
                >
                  <i className="fa-solid fa-rotate"></i>
                  Fix Mismatch
                </button>
              </section>
            )}

            {/* Labels Sync Warning */}
            {docksmithData?.labels_out_of_sync && (
              <div className="sync-warning">
                <i className="fa-solid fa-rotate"></i>
                <div className="sync-warning-text">
                  <strong>Labels Out of Sync</strong>
                  <p>Compose file labels differ from running container. Restart to apply changes.</p>
                </div>
                <button className="sync-btn" onClick={() => handleRestart(false)}>
                  Sync
                </button>
              </div>
            )}

            {/* Env-Controlled Info */}
            {docksmithData?.env_controlled && docksmithData.env_var_name && (
              <section className="env-info-card">
                <i className="fa-solid fa-file-code"></i>
                <div className="env-info-text">
                  <strong>Env-Controlled Image</strong>
                  <p>Image tag set by <code>${docksmithData.env_var_name}</code> in <code>.env</code>. Updates will modify this variable.</p>
                </div>
              </section>
            )}

            {/* Ghost Tag Note */}
            {docksmithData?.note && (
              <section className="ghost-note-card">
                <i className="fa-solid fa-ghost"></i>
                <span>{docksmithData.note}</span>
              </section>
            )}

            {/* Container Info */}
            <section className="info-section">
              <h3>Container</h3>
              <div className="info-grid">
                <div className="info-item">
                  <span className="info-label">ID</span>
                  <span className="info-value mono">{inspectData.id.substring(0, 12)}</span>
                </div>
                <div className="info-item">
                  <span className="info-label">Name</span>
                  <span className="info-value">{inspectData.name}</span>
                </div>
                <div className="info-item">
                  <span className="info-label">Image</span>
                  <span className="info-value mono">
                    {docksmithData && getRegistryUrl(docksmithData.image) ? (
                      <a
                        href={getRegistryUrl(docksmithData.image)!}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="image-link"
                      >
                        {inspectData.image}
                        <i className="fa-solid fa-external-link-alt"></i>
                      </a>
                    ) : (
                      inspectData.image
                    )}
                  </span>
                </div>
                <div className="info-item">
                  <span className="info-label">Created</span>
                  <span className="info-value">{formatTimestamp(inspectData.created)}</span>
                </div>
                {docksmithData?.stack && (
                  <div className="info-item">
                    <span className="info-label">Stack</span>
                    <span className="info-value">{docksmithData.stack}</span>
                  </div>
                )}
                {docksmithData?.service && (
                  <div className="info-item">
                    <span className="info-label">Service</span>
                    <span className="info-value">{docksmithData.service}</span>
                  </div>
                )}
              </div>
            </section>

            {/* State */}
            <section className="info-section">
              <h3>State</h3>
              <div className="info-grid">
                <div className="info-item">
                  <span className="info-label">Status</span>
                  <span className={`info-value status-${inspectData.state.status}`}>
                    {inspectData.state.status}
                  </span>
                </div>
                {inspectData.state.health && (
                  <div className="info-item">
                    <span className="info-label">Health</span>
                    <span className="info-value">{inspectData.state.health}</span>
                  </div>
                )}
                <div className="info-item">
                  <span className="info-label">Started</span>
                  <span className="info-value">{formatTimestamp(inspectData.state.started_at)}</span>
                </div>
                {inspectData.state.exit_code !== 0 && (
                  <div className="info-item">
                    <span className="info-label">Exit Code</span>
                    <span className="info-value">{inspectData.state.exit_code}</span>
                  </div>
                )}
                {inspectData.state.pid > 0 && (
                  <div className="info-item">
                    <span className="info-label">PID</span>
                    <span className="info-value mono">{inspectData.state.pid}</span>
                  </div>
                )}
              </div>
            </section>

            {/* Network */}
            {inspectData.network_settings && (
              <section className="info-section">
                <h3>Network</h3>
                <div className="info-grid">
                  {inspectData.network_settings.ip_address && (
                    <div className="info-item">
                      <span className="info-label">IP Address</span>
                      <span className="info-value mono">{inspectData.network_settings.ip_address}</span>
                    </div>
                  )}
                  {inspectData.network_settings.gateway && (
                    <div className="info-item">
                      <span className="info-label">Gateway</span>
                      <span className="info-value mono">{inspectData.network_settings.gateway}</span>
                    </div>
                  )}
                  {inspectData.network_settings.mac_address && (
                    <div className="info-item">
                      <span className="info-label">MAC Address</span>
                      <span className="info-value mono">{inspectData.network_settings.mac_address}</span>
                    </div>
                  )}
                </div>
                {inspectData.network_settings.ports && Object.keys(inspectData.network_settings.ports).length > 0 && (
                  <div className="info-subsection">
                    <h4>Ports</h4>
                    <ul className="port-list">
                      {Object.entries(inspectData.network_settings.ports).map(([port, bindings]) => (
                        <li key={port}>
                          <span className="port-container">{port}</span>
                          {bindings && bindings.length > 0 && (
                            <>
                              <i className="fa-solid fa-arrow-right"></i>
                              {bindings.map((b, i) => (
                                <span key={i} className="port-host">
                                  {b.host_ip || '0.0.0.0'}:{b.host_port}
                                </span>
                              ))}
                            </>
                          )}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {inspectData.network_settings.networks && Object.keys(inspectData.network_settings.networks).length > 0 && (
                  <div className="info-subsection">
                    <h4>Networks</h4>
                    <ul className="network-list">
                      {Object.entries(inspectData.network_settings.networks).map(([netName, net]) => (
                        <li key={netName}>
                          <span className="network-name">{netName}</span>
                          {net.ip_address && <span className="network-ip">{net.ip_address}</span>}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </section>
            )}

            {/* Mounts */}
            {inspectData.mounts && inspectData.mounts.length > 0 && (
              <section className="info-section">
                <h3>Mounts</h3>
                <ul className="mount-list">
                  {inspectData.mounts.map((mount, i) => (
                    <li key={i}>
                      <span className={`mount-type ${mount.type}`}>{mount.type}</span>
                      <span className="mount-source mono">{mount.source}</span>
                      <i className="fa-solid fa-arrow-right"></i>
                      <span className="mount-dest mono">{mount.destination}</span>
                      <span className={`mount-mode ${mount.rw ? 'rw' : 'ro'}`}>
                        {mount.rw ? 'rw' : 'ro'}
                      </span>
                    </li>
                  ))}
                </ul>
              </section>
            )}

            {/* Host Config */}
            {inspectData.host_config && (
              <section className="info-section">
                <h3>Host Configuration</h3>
                <div className="info-grid">
                  <div className="info-item">
                    <span className="info-label">Network Mode</span>
                    <span className="info-value">{inspectData.host_config.network_mode || 'default'}</span>
                  </div>
                  {inspectData.host_config.restart_policy?.name && (
                    <div className="info-item">
                      <span className="info-label">Restart Policy</span>
                      <span className="info-value">{inspectData.host_config.restart_policy.name}</span>
                    </div>
                  )}
                  {inspectData.host_config.memory > 0 && (
                    <div className="info-item">
                      <span className="info-label">Memory Limit</span>
                      <span className="info-value">{formatSize(inspectData.host_config.memory)}</span>
                    </div>
                  )}
                  {inspectData.host_config.privileged && (
                    <div className="info-item">
                      <span className="info-label">Privileged</span>
                      <span className="info-value warning">Yes</span>
                    </div>
                  )}
                </div>
              </section>
            )}

            {/* Environment Variables */}
            {inspectData.config?.env && inspectData.config.env.length > 0 && (
              <section className="info-section">
                <h3>Environment Variables</h3>
                <ul className="env-list">
                  {inspectData.config.env.map((env, i) => {
                    const [key, ...valueParts] = env.split('=');
                    const value = valueParts.join('=');
                    const isSensitive = /password|secret|key|token|auth/i.test(key);
                    return (
                      <li key={i}>
                        <span className="env-key">{key}</span>
                        <span className="env-value mono">
                          {isSensitive ? '••••••••' : value}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              </section>
            )}

            {/* Labels */}
            {inspectData.labels && Object.keys(inspectData.labels).length > 0 && (
              <section className="info-section">
                <h3>Labels</h3>
                <ul className="label-list">
                  {Object.entries(inspectData.labels)
                    .sort(([a], [b]) => {
                      const aIsDocksmith = a.startsWith('docksmith.');
                      const bIsDocksmith = b.startsWith('docksmith.');
                      if (aIsDocksmith && !bIsDocksmith) return -1;
                      if (!aIsDocksmith && bIsDocksmith) return 1;
                      return a.localeCompare(b);
                    })
                    .map(([key, value]) => (
                      <li key={key} className={key.startsWith('docksmith.') ? 'docksmith-label' : ''}>
                        <span className="label-key">{key}</span>
                        <span className="label-value mono">{value}</span>
                      </li>
                    ))}
                </ul>
              </section>
            )}
          </div>
        )}

        {/* Config Tab */}
        {activeTab === 'config' && (
          <div className="config-tab">
            <div className="settings-grid">
              {/* Ignore Flag */}
              <div className="checkbox-row">
                <Tooltip label="Help: Ignore container" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={() => setActiveHelp(activeHelp === 'ignore' ? null : 'ignore')}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <label className="row-label-area">
                  <span className="row-label">Ignore Container</span>
                  <input
                    type="checkbox"
                    checked={ignoreFlag}
                    onChange={(e) => setIgnoreFlag(e.target.checked)}
                  />
                </label>
              </div>
              {activeHelp === 'ignore' && (
                <div className="help-tooltip">
                  <strong>{helpContent.ignore.title}</strong>
                  <p>{helpContent.ignore.description}</p>
                </div>
              )}

              {/* Allow Latest Flag */}
              <div className="checkbox-row">
                <Tooltip label="Help: Allow latest tag" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={() => setActiveHelp(activeHelp === 'allowLatest' ? null : 'allowLatest')}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <label className="row-label-area">
                  <span className="row-label">Allow :latest Tag</span>
                  <input
                    type="checkbox"
                    checked={allowLatestFlag}
                    onChange={(e) => setAllowLatestFlag(e.target.checked)}
                  />
                </label>
              </div>
              {activeHelp === 'allowLatest' && (
                <div className="help-tooltip">
                  <strong>{helpContent.allowLatest.title}</strong>
                  <p>{helpContent.allowLatest.description}</p>
                </div>
              )}

              {/* Version Pin */}
              <div className="setting-item segmented-row">
                <div className="setting-label-with-help">
                  <Tooltip label="Help: Version pin" placement="bottom" align="end">
                    <button
                      className="help-btn"
                      onClick={() => setActiveHelp(activeHelp === 'versionPin' ? null : 'versionPin')}
                    >
                      <i className="fa-solid fa-circle-question"></i>
                    </button>
                  </Tooltip>
                  <span className="nav-title">Version pin</span>
                </div>
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
              {versionPin !== 'none' && docksmithData?.using_floating_tag && (
                <div className="pin-inert-warning">
                  <i className="fa-solid fa-triangle-exclamation"></i>
                  <span>This version pin has no effect while the image uses a floating tag. Migrate to a version tag first.</span>
                </div>
              )}
              {activeHelp === 'versionPin' && (
                <div className="help-tooltip">
                  <strong>{helpContent.versionPin.title}</strong>
                  <p>{helpContent.versionPin.description}</p>
                </div>
              )}

              {/* Tag Filter */}
              <div className="setting-item nav-row-with-help">
                <Tooltip label="Help: Tag filter" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={(e) => {
                      e.stopPropagation();
                      setActiveHelp(activeHelp === 'tagFilter' ? null : 'tagFilter');
                    }}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <div
                  className="nav-row-content"
                  onClick={() => navigate(`/container/${containerName}/tag-filter`, { state: {
                    currentTagRegex: tagRegex,
                    pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                    pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                  } })}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${containerName}/tag-filter`)}
                >
                  <span className="nav-title">Tag Filter</span>
                  <span className="nav-value">
                    {tagRegex || 'None'}
                    <span className="nav-arrow">›</span>
                  </span>
                </div>
              </div>
              {activeHelp === 'tagFilter' && (
                <div className="help-tooltip">
                  <strong>{helpContent.tagFilter.title}</strong>
                  <p>{helpContent.tagFilter.description}</p>
                </div>
              )}

              {/* Notifications */}
              <div className="checkbox-row">
                <Tooltip label="Help: Notifications" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={() => setActiveHelp(activeHelp === 'notify' ? null : 'notify')}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <label className="row-label-area">
                  <span className="row-label">Notifications</span>
                  <input
                    type="checkbox"
                    checked={notifyFlag}
                    onChange={(e) => setNotifyFlag(e.target.checked)}
                  />
                </label>
              </div>
              {activeHelp === 'notify' && (
                <div className="help-tooltip">
                  <strong>{helpContent.notify.title}</strong>
                  <p>{helpContent.notify.description}</p>
                </div>
              )}

              <div className="setting-item segmented-row">
                <div className="setting-label-with-help">
                  <Tooltip label="Help: Notification level" placement="bottom" align="end">
                    <button
                      className="help-btn"
                      onClick={() => setActiveHelp(activeHelp === 'notifyLevel' ? null : 'notifyLevel')}
                    >
                      <i className="fa-solid fa-circle-question"></i>
                    </button>
                  </Tooltip>
                  <label htmlFor="notify-level-select" className="nav-title">Notification Level</label>
                </div>
                <select
                  id="notify-level-select"
                  className="retention-select"
                  value={notifyLevel}
                  disabled={!notifyFlag}
                  onChange={(e) => setNotifyLevel(e.target.value)}
                >
                  <option value="">Use global</option>
                  <option value="major">Major</option>
                  <option value="minor">Minor</option>
                  <option value="patch">Patch</option>
                  <option value="build">Build</option>
                </select>
              </div>
              {activeHelp === 'notifyLevel' && (
                <div className="help-tooltip">
                  <strong>{helpContent.notifyLevel.title}</strong>
                  <p>{helpContent.notifyLevel.description}</p>
                </div>
              )}

              <div className="setting-item segmented-row">
                <div className="setting-label-with-help">
                  <Tooltip label="Help: Digest notifications" placement="bottom" align="end">
                    <button
                      className="help-btn"
                      onClick={() => setActiveHelp(activeHelp === 'notifyDigest' ? null : 'notifyDigest')}
                    >
                      <i className="fa-solid fa-circle-question"></i>
                    </button>
                  </Tooltip>
                  <label htmlFor="notify-digest-select" className="nav-title">Digest Notifications</label>
                </div>
                <select
                  id="notify-digest-select"
                  className="retention-select"
                  value={notifyDigest}
                  disabled={!notifyFlag}
                  onChange={(e) => setNotifyDigest(e.target.value as '' | 'true' | 'false')}
                >
                  <option value="">Use global</option>
                  <option value="true">On</option>
                  <option value="false">Off</option>
                </select>
              </div>
              {activeHelp === 'notifyDigest' && (
                <div className="help-tooltip">
                  <strong>{helpContent.notifyDigest.title}</strong>
                  <p>{helpContent.notifyDigest.description}</p>
                </div>
              )}

              {/* Pre-Update Script */}
              <div className="setting-item precheck-row-with-help">
                <Tooltip label="Help: Pre-update script" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={(e) => {
                      e.stopPropagation();
                      setActiveHelp(activeHelp === 'preUpdateScript' ? null : 'preUpdateScript');
                    }}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <div
                  className="precheck-nav-area"
                  onClick={() => navigate(`/container/${containerName}/script-selection`, { state: {
                    currentScript: selectedScript,
                    pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                    pendingRestartAfter: restartAfter !== originalRestartAfter ? restartAfter : undefined,
                  } })}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${containerName}/script-selection`)}
                >
                  <span className="nav-title">
                    Pre-Update Script
                    {docksmithData && (docksmithData.pre_update_check_pass || docksmithData.pre_update_check_fail) && (
                      <span className={`precheck-status ${docksmithData.pre_update_check_pass ? 'pass' : 'fail'}`}>
                        {docksmithData.pre_update_check_pass ? (
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
                {(selectedScript || docksmithData?.pre_update_check) && (
                  <Tooltip label="Re-run pre-update check" placement="bottom" align="end">
                    <button
                      className="precheck-refresh-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleRefreshPrecheck();
                      }}
                      disabled={refreshingPrecheck}
                    >
                      <i className={`fa-solid fa-rotate-right ${refreshingPrecheck ? 'spinning' : ''}`}></i>
                    </button>
                  </Tooltip>
                )}
              </div>
              {activeHelp === 'preUpdateScript' && (
                <div className="help-tooltip">
                  <strong>{helpContent.preUpdateScript.title}</strong>
                  <p>{helpContent.preUpdateScript.description}</p>
                </div>
              )}

              {/* Restart Dependencies */}
              <div className="setting-item nav-row-with-help">
                <Tooltip label="Help: Restart dependencies" placement="bottom" align="end">
                  <button
                    className="help-btn"
                    onClick={(e) => {
                      e.stopPropagation();
                      setActiveHelp(activeHelp === 'restartDeps' ? null : 'restartDeps');
                    }}
                  >
                    <i className="fa-solid fa-circle-question"></i>
                  </button>
                </Tooltip>
                <div
                  className="nav-row-content"
                  onClick={() => navigate(`/container/${containerName}/restart-dependencies`, { state: {
                    currentDependencies: restartAfter,
                    pendingTagRegex: tagRegex !== originalTagRegex ? tagRegex : undefined,
                    pendingScript: selectedScript !== originalScript ? selectedScript : undefined,
                  } })}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => e.key === 'Enter' && navigate(`/container/${containerName}/restart-dependencies`)}
                >
                  <span className="nav-title">Restart Dependencies</span>
                  <span className="nav-value">
                    {restartAfter ? `${restartAfter.split(',').length}` : 'None'}
                    <span className="nav-arrow">›</span>
                  </span>
                </div>
              </div>
              {activeHelp === 'restartDeps' && (
                <div className="help-tooltip">
                  <strong>{helpContent.restartDeps.title}</strong>
                  <p>{helpContent.restartDeps.description}</p>
                </div>
              )}

              {/* Docker Compose Dependencies (read-only) */}
              {docksmithData?.dependencies && docksmithData.dependencies.length > 0 && (
                <div className="setting-item compose-deps">
                  <span className="nav-title">Compose Dependencies</span>
                  <span className="nav-value">
                    {docksmithData.dependencies.join(', ')}
                  </span>
                </div>
              )}
            </div>

            {/* Config Footer - only shown when there are unsaved changes */}
            {hasChanges && (
              <div className="config-footer">
                <button className="button button-secondary" onClick={handleReset}>
                  <i className="fa-solid fa-undo"></i>
                  Cancel
                </button>
                <button className="button button-primary" onClick={() => handleSave(false)}>
                  <i className="fa-solid fa-save"></i>
                  Save & Restart
                </button>
                {docksmithData?.status === 'UPDATE_AVAILABLE_BLOCKED' && (
                  <button className="button button-warning" onClick={() => handleSave(true)}>
                    <i className="fa-solid fa-triangle-exclamation"></i>
                    Force
                  </button>
                )}
              </div>
            )}
          </div>
        )}

        {/* Logs Tab */}
        {activeTab === 'logs' && (
          <div className="logs-tab">
            <div className="logs-toolbar">
              <div className="logs-controls">
                <label>
                  Lines:
                  <select
                    value={tailLines}
                    onChange={(e) => setTailLines(Number(e.target.value))}
                  >
                    <option value={100}>100</option>
                    <option value={250}>250</option>
                    <option value={500}>500</option>
                    <option value={1000}>1000</option>
                    <option value={5000}>5000</option>
                  </select>
                </label>
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    checked={autoScroll}
                    onChange={(e) => setAutoScroll(e.target.checked)}
                  />
                  Auto-scroll
                </label>
              </div>
              <div className="logs-actions">
                <Tooltip label="Refresh logs" placement="bottom" align="end">
                  <button onClick={() => { setLogsLoading(true); void fetchLogs(true); }} disabled={logsLoading}>
                    <i className={`fa-solid fa-rotate ${logsLoading ? 'fa-spin' : ''}`}></i>
                  </button>
                </Tooltip>
                <Tooltip label="Download logs" placement="bottom" align="end">
                  <button onClick={downloadLogs}>
                    <i className="fa-solid fa-download"></i>
                  </button>
                </Tooltip>
              </div>
            </div>
            <div className="logs-viewer" ref={logsViewerRef} onScroll={handleLogsScroll}>
              <pre
                dangerouslySetInnerHTML={{
                  __html: logs ? ansiToHtml(logs) : 'No logs available'
                }}
              />
            </div>
            <Tooltip label="Scroll to bottom" placement="top" align="end">
              <button
                className={`logs-scroll-btn ${showScrollButton ? 'visible' : ''}`}
                onClick={scrollToBottom}
              >
                <i className="fa-solid fa-arrow-down"></i>
              </button>
            </Tooltip>
          </div>
        )}

        {/* Inspect Tab */}
        {activeTab === 'inspect' && inspectData && (
          <div className="inspect-tab">
            <div className="inspect-toolbar">
              <button
                onClick={() => {
                  navigator.clipboard.writeText(JSON.stringify(inspectData, null, 2));
                  toast.success('Copied to clipboard');
                }}
                title="Copy JSON"
              >
                <i className="fa-solid fa-copy"></i>
                Copy JSON
              </button>
            </div>
            <div className="inspect-viewer">
              <pre>{JSON.stringify(inspectData, null, 2)}</pre>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
