import { useState, useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getScripts } from '../api/client';
import type { Script } from '../types/api';
import { useContainerData } from '../hooks/useContainerData';
import { ContainerConfigCard } from '../components/ContainerConfigCard';
import './ScriptSelectionPage.css';

export function ScriptSelectionPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { containerName } = useParams<{ containerName: string }>();

  const { container, loading: loadingContainer } = useContainerData(containerName);
  const [scripts, setScripts] = useState<Script[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedScript, setSelectedScript] = useState<string>('');
  const [originalScript, setOriginalScript] = useState<string>('');

  // Get current script from location state
  const currentScript = (location.state as any)?.currentScript || '';

  useEffect(() => {
    if (!containerName) {
      navigate('/');
      return;
    }

    // Initialize selected script from current
    setSelectedScript(currentScript);
    setOriginalScript(currentScript);

    const fetchScripts = async () => {
      try {
        setLoading(true);
        const scriptsResponse = await getScripts();

        if (scriptsResponse.success && scriptsResponse.data) {
          setScripts(scriptsResponse.data.scripts || []);
        } else {
          setError(scriptsResponse.error || 'Failed to load scripts');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    };

    fetchScripts();
  }, [containerName, navigate, currentScript]);

  // Get other pending changes from location state (to preserve them)
  const pendingTagRegex = (location.state as any)?.pendingTagRegex;
  const pendingRestartAfter = (location.state as any)?.pendingRestartAfter;

  const handleDone = () => {
    if (!containerName) return;

    // Navigate back to detail page with the selected script and any other pending changes
    navigate(`/container/${containerName}`, {
      state: {
        selectedScript: selectedScript,
        tagRegex: pendingTagRegex,
        restartAfter: pendingRestartAfter,
      }
    });
  };

  const handleCancel = () => {
    if (!containerName) return;

    // Navigate back, preserving other pending changes but not the script change
    navigate(`/container/${containerName}`, {
      state: {
        selectedScript: originalScript, // Restore original
        tagRegex: pendingTagRegex,
        restartAfter: pendingRestartAfter,
      }
    });
  };

  const hasChanges = selectedScript !== originalScript;

  const filteredScripts = scripts.filter(script => {
    if (!searchQuery) return true;
    const query = searchQuery.toLowerCase();
    return (
      script.name.toLowerCase().includes(query) ||
      script.path.toLowerCase().includes(query)
    );
  });

  return (
    <div className="page script-selection-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate(`/container/${containerName}`)}>
          ← Back
        </button>
        <h1>Select Script</h1>
        <div className="header-spacer"></div>
      </header>

      <main className="page-content">
        {/* Current Configuration */}
        {loadingContainer && (
          <div className="loading-state">
            <span className="spinner" />
            <p>Loading container data...</p>
          </div>
        )}

        {!loadingContainer && container && (
          <ContainerConfigCard container={container} />
        )}


        {/* Current Selection */}
        {selectedScript && (
          <div className="selection-summary">
            <div className="summary-header">
              <span className="summary-label">Selected Script</span>
              <button className="clear-button" onClick={() => setSelectedScript('')}>
                Clear
              </button>
            </div>
            <div className="selected-script-display">
              <i className="fa-solid fa-shield-alt"></i>
              <span className="script-name-display">{selectedScript.split('/').pop()}</span>
            </div>
          </div>
        )}

        {/* Search Bar */}
        <div className="search-section">
          <div className="search-bar">
            <i className="fa-solid fa-search"></i>
            <input
              type="text"
              placeholder="Search scripts..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="search-input"
            />
            {searchQuery && (
              <button className="clear-search" onClick={() => setSearchQuery('')}>
                <i className="fa-solid fa-times"></i>
              </button>
            )}
          </div>
        </div>

        {/* Scripts List */}
        {loading ? (
          <div className="loading-state">
            <div className="spinner"></div>
            <p>Loading scripts...</p>
          </div>
        ) : error ? (
          <div className="error-state">
            <i className="fa-solid fa-exclamation-circle"></i>
            <p className="error-text">{error}</p>
          </div>
        ) : (
          <>
            {/* None Option */}
            <div className="section-header">
              <h2>Available Scripts</h2>
              <span className="section-hint">{filteredScripts.length} found</span>
            </div>

            <div className="scripts-list">
              {/* None/Clear option */}
              <label className={`script-item none-option ${selectedScript === '' ? 'selected' : ''}`}>
                <input
                  type="radio"
                  name="script"
                  checked={selectedScript === ''}
                  onChange={() => setSelectedScript('')}
                  className="script-radio"
                />
                <div className="radio-icon">
                  <i className={selectedScript === '' ? 'fa-solid fa-check-circle' : 'fa-regular fa-circle'}></i>
                </div>
                <div className="script-info">
                  <span className="script-name">No Script</span>
                  <span className="script-path">Don't run any pre-update check</span>
                </div>
              </label>

              {filteredScripts.length === 0 ? (
                <div className="empty-state">
                  <i className="fa-solid fa-search"></i>
                  <p>No scripts found matching "{searchQuery}"</p>
                </div>
              ) : (
                filteredScripts.map(script => {
                  const isSelected = selectedScript === script.path;
                  return (
                    <label
                      key={script.path}
                      className={`script-item ${!script.executable ? 'disabled' : ''} ${isSelected ? 'selected' : ''}`}
                    >
                      <input
                        type="radio"
                        name="script"
                        checked={isSelected}
                        onChange={() => script.executable && setSelectedScript(script.path)}
                        disabled={!script.executable}
                        className="script-radio"
                      />
                      <div className="radio-icon">
                        <i className={isSelected ? 'fa-solid fa-check-circle' : 'fa-regular fa-circle'}></i>
                      </div>
                      <div className="script-info">
                        <div className="script-header">
                          <span className="script-name">{script.name}</span>
                          {!script.executable && (
                            <span className="script-badge not-executable">Not Executable</span>
                          )}
                        </div>
                        <span className="script-path">{script.path}</span>
                      </div>
                    </label>
                  );
                })
              )}
            </div>

            {scripts.length === 0 && (
              <div className="info-box">
                <i className="fa-solid fa-info-circle"></i>
                <div>
                  <strong>No scripts available</strong>
                  <p>Place executable scripts in your scripts directory to use pre-update checks.</p>
                </div>
              </div>
            )}
          </>
        )}

      </main>

      {/* Footer */}
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
          disabled={!hasChanges}
        >
          Done
        </button>
      </footer>
    </div>
  );
}
