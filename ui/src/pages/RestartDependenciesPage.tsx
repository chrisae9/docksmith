import { useState, useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { getContainerStatus } from '../api/client';
import type { ContainerInfo } from '../types/api';
import './RestartDependenciesPage.css';

export function RestartDependenciesPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { containerName } = useParams<{ containerName: string }>();

  const [containers, setContainers] = useState<ContainerInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());

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
    }

    const fetchContainers = async () => {
      try {
        setLoading(true);
        const response = await getContainerStatus();
        if (response.success && response.data) {
          // Filter out the current container from the list
          const allContainers = response.data.containers.filter(
            c => c.container_name !== containerName
          );
          setContainers(allContainers);
        } else {
          setError(response.error || 'Failed to load containers');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    };

    fetchContainers();
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

  const handleSave = () => {
    const dependenciesString = Array.from(selectedContainers).join(', ');
    navigate(`/container/${containerName}`, {
      state: { restartDependsOn: dependenciesString }
    });
  };

  const handleClear = () => {
    navigate(`/container/${containerName}`, {
      state: { restartDependsOn: '' }
    });
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
        <button className="back-button" onClick={() => navigate(`/container/${containerName}`)}>
          ← Back
        </button>
        <h1>Restart Dependencies</h1>
        <div className="header-spacer"></div>
      </header>

      <main className="page-content">
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
        {selectedContainers.size > 0 && (
          <button className="button button-secondary" onClick={handleClear}>
            Clear All
          </button>
        )}
        <button
          className="button button-primary"
          onClick={handleSave}
          disabled={loading}
        >
          {selectedContainers.size > 0 ? `Save (${selectedContainers.size})` : 'Save'}
        </button>
      </footer>
    </div>
  );
}
