import { useState, useEffect, useMemo } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { List } from 'react-window';
import { useRegistryTags } from '../hooks/useRegistryTags';
import { useContainerData } from '../hooks/useContainerData';
import { ContainerConfigCard } from '../components/ContainerConfigCard';
import { parseImageRef } from '../utils/registry';
import './TagFilterPage.css';

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

export function TagFilterPage() {
  const { containerName } = useParams<{ containerName: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const { container, loading: loadingContainer } = useContainerData(containerName);
  const [pattern, setPattern] = useState('');
  const [originalPattern, setOriginalPattern] = useState('');
  const [isValid, setIsValid] = useState(true);
  const [errorMessage, setErrorMessage] = useState('');
  const [copied, setCopied] = useState(false);

  // Get current tag regex from location state (passed from detail page)
  const currentTagRegex = (location.state as any)?.currentTagRegex || '';

  // Get container details
  const currentTag = container?.current_tag || '';
  const imageRef = container?.image ? parseImageRef(container.image).repository : '';

  // Fetch tags from registry
  const { tags, loading, error: fetchError } = useRegistryTags(imageRef);

  // Initialize pattern from location state (passed from detail page)
  useEffect(() => {
    // Use location state value if available, otherwise fallback to container label
    const initialPattern = currentTagRegex || container?.labels?.['docksmith.tag-regex'] || '';
    setPattern(initialPattern);
    setOriginalPattern(initialPattern);
  }, [currentTagRegex, container]);

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

  // Copy tags and context for AI regex generation
  const handleCopyForAI = async () => {
    const tagsList = tags.slice(0, 100).join('\n'); // Limit to first 100 tags for clipboard
    const prompt = `I need a regex pattern to filter Docker image tags for "${imageRef}".

Current tag: ${currentTag}
${pattern ? `Current regex pattern: ${pattern}` : 'No regex pattern set yet.'}

Available tags (sample):
${tagsList}${tags.length > 100 ? `\n... and ${tags.length - 100} more tags` : ''}

Please provide a regex pattern that matches the tags I want. The pattern should be compatible with JavaScript's RegExp.`;

    try {
      await navigator.clipboard.writeText(prompt);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy:', err);
    }
  };

  // Get other pending changes from location state (to preserve them)
  const pendingScript = (location.state as any)?.pendingScript;
  const pendingRestartAfter = (location.state as any)?.pendingRestartAfter;

  const handleDone = () => {
    if (!isValid || !containerName) return;

    // Navigate back to detail page with the new pattern and any other pending changes
    navigate(`/container/${containerName}`, {
      state: {
        tab: 'config',
        tagRegex: pattern,
        selectedScript: pendingScript,
        restartAfter: pendingRestartAfter,
      }
    });
  };

  const handleCancel = () => {
    if (!containerName) return;

    // Navigate back, preserving other pending changes but not the tag regex change
    navigate(`/container/${containerName}`, {
      state: {
        tab: 'config',
        tagRegex: originalPattern, // Restore original
        selectedScript: pendingScript,
        restartAfter: pendingRestartAfter,
      }
    });
  };

  const hasChanges = pattern !== originalPattern;

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
        <button className="back-button" onClick={() => navigate(`/container/${containerName}`, { state: { tab: 'config' } })}>
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
          <ContainerConfigCard container={container} />
        )}

        {!loadingContainer && (
          <>
            <section className="regex-section">
              <div className="section-header">
                <h2>Regular Expression Pattern</h2>
                <span className="section-hint">Filter which tags are considered for updates</span>
              </div>

              <div className="input-group">
                <div className="input-wrapper">
                  <input
                    type="text"
                    className={`regex-input ${!isValid ? 'invalid' : ''}`}
                    value={pattern}
                    onChange={(e) => setPattern(e.target.value)}
                    placeholder="^v?[0-9.]+-alpine$"
                    spellCheck={false}
                  />
                  {pattern && (
                    <button
                      type="button"
                      className="clear-input-button"
                      onClick={() => setPattern('')}
                      aria-label="Clear pattern"
                    >
                      <i className="fa-solid fa-times"></i>
                    </button>
                  )}
                </div>
                {errorMessage && <span className="error-message">{errorMessage}</span>}
                {!errorMessage && pattern && isValid && (
                  <span className="success-message">Valid regex pattern</span>
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
                <div className="tags-header-actions">
                  <span className="match-counter">
                    {matchCount} of {tags.length} tags match
                  </span>
                  {tags.length > 0 && (
                    <button
                      className="copy-for-ai-btn"
                      onClick={handleCopyForAI}
                      title="Copy tags for AI regex generation"
                    >
                      <i className={`fa-solid ${copied ? 'fa-check' : 'fa-copy'}`}></i>
                      {copied ? 'Copied!' : 'Copy for AI'}
                    </button>
                  )}
                </div>
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

      </main>

      <footer className="page-footer">
        <button
          className="button button-secondary"
          onClick={handleCancel}
        >
          Cancel
        </button>
        <button
          className="button button-primary"
          onClick={handleDone}
          disabled={!isValid || !hasChanges}
        >
          Done
        </button>
      </footer>
    </div>
  );
}
