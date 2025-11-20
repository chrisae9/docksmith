import { useState, useEffect } from 'react';
import { checkContainers, getContainerStatus, getDockerConfig } from '../api/client';
import type { DiscoveryResult, DockerRegistryInfo } from '../types/api';

interface SettingsProps {
  onBack: () => void;
}

export function Settings({ onBack: _onBack }: SettingsProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [lastCheckTime, setLastCheckTime] = useState<string | null>(null);
  const [lastBackgroundRun, setLastBackgroundRun] = useState<string | null>(null);
  const [cacheAge, setCacheAge] = useState<string>('');
  const [backgroundAge, setBackgroundAge] = useState<string>('');
  const [dockerConfig, setDockerConfig] = useState<DockerRegistryInfo | null>(null);

  // Fetch initial status
  useEffect(() => {
    fetchStatus();
    fetchDockerConfigData();
  }, []);

  // Update time ago every 10 seconds
  useEffect(() => {
    const interval = setInterval(() => {
      if (lastCheckTime) {
        setCacheAge(calculateTimeAgo(lastCheckTime));
      }
      if (lastBackgroundRun) {
        setBackgroundAge(calculateTimeAgo(lastBackgroundRun));
      }
    }, 10000);
    return () => clearInterval(interval);
  }, [lastCheckTime, lastBackgroundRun]);

  const calculateTimeAgo = (timestamp: string): string => {
    const now = Date.now();
    const then = new Date(timestamp).getTime();
    const diffMs = now - then;
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHr = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHr / 24);

    if (diffDay > 0) return `${diffDay}d ago`;
    if (diffHr > 0) return `${diffHr}h ago`;
    if (diffMin > 0) return `${diffMin}m ago`;
    return `${diffSec}s ago`;
  };

  const fetchStatus = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getContainerStatus();
      if (response.success && response.data) {
        setResult(response.data);
        if (response.data.last_check) {
          setLastCheckTime(response.data.last_check);
          setCacheAge(calculateTimeAgo(response.data.last_check));
        }
        if (response.data.last_background_run) {
          setLastBackgroundRun(response.data.last_background_run);
          setBackgroundAge(calculateTimeAgo(response.data.last_background_run));
        }
      } else {
        setError(response.error || 'Failed to fetch status');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const fetchDockerConfigData = async () => {
    try {
      const response = await getDockerConfig();
      if (response.success && response.data) {
        setDockerConfig(response.data);
      }
    } catch (err) {
      console.error('Failed to fetch Docker config:', err);
    }
  };

  const backgroundRefresh = async () => {
    setLoading(true);
    setError(null);
    try {
      await fetch('/api/trigger-check', { method: 'POST' });
      await new Promise(resolve => setTimeout(resolve, 500));
      await fetchStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const cacheRefresh = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await checkContainers();
      if (response.success && response.data) {
        setResult(response.data);
        if (response.data.last_check) {
          setLastCheckTime(response.data.last_check);
          setCacheAge(calculateTimeAgo(response.data.last_check));
        }
        if (response.data.last_background_run) {
          setLastBackgroundRun(response.data.last_background_run);
          setBackgroundAge(calculateTimeAgo(response.data.last_background_run));
        }
      } else {
        setError(response.error || 'Failed to fetch data');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="settings-page">
      <header>
        <div className="header-top">
          <h1>Settings</h1>
        </div>
      </header>

      <div className="settings-content">
        {error && (
          <div className="error-banner">
            <i className="fa-solid fa-circle-exclamation"></i>
            {error}
          </div>
        )}

        {/* Status Information */}
        <section className="settings-section">
          <h2 className="section-title">
            <i className="fa-solid fa-clock"></i>
            Status Information
          </h2>
          <div className="settings-card">
            <div className="setting-row">
              <span className="setting-label">Last Background Check:</span>
              <span className="setting-value">{backgroundAge || 'Loading...'}</span>
            </div>
            <div className="setting-row">
              <span className="setting-label">Last Cache Update:</span>
              <span className="setting-value">{cacheAge || 'Loading...'}</span>
            </div>
          </div>
        </section>

        {/* Refresh Controls */}
        <section className="settings-section">
          <h2 className="section-title">
            <i className="fa-solid fa-arrows-rotate"></i>
            Refresh Controls
          </h2>
          <div className="settings-card">
            <button
              onClick={backgroundRefresh}
              className="settings-btn"
              disabled={loading}
            >
              <i className="fa-solid fa-rotate"></i>
              <div>
                <div className="btn-title">Background Refresh</div>
                <div className="btn-description">Check for updates using cached registry data</div>
              </div>
            </button>
            <button
              onClick={cacheRefresh}
              className="settings-btn cache-btn"
              disabled={loading}
            >
              <i className="fa-solid fa-database"></i>
              <div>
                <div className="btn-title">Cache Refresh</div>
                <div className="btn-description">Clear cache and query registries for fresh data</div>
              </div>
            </button>
            <button
              onClick={fetchStatus}
              className="settings-btn"
              disabled={loading}
            >
              <i className="fa-solid fa-sync"></i>
              <div>
                <div className="btn-title">Refresh Display</div>
                <div className="btn-description">Reload current status from cache</div>
              </div>
            </button>
          </div>
        </section>

        {/* Statistics */}
        <section className="settings-section">
          <h2 className="section-title">
            <i className="fa-solid fa-chart-simple"></i>
            Container Statistics
          </h2>
          <div className="settings-card stats-grid">
            <div className="stat-item">
              <div className="stat-value">{result?.total_checked || 0}</div>
              <div className="stat-label">Total Checked</div>
            </div>
            <div className="stat-item">
              <div className="stat-value">{result?.updates_found || 0}</div>
              <div className="stat-label">Updates Found</div>
            </div>
            <div className="stat-item">
              <div className="stat-value">{result?.up_to_date || 0}</div>
              <div className="stat-label">Up to Date</div>
            </div>
            <div className="stat-item">
              <div className="stat-value">{result?.local_images || 0}</div>
              <div className="stat-label">Local Images</div>
            </div>
            <div className="stat-item">
              <div className="stat-value">{result?.failed || 0}</div>
              <div className="stat-label">Failed</div>
            </div>
            <div className="stat-item">
              <div className="stat-value">{result?.ignored || 0}</div>
              <div className="stat-label">Ignored</div>
            </div>
          </div>
        </section>

        {/* Configuration */}
        <section className="settings-section">
          <h2 className="section-title">
            <i className="fa-solid fa-gear"></i>
            Environment Variables
          </h2>
          <div className="settings-card">
            <div className="setting-row">
              <span className="setting-label monospace">CHECK_INTERVAL</span>
              <span className="setting-value monospace">{result?.check_interval || 'Not set'}</span>
            </div>
            <div className="setting-row">
              <span className="setting-label monospace">CACHE_TTL</span>
              <span className="setting-value monospace">{result?.cache_ttl || 'Not set'}</span>
            </div>
            <div className="setting-info">
              <i className="fa-solid fa-circle-info"></i>
              Configure these in docker-compose.yml
            </div>
          </div>
        </section>

        {/* Docker Configuration */}
        <section className="settings-section">
          <h2 className="section-title">
            <i className="fa-brands fa-docker"></i>
            Authenticated Registries
          </h2>
          <div className="settings-card">
            {dockerConfig && dockerConfig.registries.length > 0 ? (
              dockerConfig.registries.map((registry, index) => (
                <div key={index} className="setting-row">
                  <span className="setting-label">
                    <i className="fa-solid fa-check-circle" style={{ color: 'var(--color-success)' }}></i>
                  </span>
                  <span className="setting-value monospace">{registry}</span>
                </div>
              ))
            ) : (
              <div className="setting-row">
                <span className="setting-value">No authenticated registries found</span>
              </div>
            )}
            <div className="setting-info">
              <i className="fa-solid fa-circle-info"></i>
              Credentials are stored in {dockerConfig?.config_path || '~/.docker/config.json'}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
