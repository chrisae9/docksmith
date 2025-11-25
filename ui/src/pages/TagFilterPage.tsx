import { useState, useEffect, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { List } from 'react-window';
import { useRegistryTags } from '../hooks/useRegistryTags';
import { setLabels, getContainerStatus, getContainerLabels } from '../api/client';
import type { ContainerInfo } from '../types/api';
import { getRegistryUrl } from '../utils/registry';
import './TagFilterPage.css';

interface TagFilterPageProps {
  container?: ContainerInfo;
}

interface TagInfo {
  name: string;
  matches: boolean;
  isCurrent: boolean;
  isLatest: boolean;
}

const REGEX_PRESETS = {
  'Alpine builds only': '^.*-alpine$',
  'CUDA builds only': '^.*-cuda[0-9.]+.*$',
  'LTS tags': '^v?[0-9]+-lts(-[a-z]+)?$',
  'Stable releases (no pre-release)': '^v?[0-9]+\\.[0-9]+\\.[0-9]+(-[a-z]+)?$',
  'No nightly builds': '^(?!.*nightly).*$',
} as const;

export function TagFilterPage({ container: containerProp }: TagFilterPageProps) {
  const { containerName } = useParams<{ containerName: string }>();
  const navigate = useNavigate();

  const [container, setContainer] = useState<ContainerInfo | undefined>(containerProp);
  const [loadingContainer, setLoadingContainer] = useState(!containerProp);
  const [pattern, setPattern] = useState('');
  const [isValid, setIsValid] = useState(true);
  const [errorMessage, setErrorMessage] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [preCheckFailed, setPreCheckFailed] = useState(false);
  const [restartProgress, setRestartProgress] = useState<{
    stage: 'stopping' | 'starting' | 'checking' | 'complete' | 'failed';
    message: string;
    startTime: number;
    logs: Array<{ time: number; message: string; type: 'info' | 'success' | 'error' }>;
  } | null>(null);

  // Fetch container data if not provided as prop
  useEffect(() => {
    if (containerProp) {
      setContainer(containerProp);
      setLoadingContainer(false);
      return;
    }

    if (!containerName) return;

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
            // Merge labels into container object
            setContainer({
              ...foundContainer,
              labels: labelsResponse.data.labels || {},
            });
          }
        }
      } catch (err) {
        console.error('Failed to fetch container data:', err);
      } finally {
        setLoadingContainer(false);
      }
    };

    fetchContainerData();
  }, [containerName, containerProp]);

  // Get container details
  const currentTag = container?.current_tag || '';
  const imageRef = container?.image?.split(':')[0] || '';

  // Fetch tags from registry
  const { tags, loading, error: fetchError } = useRegistryTags(imageRef);

  // Load existing tag regex from container labels
  useEffect(() => {
    if (container?.labels?.['docksmith.tag-regex']) {
      setPattern(container.labels['docksmith.tag-regex']);
    }
  }, [container]);

  // Validate regex pattern
  useEffect(() => {
    if (!pattern) {
      setIsValid(true);
      setErrorMessage('');
      return;
    }

    try {
      new RegExp(pattern);

      if (pattern.length > 500) {
        setIsValid(false);
        setErrorMessage('Pattern too long (max 500 characters)');
        return;
      }

      setIsValid(true);
      setErrorMessage('');
    } catch (e) {
      setIsValid(false);
      setErrorMessage(e instanceof Error ? e.message : 'Invalid regex pattern');
    }
  }, [pattern]);

  // Process tags with match info
  const processedTags = useMemo((): TagInfo[] => {
    if (!tags.length) return [];

    let regex: RegExp | null = null;
    if (pattern && isValid) {
      try {
        regex = new RegExp(pattern);
      } catch {
        regex = null;
      }
    }

    return tags.map((tag) => ({
      name: tag,
      matches: regex ? regex.test(tag) : true,
      isCurrent: tag === currentTag,
      isLatest: false, // TODO: determine latest from API
    }));
  }, [tags, pattern, isValid, currentTag]);

  // Sort tags: current first, then matching, then by name
  const sortedTags = useMemo(() => {
    return [...processedTags].sort((a, b) => {
      if (a.isCurrent) return -1;
      if (b.isCurrent) return 1;

      if (a.matches !== b.matches) {
        return a.matches ? -1 : 1;
      }

      return a.name.localeCompare(b.name, undefined, { numeric: true });
    });
  }, [processedTags]);

  const matchCount = processedTags.filter((t) => t.matches).length;

  const handlePresetClick = (presetPattern: string) => {
    setPattern(presetPattern);
  };

  const addLog = (message: string, type: 'info' | 'success' | 'error' = 'info') => {
    setRestartProgress(prev => {
      if (!prev) return prev;
      return {
        ...prev,
        logs: [...prev.logs, { time: Date.now(), message, type }],
      };
    });
  };

  const handleSave = async (force = false) => {
    if (!isValid || !containerName || !container) return;

    setSaving(true);
    setSaveError(null);
    setPreCheckFailed(false);

    const startTime = Date.now();

    // Start progress view
    setRestartProgress({
      stage: 'stopping',
      message: 'Saving tag filter and restarting...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Saving tag filter with force restart for ${containerName}`
            : `Saving tag filter for ${containerName}`,
          type: 'info'
        }
      ],
    });

    try {
      addLog('Updating compose file...', 'info');

      const response = await setLabels(containerName, {
        tag_regex: pattern || '',
        force,
      });

      if (response.success && response.data) {
        const data = response.data;

        addLog('✓ Compose file updated', 'success');

        if (data.pre_check_ran && data.pre_check_passed) {
          addLog('✓ Pre-update check passed', 'success');
        }

        if (data.restarted) {
          addLog('✓ Container restarted successfully', 'success');
        }

        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: 'Tag filter saved and container restarted',
        } : null);

        addLog('✓ Tag filter saved successfully', 'success');

        // Wait briefly then navigate back to dashboard
        setTimeout(() => {
          navigate('/');
        }, 1500);
      } else {
        throw new Error(response.error || 'Failed to save tag filter');
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : String(err);

      // Check if it's a pre-update check failure
      if (errorMessage.includes('pre-update check failed') || errorMessage.includes('script exited with code')) {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Pre-update check failed',
        } : null);
        addLog(`✗ Pre-update check failed: ${errorMessage}`, 'error');
        setPreCheckFailed(true);
      } else {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Save failed',
        } : null);
        addLog(`✗ Error: ${errorMessage}`, 'error');
        setSaveError(errorMessage);
      }
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async (force = false) => {
    if (!containerName || !container) return;

    setSaving(true);
    setSaveError(null);
    setPreCheckFailed(false);

    const startTime = Date.now();

    setRestartProgress({
      stage: 'stopping',
      message: 'Clearing tag filter and restarting...',
      startTime,
      logs: [
        {
          time: startTime,
          message: force
            ? `Clearing tag filter with force restart for ${containerName}`
            : `Clearing tag filter for ${containerName}`,
          type: 'info'
        }
      ],
    });

    try {
      addLog('Updating compose file...', 'info');

      const response = await setLabels(containerName, {
        tag_regex: '',
        force,
      });

      if (response.success && response.data) {
        const data = response.data;

        addLog('✓ Compose file updated', 'success');

        if (data.pre_check_ran && data.pre_check_passed) {
          addLog('✓ Pre-update check passed', 'success');
        }

        if (data.restarted) {
          addLog('✓ Container restarted successfully', 'success');
        }

        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'complete',
          message: 'Tag filter cleared and container restarted',
        } : null);

        addLog('✓ Tag filter cleared successfully', 'success');

        // Wait briefly then navigate back to dashboard
        setTimeout(() => {
          navigate('/');
        }, 1500);
      } else {
        throw new Error(response.error || 'Failed to clear tag filter');
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : String(err);

      // Check if it's a pre-update check failure
      if (errorMessage.includes('pre-update check failed') || errorMessage.includes('script exited with code')) {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Pre-update check failed',
        } : null);
        addLog(`✗ Pre-update check failed: ${errorMessage}`, 'error');
        setPreCheckFailed(true);
      } else {
        setRestartProgress(prev => prev ? {
          ...prev,
          stage: 'failed',
          message: 'Clear failed',
        } : null);
        addLog(`✗ Error: ${errorMessage}`, 'error');
        setSaveError(errorMessage);
      }
    } finally {
      setSaving(false);
    }
  };

  const handleCancelProgress = () => {
    setRestartProgress(null);
    setPreCheckFailed(false);
    setSaveError(null);
  };

  function TagRow({
    index,
    style,
  }: {
    index: number;
    style: React.CSSProperties;
  }) {
    const tag = sortedTags[index];

    return (
      <div
        className={`tag-item ${tag.matches ? 'matches' : 'no-match'} ${tag.isCurrent ? 'current' : ''}`}
        style={style}
      >
        <span className="tag-name">{tag.name}</span>
        {tag.isCurrent && <span className="tag-badge current-badge">Current</span>}
        {tag.isLatest && <span className="tag-badge latest-badge">Latest</span>}
      </div>
    );
  }

  return (
    <div className="page tag-filter-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1>Tag Filter</h1>
        <div className="header-spacer" />
      </header>

      <main className="page-content">
        {loadingContainer && (
          <div className="loading-state">
            <span className="spinner" />
            <p>Loading container data...</p>
          </div>
        )}

        {!loadingContainer && container && (
          <section className="current-tag-section">
            <div className="section-header">
              <h2>Current Configuration</h2>
            </div>
            <div className="info-card">
              <div className="info-row">
                <span className="info-label">Container</span>
                <span className="info-value">{container.container_name}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Image</span>
                <span className="info-value">
                  {getRegistryUrl(imageRef) ? (
                    <a
                      href={getRegistryUrl(imageRef)!}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="image-link"
                    >
                      {imageRef}
                      <i className="fa-solid fa-external-link-alt" style={{ marginLeft: '6px', fontSize: '12px', opacity: 0.7 }}></i>
                    </a>
                  ) : (
                    imageRef
                  )}
                </span>
              </div>
              <div className="info-row">
                <span className="info-label">Current Tag</span>
                <span className="info-value code">{currentTag}</span>
              </div>
            </div>
          </section>
        )}

        {!loadingContainer && (
          <>
            <section className="regex-section">
              <div className="section-header">
                <h2>Regular Expression Pattern</h2>
                <span className="section-hint">Filter which tags are considered for updates</span>
              </div>

              <div className="input-group">
                <input
                  type="text"
                  className={`regex-input ${!isValid ? 'error' : ''}`}
                  value={pattern}
                  onChange={(e) => setPattern(e.target.value)}
                  placeholder="^v?[0-9.]+-alpine$"
                  spellCheck={false}
                />
                {errorMessage && <span className="error-message">{errorMessage}</span>}
                {!errorMessage && pattern && isValid && (
                  <span className="success-message">✓ Valid regex pattern</span>
                )}
              </div>

              <div className="presets-section">
                <h3>Quick Patterns</h3>
                <div className="presets-grid">
                  {Object.entries(REGEX_PRESETS).map(([label, presetPattern]) => (
                    <button
                      key={label}
                      className="preset-button"
                      onClick={() => handlePresetClick(presetPattern)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
            </section>

            <section className="tags-section">
              <div className="section-header">
                <h2>Available Tags</h2>
                <span className="match-counter">
                  {matchCount} of {tags.length} tags match
                </span>
              </div>

              {loading && (
                <div className="loading-state">
                  <span className="spinner" />
                  <p>Loading tags from registry...</p>
                </div>
              )}

              {fetchError && (
                <div className="error-state">
                  <p className="error-text">{fetchError}</p>
                </div>
              )}

              {!loading && !fetchError && tags.length === 0 && (
                <div className="empty-state">
                  <p>No tags found for this image</p>
                </div>
              )}

              {!loading && !fetchError && tags.length > 0 && (
                <div className="tags-list">
                  <List
                    defaultHeight={400}
                    rowCount={sortedTags.length}
                    rowHeight={48}
                    rowComponent={TagRow}
                    rowProps={{} as never}
                    style={{ width: '100%' }}
                  />
                </div>
              )}
            </section>
          </>
        )}

        {saveError && (
          <div className="save-error">
            <p>{saveError}</p>
          </div>
        )}
      </main>

      <footer className="page-footer">
        <button
          className="button button-secondary"
          onClick={() => handleClear()}
          disabled={saving || !pattern || !!restartProgress}
        >
          Clear Filter
        </button>
        <button
          className="button button-primary"
          onClick={() => handleSave()}
          disabled={!isValid || saving || !!restartProgress}
        >
          Save Filter
        </button>
      </footer>

      {/* Progress Modal */}
      {restartProgress && (
        <div className="progress-overlay">
          <div className="progress-modal">
            <div className="progress-header">
              <h3>{restartProgress.message}</h3>
              <div className={`progress-status status-${restartProgress.stage}`}>
                {restartProgress.stage === 'complete' && '✓ Complete'}
                {restartProgress.stage === 'failed' && '✗ Failed'}
                {restartProgress.stage === 'stopping' && '⟳ Updating...'}
                {restartProgress.stage === 'starting' && '⟳ Restarting...'}
                {restartProgress.stage === 'checking' && '⟳ Verifying...'}
              </div>
            </div>

            <div className="progress-logs">
              {restartProgress.logs.map((log, idx) => (
                <div key={idx} className={`log-entry log-${log.type}`}>
                  <span className="log-message">{log.message}</span>
                </div>
              ))}
            </div>

            <div className="progress-actions">
              {restartProgress.stage === 'complete' && (
                <button
                  className="button button-primary"
                  onClick={handleCancelProgress}
                >
                  Close
                </button>
              )}
              {restartProgress.stage === 'failed' && (
                <>
                  {preCheckFailed && (
                    <>
                      <button
                        className="button button-secondary"
                        onClick={handleCancelProgress}
                      >
                        Cancel
                      </button>
                      <button
                        className="button button-danger"
                        onClick={() => {
                          const isClear = pattern === '';
                          if (isClear) {
                            handleClear(true);
                          } else {
                            handleSave(true);
                          }
                        }}
                      >
                        Force Save
                      </button>
                    </>
                  )}
                  {!preCheckFailed && (
                    <button
                      className="button button-primary"
                      onClick={handleCancelProgress}
                    >
                      Close
                    </button>
                  )}
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
