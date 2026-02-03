import { useState, useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getContainerStatus } from '../api/client';
import type { ContainerInfo } from '../types/api';
import { useContainerData } from '../hooks/useContainerData';
import { ContainerConfigCard } from '../components/ContainerConfigCard';
import './RestartDependenciesPage.css';

export function RestartDependenciesPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { containerName } = useParams<{ containerName: string }>();

  const { container, loading: loadingContainer } = useContainerData(containerName);
  const [containers, setContainers] = useState<ContainerInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const [originalDependencies, setOriginalDependencies] = useState<Set<string>>(new Set());

  // Get current dependencies from location state or empty
  const currentDependencies = (location.state as any)?.currentDependencies || '';

  useEffect(() => {
    if (!containerName) {
      navigate('/');
      return;
    }

    // Initialize selected containers from current dependencies
    if (currentDependencies) {
      const deps = currentDependencies.split(',').map((d: string) => d.trim()).filter(Boolean);
      setSelectedContainers(new Set(deps));
      setOriginalDependencies(new Set(deps));
    }

    const fetchContainersList = async () => {
      try {
        setLoading(true);
        const statusResponse = await getContainerStatus();

        if (statusResponse.success && statusResponse.data) {
          // Filter out the current container from the list of available containers
          const allContainers = statusResponse.data.containers.filter(
            c => c.container_name !== containerName
          );
          setContainers(allContainers);
        } else {
          setError(statusResponse.error || 'Failed to load containers');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    };

    fetchContainersList();
  }, [containerName, navigate, currentDependencies]);

  const toggleContainer = (name: string) => {
    setSelectedContainers(prev => {
      const newSet = new Set(prev);
      if (newSet.has(name)) {
        newSet.delete(name);
      } else {
        newSet.add(name);
      }
      return newSet;
    });
  };

  // Get other pending changes from location state (to preserve them)
  const pendingTagRegex = (location.state as any)?.pendingTagRegex;
  const pendingScript = (location.state as any)?.pendingScript;

  const handleDone = () => {
    if (!containerName) return;

    const dependenciesString = Array.from(selectedContainers).join(',');

    // Navigate back to detail page with the selected dependencies and any other pending changes
    navigate(`/container/${containerName}`, {
      state: {
        tab: 'config',
        restartAfter: dependenciesString,
        tagRegex: pendingTagRegex,
        selectedScript: pendingScript,
      }
    });
  };

  const handleCancel = () => {
    if (!containerName) return;

    const originalDepsString = Array.from(originalDependencies).join(',');

    // Navigate back, preserving other pending changes but not the dependencies change
    navigate(`/container/${containerName}`, {
      state: {
        tab: 'config',
        restartAfter: originalDepsString, // Restore original
        tagRegex: pendingTagRegex,
        selectedScript: pendingScript,
      }
    });
  };

  // Check if there are changes from original
  const hasChanges = () => {
    if (selectedContainers.size !== originalDependencies.size) return true;
    for (const dep of selectedContainers) {
      if (!originalDependencies.has(dep)) return true;
    }
    return false;
  };

  const filteredContainers = containers.filter(container => {
    if (!searchQuery) return true;
    const query = searchQuery.toLowerCase();
    return (
      container.container_name.toLowerCase().includes(query) ||
      container.image.toLowerCase().includes(query) ||
      container.stack?.toLowerCase().includes(query)
    );
  });

  return (
    <div className="page restart-dependencies-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate(`/container/${containerName}`, { state: { tab: 'config' } })}>
          ← Back
        </button>
        <h1>Restart Dependencies</h1>
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

        {/* Info Section */}
        <div className="info-section">
          <div className="info-card">
            <i className="fa-solid fa-info-circle"></i>
            <div>
              <strong>Select containers to depend on</strong>
              <p>This container will automatically restart when any selected container restarts</p>
            </div>
          </div>
        </div>

        {/* Selection Summary */}
        {selectedContainers.size > 0 && (
          <div className="selection-summary">
            <div className="summary-header">
              <span className="summary-count">{selectedContainers.size} selected</span>
              <button className="clear-all-button" onClick={() => setSelectedContainers(new Set())}>
                Clear All
              </button>
            </div>
            <div className="selected-tags">
              {Array.from(selectedContainers).map(name => (
                <span key={name} className="selected-tag">
                  {name}
                  <button onClick={() => toggleContainer(name)}>
                    <i className="fa-solid fa-times"></i>
                  </button>
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Search Bar */}
        <div className="search-section">
          <div className="search-bar">
            <i className="fa-solid fa-search"></i>
            <input
              type="text"
              placeholder="Search containers..."
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

        {/* Containers List */}
        {loading ? (
          <div className="loading-state">
            <div className="spinner"></div>
            <p>Loading containers...</p>
          </div>
        ) : error ? (
          <div className="error-state">
            <i className="fa-solid fa-exclamation-circle"></i>
            <p className="error-text">{error}</p>
          </div>
        ) : (
          <>
            <div className="section-header">
              <h2>Available Containers</h2>
              <span className="section-hint">{filteredContainers.length} found</span>
            </div>

            {filteredContainers.length === 0 ? (
              <div className="empty-state">
                <i className="fa-solid fa-search"></i>
                <p>No containers found matching "{searchQuery}"</p>
              </div>
            ) : (
              <div className="containers-list">
                {filteredContainers.map(container => {
                  const isSelected = selectedContainers.has(container.container_name);
                  return (
                    <label
                      key={container.container_name}
                      className={`container-item ${isSelected ? 'selected' : ''}`}
                    >
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => toggleContainer(container.container_name)}
                        className="container-checkbox"
                      />
                      <div className="checkbox-icon">
                        <i className={isSelected ? 'fa-solid fa-check-circle' : 'fa-regular fa-circle'}></i>
                      </div>
                      <div className="container-info">
                        <div className="container-header">
                          <span className="container-name">{container.container_name}</span>
                          {container.stack && (
                            <span className="container-stack">{container.stack}</span>
                          )}
                        </div>
                        <span className="container-image">{container.image}</span>
                      </div>
                    </label>
                  );
                })}
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
          disabled={!hasChanges()}
        >
          {selectedContainers.size > 0 ? `Done (${selectedContainers.size})` : 'Done'}
        </button>
      </footer>
    </div>
  );
}
