import { useState, useEffect } from 'react';
import { checkContainers, getContainerStatus, getDockerConfig } from '../api/client';
import type { DiscoveryResult, DockerRegistryInfo } from '../types/api';
import { formatTimeAgo } from '../utils/time';

interface SettingsProps {
  onBack: () => void;
}

export function Settings({ onBack: _onBack }: SettingsProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [lastCheckTime, setLastCheckTime] = useState<string | null>(null);
  const [lastBackgroundRun, setLastBackgroundRun] = useState<string | null>(null);
  const [cacheAge, setCacheAge] = useState<string>('Loading...');
  const [backgroundAge, setBackgroundAge] = useState<string>('Loading...');
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
        setCacheAge(formatTimeAgo(lastCheckTime));
      }
      if (lastBackgroundRun) {
        setBackgroundAge(formatTimeAgo(lastBackgroundRun));
      }
    }, 10000);
    return () => clearInterval(interval);
  }, [lastCheckTime, lastBackgroundRun]);

  const fetchStatus = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getContainerStatus();
      if (response.success && response.data) {
        setResult(response.data);
        if (response.data.last_cache_refresh) {
          setLastCheckTime(response.data.last_cache_refresh);
          setCacheAge(formatTimeAgo(response.data.last_cache_refresh));
        } else {
          setLastCheckTime(null);
          setCacheAge('Never');
        }
        if (response.data.last_background_run) {
          setLastBackgroundRun(response.data.last_background_run);
          setBackgroundAge(formatTimeAgo(response.data.last_background_run));
        } else {
          setLastBackgroundRun(null);
          setBackgroundAge('Never');
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
    } catch {
      // Silently fail - UI will show "No authenticated registries found"
    }
  };

  const backgroundRefresh = async () => {
    setLoading(true);
    setError(null);
    try {
      await fetch('/api/trigger-check', { method: 'POST' });
      // Wait for background check to start and update timestamps
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
        if (response.data.last_cache_refresh) {
          setLastCheckTime(response.data.last_cache_refresh);
          setCacheAge(formatTimeAgo(response.data.last_cache_refresh));
        } else {
          setLastCheckTime(null);
          setCacheAge('Never');
        }
        if (response.data.last_background_run) {
          setLastBackgroundRun(response.data.last_background_run);
          setBackgroundAge(formatTimeAgo(response.data.last_background_run));
        } else {
          setLastBackgroundRun(null);
          setBackgroundAge('Never');
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
                  <span className="setting-label monospace">{registry}</span>
                  <i className="fa-solid fa-check-circle" style={{ color: 'var(--color-success)', fontSize: '18px' }}></i>
                </div>
              ))
            ) : (
              <div className="setting-row">
                <span className="setting-value">No authenticated registries found</span>
              </div>
            )}
            <div className="setting-info">
              <i className="fa-solid fa-circle-info"></i>
              {dockerConfig?.running_in_docker ? (
                <>Mounted from host: {dockerConfig?.host_config_path || '~/.docker/config.json'}</>
              ) : (
                <>Credentials stored in {dockerConfig?.config_path || '~/.docker/config.json'}</>
              )}
            </div>
          </div>
        </section>

        {/* Footer */}
        <section className="settings-footer">
          <div className="footer-content">
            <img src="/docksmith-title.svg" alt="Docksmith" className="footer-logo" />
            <p className="footer-text">Made with ❤️ by chis</p>
          </div>
        </section>
      </div>
    </div>
  );
}
